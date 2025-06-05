package web

import (
	"dtm/graph"
	"dtm/mq/goch"
	"dtm/mq/mq"
	"dtm/mq/rabbit"

	"dtm/db/db"
	"dtm/db/mem"
	"dtm/db/pg"

	"github.com/gin-gonic/gin"
)

type WebServiceConfig struct {
	IsDev bool
}

func Serve(config WebServiceConfig) {
	// Setting up Gin
	r := gin.Default()
	// set by config
	if config.IsDev {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	// middle ware
	setupMiddlewares(r)
	// Setting up health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	// setup service
	var dbDep db.TripDBWrapper
	var mqDep mq.TripMessageQueueWrapper
	if config.IsDev {
		dbDep = mem.NewInMemoryTripDBWrapper()
		mqDep = goch.NewGoChanTripMessageQueueWrapper()
	} else {
		db, err := pg.InitPostgresGORM(pg.CreateDSN())
		if err != nil {
			panic(err)
		}
		defer pg.CloseGORM(db)
		dbDep = pg.NewPgDBWrapper(db)
		mqc := rabbit.NewRabbitConnection(rabbit.CreateAmqpURL())
		if mqc == nil {
			panic("Failed to connect to RabbitMQ")
		}
		defer mqc.Close()
		mqDep, err = rabbit.NewRabbitTripMessageQueueWrapper(mqc)
		if err != nil {
			panic("Failed to create RabbitMQ trip message queue wrapper: " + err.Error())
		}
	}
	// GraphQL endpoint
	executableSchema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		TripDB:                  dbDep,
		TripMessageQueueWrapper: mqDep,
	}})
	if config.IsDev {
		r.GET("/", GraphQLPlaygroundHandler("DTM", "/query"))
	}
	r.POST("/query", TripDataLoaderInjectionMiddleware(dbDep), GraphQLHandler(executableSchema))
	r.GET("/query", TripDataLoaderInjectionMiddleware(dbDep), GraphQLHandler(executableSchema))
	r.Run()
}
