package domain

import (
	"context"
	"github.com/google/uuid"
)

type userContextKey struct{}

func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userContextKey{}, userID)
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(userContextKey{}).(uuid.UUID)
	return userID, ok && userID != uuid.Nil
}
