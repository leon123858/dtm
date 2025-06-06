package utils

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
)

type GinContextKey string

const GinContextKeyValue GinContextKey = "GIN_CTX_KEY"

func GinContextFromContext(ctx context.Context) (*gin.Context, error) {
	ginContext := ctx.Value(GinContextKeyValue)
	if ginContext == nil {
		err := fmt.Errorf("could not retrieve gin.Context")
		return nil, err
	}

	gc, ok := ginContext.(*gin.Context)
	if !ok {
		err := fmt.Errorf("gin.Context has wrong type")
		return nil, err
	}
	return gc, nil
}
