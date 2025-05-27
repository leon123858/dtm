package web

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
)

func GraphQLHandler(executableSchema graphql.ExecutableSchema) gin.HandlerFunc {
	srv := handler.New(executableSchema)

	// 僅添加 HTTP 相關的傳輸器
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.MultipartForm{}) // 支持文件上傳
	srv.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// allow all origins for WebSocket connections
				// should only in dev
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		InitFunc: func(ctx context.Context, initPayload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
			return ctx, &initPayload, nil
		},
	})

	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000)) // 設置查詢緩存

	srv.Use(extension.Introspection{}) // 啟用內省 (通常在開發環境啟用)
	srv.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100), // 如果需要 APQ
	})

	return func(c *gin.Context) {
		// 在這裡，你可以從 Gin 的 Context 中獲取一些請求相關的資訊
		// 並將其添加到 GraphQL 執行的 Context 中，供 Resolver 使用
		// 例如，傳遞用戶身份驗證信息：
		// userID := c.GetString("userID") // 假設你用 AuthMiddleware 將 userID 存儲為 string
		// ctx := context.WithValue(c.Request.Context(), "user_id", userID)
		// r := c.Request.WithContext(ctx)

		// 讓 gqlgen 的 handler 處理 HTTP 請求
		srv.ServeHTTP(c.Writer, c.Request)
	}
}

// GraphQLPlaygroundHandler 處理 GraphQL Playground 頁面
func GraphQLPlaygroundHandler(title string, endpoint string) gin.HandlerFunc {
	h := playground.Handler(title, endpoint)
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
