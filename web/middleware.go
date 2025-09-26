package web

import (
	"bytes"
	"context"
	"dtm/db/db"
	"dtm/graph/utils"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"
)

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func AdminKeyMiddleware() gin.HandlerFunc {
	adminKey := os.Getenv("ADMIN_KEY") // Retrieve from env variable

	if adminKey == "" {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		reqKey := c.GetHeader("X-Admin-Key")

		if reqKey == adminKey {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
	}
}

func CorsConfig(webConfig ServiceConfig) cors.Config {
	corsConf := cors.DefaultConfig()
	if webConfig.IsDev {
		corsConf.AllowAllOrigins = true
	} else {
		var frontend string = "http://localhost:3000" // Default frontend URL
		if os.Getenv("FRONTEND_URL") != "" {
			frontend = os.Getenv("FRONTEND_URL")
		}
		corsConf.AllowAllOrigins = false
		corsConf.AllowOrigins = []string{frontend}
	}
	corsConf.AllowMethods = []string{"GET", "POST"}
	corsConf.AllowHeaders = []string{"Origin", "Content-Type", "X-Requested-With"}
	corsConf.AllowCredentials = true
	corsConf.MaxAge = 1 * 3600 // 1 hours
	return corsConf
}

//func limiterMiddleWare() gin.HandlerFunc {
//	rate := limiter.Rate{
//		Period: 5 * time.Minute,
//		Limit:  1000, // 1000 requests per 5 minutes
//	}
//	store := memory.NewStore()
//	instance := limiter.New(store, rate)
//	middleware := mgin.NewMiddleware(instance)
//
//	return middleware
//}

func GinContextToContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), utils.GinContextKeyValue, c)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// GraphQLBodyLogMiddleware log post body
func GraphQLBodyLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		const maxBodySize = 4 * 1024 * 1024 // 4MB
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Error("Failed to read request body", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		// gin body can read only once so write back
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		logger.Info("post", "body", json.RawMessage(bodyBytes))

		c.Next()
	}
}

func TripDataLoaderInjectionMiddleware(wrapper db.TripDBWrapper) gin.HandlerFunc {
	return func(c *gin.Context) {
		DBTripDataLoader := *db.NewTripDataLoader(wrapper)
		c.Set(string(db.DataLoaderKeyTripData), &DBTripDataLoader)
		c.Next()
	}
}

func setupMiddlewares(r *gin.Engine, webConfig ServiceConfig) {
	// r.Use(limiterMiddleWare()) // We limit it by cloudflare, so no need to limit here
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	r.Use(AdminKeyMiddleware())
	r.Use(GraphQLBodyLogMiddleware(logger))
	r.Use(cors.New(CorsConfig(webConfig)))
	r.Use(secure.New(secure.Config{
		STSSeconds:           2592000, // 1 month
		STSIncludeSubdomains: true,
		FrameDeny:            true,
		ContentTypeNosniff:   true,
		BrowserXssFilter:     true,
		// ContentSecurityPolicy: "default-src 'self'", // Customize as needed
		ReferrerPolicy: "strict-origin-when-cross-origin",
	}))
	r.Use(GinContextToContextMiddleware())
}
