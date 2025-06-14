package web

import (
	"context"
	"dtm/graph"
	"dtm/mq/gcppubsub"
	"dtm/mq/goch"
	"dtm/mq/mq"
	"dtm/mq/rabbit"

	"dtm/db/db"
	"dtm/db/mem"
	"dtm/db/pg"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

type WebServiceConfig struct {
	IsDev  bool
	Port   string
	MqMode mq.MqMode
}

func Serve(config WebServiceConfig) {
	// set by config
	if config.IsDev {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	// Setting up Gin
	r := gin.Default()
	// middle ware
	setupMiddlewares(r, config)
	// Setting up health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	// setup service
	var dbDep db.TripDBWrapper
	var mqDep mq.TripMessageQueueWrapper
	if config.IsDev {
		dbDep = mem.NewInMemoryTripDBWrapper()
	} else {
		db, err := pg.InitPostgresGORM(pg.CreateDSN())
		if err != nil {
			panic(err)
		}
		defer pg.CloseGORM(db)
		dbDep = pg.NewPgDBWrapper(db)
	}
	switch config.MqMode {
	case mq.MqModeGoChan:
		mqDep = goch.NewGoChanTripMessageQueueWrapper()
	case mq.MqModeRabbitMQ:
		mqc := rabbit.NewRabbitConnection(rabbit.CreateAmqpURL())
		if mqc == nil {
			panic("Failed to connect to RabbitMQ")
		}
		defer mqc.Close()
		var err error
		mqDep, err = rabbit.NewRabbitTripMessageQueueWrapper(mqc)
		if err != nil {
			panic("Failed to create RabbitMQ trip message queue wrapper: " + err.Error())
		}
	case mq.MqModeGCPPubSub:
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
	executableSchema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		TripDB:                  dbDep,
		TripMessageQueueWrapper: mqDep,
	}})
	if config.IsDev {
		r.GET("/", GraphQLPlaygroundHandler("DTM", "/query"))
	}
	// query and mutation endpoints
	r.POST("/query", gzip.Gzip(gzip.DefaultCompression), TripDataLoaderInjectionMiddleware(dbDep), GraphQLHandler(executableSchema))
	r.GET("/query", gzip.Gzip(gzip.DefaultCompression), TripDataLoaderInjectionMiddleware(dbDep), GraphQLHandler(executableSchema))
	// Subscriptions endpoint
	r.GET("/subscription", TripDataLoaderInjectionMiddleware(dbDep), GraphQLHandler(executableSchema))

	// Start the server
	println("Starting web server on port " + config.Port)
	r.Run("0.0.0.0:" + config.Port)
}
