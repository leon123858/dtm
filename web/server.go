package web

import (
	"dtm/graph"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func CorsConfig() cors.Config {
	corsConf := cors.DefaultConfig()
	corsConf.AllowAllOrigins = true
	corsConf.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	corsConf.AllowHeaders = []string{"Origin", "Content-Type", "Authorization", "X-Requested-With"}
	corsConf.AllowCredentials = true
	corsConf.MaxAge = 12 * 3600 // 12 hours
	return corsConf
}

func setupMiddlewares(r *gin.Engine) {
	// 啟用 CORS (跨來源資源共享)
	r.Use(cors.New(CorsConfig()))
	// 啟用 gzip 壓縮
	r.Use(gzip.Gzip(gzip.DefaultCompression))
}

func Serve() {
	// Setting up Gin
	r := gin.Default()
	setupMiddlewares(r)
	// Setting up routes
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	// GraphQL endpoint
	executableSchema := graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{}})
	r.POST("/query", GraphQLHandler(executableSchema))
	r.GET("/query", GraphQLHandler(executableSchema))
	r.GET("/", GraphQLPlaygroundHandler("DTM", "/query"))
	r.Run()
}
