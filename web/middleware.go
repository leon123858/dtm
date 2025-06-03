package web

import (
	"context"
	"dtm/db/db"
	"dtm/graph/utils"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

func CorsConfig() cors.Config {
	corsConf := cors.DefaultConfig()
	corsConf.AllowAllOrigins = true
	corsConf.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	corsConf.AllowHeaders = []string{"Origin", "Content-Type", "Authorization", "X-Requested-With"}
	corsConf.AllowCredentials = true
	corsConf.MaxAge = 1 * 3600 // 1 hours
	return corsConf
}

func limiterMiddleWare() gin.HandlerFunc {
	rate := limiter.Rate{
		Period: 1 * time.Hour,
		Limit:  1000, // 1000 requests per hour,
	}
	store := memory.NewStore()
	instance := limiter.New(store, rate)
	middleware := mgin.NewMiddleware(instance)

	return middleware
}

func GinContextToContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), utils.GinContextKeyValue, c)
		c.Request = c.Request.WithContext(ctx)
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

func setupMiddlewares(r *gin.Engine) {
	r.Use(limiterMiddleWare())
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	r.Use(cors.New(CorsConfig()))
	r.Use(gzip.Gzip(gzip.DefaultCompression))
	r.Use(secure.New(secure.Config{
		STSSeconds:           31536000, // 1 year
		STSIncludeSubdomains: true,
		FrameDeny:            true,
		ContentTypeNosniff:   true,
		BrowserXssFilter:     true,
		// ContentSecurityPolicy: "default-src 'self'", // Customize as needed
		ReferrerPolicy: "strict-origin-when-cross-origin",
	}))
	r.Use(GinContextToContextMiddleware())
}
