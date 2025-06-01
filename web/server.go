package web

import (
	"dtm/db/mem"
	"dtm/db/pg"
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
	// setup db service
	db, err := pg.InitPostgresGORM(pg.CreateDSN())
	if err != nil {
		panic(err)
	}
	defer pg.CloseGORM(db)
	// GraphQL endpoint
	dbDep := mem.NewInMemoryTripDBWrapper()
	// dbDep := pg.NewGORMTripDBWrapper(db)
	executableSchema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{
		TripDB:                  dbDep,
		TripMessageQueueWrapper: goch.NewGoChanTripMessageQueueWrapper(), // Use in-memory message queue
		// TripDataLoader: loader.TripDataLoader{
		// 	RecordLoader: dataloadgen.NewMappedLoader(dbDep.DataLoaderGetTripRecordList),
		// },
	}})
	r.POST("/query", GraphQLHandler(executableSchema))
	r.GET("/query", GraphQLHandler(executableSchema))
	r.GET("/", GraphQLPlaygroundHandler("DTM", "/query"))
	r.Run()
}
