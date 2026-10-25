package web

import (
	"context"
	"dtm/adapters/identity"
	"dtm/adapters/mq/gcppubsub"
	"dtm/adapters/mq/goch"
	"dtm/adapters/mq/mq"
	"dtm/adapters/mq/rabbit"
	appconfig "dtm/config"
	"dtm/graph"
	"dtm/services/auth"
	tripservice "dtm/services/trip"

	"log"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/adapters/db/pg"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	amqp "github.com/rabbitmq/amqp091-go"
)

type ServiceConfig struct {
	IsDev  bool
	Port   string
	MqMode mq.Mode
}

func Serve(config ServiceConfig) {
	if err := appconfig.LoadDevEnv(config.IsDev); err != nil {
		panic(err)
	}
	authConfig, err := appconfig.LoadAuth(config.IsDev, config.Port)
	if err != nil {
		panic(err)
	}

	// set by config
	if config.IsDev {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	// Setting up Gin
	r := gin.New()
	// middle ware
	setupMiddlewares(r, config)
	// Setting up health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	// setup service
	var tripDB db.TripDBWrapper
	var authDB db.AuthDBWrapper
	var mqDep mq.TripMessageQueueWrapper
	if config.IsDev {
		tripDB, authDB = mem.NewInMemoryDB()
	} else {
		iDB, err := pg.InitPostgresGORM(pg.CreateDSN())
		if err != nil {
			panic(err)
		}
		defer pg.CloseGORM(iDB)
		tripDB = pg.NewPgTripDBWrapper(iDB)
		authDB = pg.NewPgAuthDBWrapper(iDB)
	}

	// auth service
	authService, err := auth.NewService(context.Background(), authConfig, authDB)
	if err != nil {
		panic(err)
	}
	defer authService.Close()
	setupAuthRoutes(r, authService, authConfig, config.IsDev)

	// mq service
	switch config.MqMode {
	case mq.ModeGoChan:
		mqDep = goch.NewGoChanTripMessageQueueWrapper()
	case mq.ModeRabbitMQ:
		mqc := rabbit.NewRabbitConnection(rabbit.CreateAmqpURL())
		if mqc == nil {
			panic("Failed to connect to RabbitMQ")
		}
		defer func(mqc *amqp.Connection) {
			err := mqc.Close()
			if err != nil {
				panic(err)
			}
		}(mqc)
		var err error
		mqDep, err = rabbit.NewRabbitTripMessageQueueWrapper(mqc)
		if err != nil {
			panic("Failed to create RabbitMQ trip message queue wrapper: " + err.Error())
		}
	case mq.ModeGCPPubSub:
		// os.Setenv("GCP_PROJECT_ID", "gcp-exercise-434714")
		mqc, err := gcppubsub.NewGCPTripMessageQueueWrapper(context.Background(), gcppubsub.GetGCPProjectID())
		if err != nil {
			panic("Failed to create GCP Pub/Sub trip message queue wrapper: " + err.Error())
		}
		mqDep = mqc
	default:
		panic("Unsupported message queue mode: " + string(config.MqMode))
	}
	// GraphQL endpoint
	readerProvider := func(ctx context.Context) (db.Reader, error) {
		return db.TripDataLoaderFromContext(ctx)
	}
	executableSchema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		Auth:                    authService,
		RecordFactory:           tripservice.NewRecordFactory(readerProvider),
		TripFactory:             tripservice.NewTripFactory(tripDB, authDB, readerProvider),
		TripMessageQueueWrapper: mqDep,
	}})
	if config.IsDev {
		r.GET("/", GraphQLPlaygroundHandler("DTM", "/query"))
	}
	// query and mutation endpoints
	query := r.Group("/query")
	query.Use(
		UserBearerExtractIdMiddleware(identity.NewGCIPVerifier(authConfig.ProjectID), authDB),
		gzip.Gzip(gzip.DefaultCompression),
		TripDataLoaderInjectionMiddleware(tripDB),
	)
	query.POST("", GraphQLHandler(executableSchema))
	query.GET("", GraphQLHandler(executableSchema))
	// Subscriptions endpoint
	r.GET("/subscription", TripDataLoaderInjectionMiddleware(tripDB), GraphQLHandler(executableSchema))

	// Start the server
	println("Starting web server on port " + config.Port)
	err = r.Run("0.0.0.0:" + config.Port)
	if err != nil {
		log.Fatal(err)
		return
	}
}
