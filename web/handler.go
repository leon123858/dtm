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

	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.MultipartForm{})
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

	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	return func(c *gin.Context) {
		// Here you can extract request-related information from Gin's Context
		// and add it to the GraphQL execution Context for use in Resolvers.
		// For example, to pass user authentication info:
		// userID := c.GetString("userID")
		// ctx := context.WithValue(c.Request.Context(), "user_id", userID)
		// r := c.Request.WithContext(ctx)

		srv.ServeHTTP(c.Writer, c.Request)
	}
}

func GraphQLPlaygroundHandler(title string, endpoint string) gin.HandlerFunc {
	h := playground.Handler(title, endpoint)
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
