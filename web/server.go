package web

import (
	"dtm/graph"
	"dtm/mq/goch"
	"dtm/mq/rabbit"

	"dtm/db/mem"
	"dtm/db/pg"

	"github.com/gin-gonic/gin"
)

func Serve() {
	// Setting up Gin
	r := gin.Default()
	setupMiddlewares(r)
	// Setting up routes
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	// setup service
	db, err := pg.InitPostgresGORM(pg.CreateDSN())
	if err != nil {
		panic(err)
	}
	defer pg.CloseGORM(db)
	mq, err := rabbit.InitRabbitMQ(rabbit.CreateAmqpURL())
	if err != nil {
		panic(err)
	}
	defer rabbit.CloseRabbitMQ(mq)
	// GraphQL endpoint
	dbDep := mem.NewInMemoryTripDBWrapper()
	// mqDep, err := rabbit.NewRabbitTripMessageQueueWrapper(mq)
	// if err != nil {
	// 	panic(err)
	// }
	// dbDep := pg.NewPgDBWrapper(db)
	executableSchema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		TripDB:                  dbDep,
		TripMessageQueueWrapper: goch.NewGoChanTripMessageQueueWrapper(),
	}})

	r.POST("/query", TripDataLoaderInjectionMiddleware(dbDep), GraphQLHandler(executableSchema))
	r.GET("/query", TripDataLoaderInjectionMiddleware(dbDep), GraphQLHandler(executableSchema))
	r.GET("/", GraphQLPlaygroundHandler("DTM", "/query"))
	r.Run()
}
