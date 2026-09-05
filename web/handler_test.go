package web

import (
	"context"
	"errors"
	"testing"

	"dtm/db/db"
	"dtm/db/mem"
	"dtm/domain"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetTripDataLoaderAfterMutation(t *testing.T) {
	tests := []struct {
		name        string
		object      string
		resolverErr error
		wantBatches int64
	}{
		{name: "query keeps cache", object: "Query", wantBatches: 1},
		{name: "successful mutation resets cache", object: "Mutation", wantBatches: 2},
		{name: "failed mutation resets cache", object: "Mutation", resolverErr: errors.New("mutation failed"), wantBatches: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := mem.NewInMemoryTripDBWrapper()
			tripID := uuid.New()
			require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "trip"}))
			loader := db.NewTripDataLoader(store)
			ctx := db.WithTripDataLoader(context.Background(), loader)
			ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{Object: tt.object})

			db.DataLoaderDebug.Reset()
			_, err := loader.LoadTrip(ctx, tripID)
			require.NoError(t, err)
			_, err = loader.LoadTrip(ctx, tripID)
			require.NoError(t, err)
			assert.Equal(t, db.DataLoadCount{Batches: 1, Keys: 1}, db.DataLoaderDebug.Snapshot().Trips)

			_, err = resetTripDataLoaderAfterMutation(ctx, func(context.Context) (any, error) {
				return nil, tt.resolverErr
			})
			require.ErrorIs(t, err, tt.resolverErr)

			_, err = loader.LoadTrip(ctx, tripID)
			require.NoError(t, err)
			assert.Equal(t, db.DataLoadCount{Batches: tt.wantBatches, Keys: tt.wantBatches}, db.DataLoaderDebug.Snapshot().Trips)
		})
	}
}
