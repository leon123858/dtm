package utils

import (
	"context"
	"dtm/adapters/db/db"
)

func ResetLoader(ctx context.Context) {
	if loader, err := db.TripDataLoaderFromContext(ctx); err == nil {
		loader.Reset()
	}
}
