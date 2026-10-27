package web

import (
	"bytes"
	"dtm/domain"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"dtm/adapters/db/db"
	"dtm/adapters/identity"
	"dtm/config"

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
		if isAuthPath(c.Request.URL.Path) {
			c.Next()
			return
		}
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
	corsConf.AllowOrigins = []string{config.FrontendURL()}
	corsConf.AllowMethods = []string{"GET", "POST"}
	corsConf.AllowHeaders = []string{"Origin", "Content-Type", "X-Requested-With", "Authorization"}
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

// GraphQLBodyLogMiddleware log post body
func GraphQLBodyLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isAuthPath(c.Request.URL.Path) || c.Request.Method != http.MethodPost {
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

		jsonData := make(map[string]interface{})
		err = json.Unmarshal(bodyBytes, &jsonData)
		if err != nil {
			logger.Error("Failed to unmarshal request body", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		// rm query property, it is unused
		delete(jsonData, "query")

		jsonStr, err := json.Marshal(jsonData)
		if err != nil {
			logger.Error("Failed to marshal request body", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			return
		}

		logger.Info("post", "body", string(jsonStr))

		c.Next()
	}
}

func TripDataLoaderInjectionMiddleware(wrapper db.DataLoaderStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := db.WithTripDataLoader(c.Request.Context(), db.NewTripDataLoader(wrapper))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func UserBearerExtractIdMiddleware(verifier identity.TokenVerifier, store db.AuthDBWrapper) gin.HandlerFunc {
	httpAuthError := func(status int) string {
		if status == 401 {
			return "UNAUTHENTICATED"
		}
		return "SERVICE_UNAVAILABLE"
	}

	return func(c *gin.Context) {
		headers := c.Request.Header.Values("Authorization")
		if len(headers) == 0 {
			c.Next()
			return
		}
		fields := strings.Fields(c.GetHeader("Authorization"))
		if len(headers) != 1 || len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			c.AbortWithStatusJSON(401, gin.H{"error": "UNAUTHENTICATED"})
			return
		}
		gcid, err := verifier.Verify(c.Request.Context(), fields[1])
		if err != nil {
			status := 401
			if errors.Is(err, identity.ErrKeysUnavailable) {
				status = 503
			}
			c.AbortWithStatusJSON(status, gin.H{"error": httpAuthError(status)})
			return
		}
		userID, err := store.UserGCID2UserId(c.Request.Context(), gcid)
		if err != nil {
			status := 503
			if errors.Is(err, domain.ErrUserNotFound) {
				status = 401
			}
			c.AbortWithStatusJSON(status, gin.H{"error": httpAuthError(status)})
			return
		}
		c.Request = c.Request.WithContext(domain.WithUserID(c.Request.Context(), userID))
		c.Next()
	}
}

func setupMiddlewares(r *gin.Engine, webConfig ServiceConfig) {
	// r.Use(limiterMiddleWare()) // We limit it by cloudflare, so no need to limit here
	standardRecovery := gin.Recovery()
	r.Use(func(c *gin.Context) {
		if !isAuthPath(c.Request.URL.Path) {
			standardRecovery(c)
			return
		}
		defer func() {
			if recover() != nil {
				logger.Error("auth failed", "stage", "recovery", "code", "internal_error")
				c.AbortWithStatusJSON(500, gin.H{"error": "internal_error"})
			}
		}()
		c.Next()
	})
	accessLogger := gin.Logger()
	r.Use(func(c *gin.Context) {
		if isAuthPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		accessLogger(c)
	})
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
}
