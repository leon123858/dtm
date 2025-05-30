package web

import (
	"dtm/db/mem"
	"dtm/graph"
	"dtm/mq/goch"

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
	// GraphQL endpoint
	executableSchema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		TripDB:                  mem.NewInMemoryTripDBWrapper(),          // Use in-memory DB for simplicity
		TripMessageQueueWrapper: goch.NewGoChanTripMessageQueueWrapper(), // Use in-memory message queue
	}})
	r.POST("/query", GraphQLHandler(executableSchema))
	r.GET("/query", GraphQLHandler(executableSchema))
	r.GET("/", GraphQLPlaygroundHandler("DTM", "/query"))
	r.Run()
}
