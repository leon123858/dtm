package mem

import (
	"context"
	"dtm/adapters/db/internal/testutil"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAuthContract(t *testing.T) {
	s := NewInMemoryAuthDBWrapper()
	testutil.CheckAuthContract(t, s, s)
}

func TestUserContract(t *testing.T) {
	state := newMemoryState()
	testutil.CheckUserContract(t, &inMemoryAuthDBWrapper{state}, &inMemoryAuthDBWrapper{state}, &inMemoryTripDBWrapper{state})
}

func TestTouchPreservesFutureTimestamp(t *testing.T) {
	state := newMemoryState()
	store := &inMemoryAuthDBWrapper{state}
	ctx := context.Background()
	user, err := store.UpsertUser(ctx, "future")
	require.NoError(t, err)
	trip := &domain.TripInfo{ID: uuid.New(), Name: "future"}
	require.NoError(t, store.CreateSubscribedTrip(ctx, user.ID, trip))
	future := time.Now().UTC().Add(time.Hour)
	state.subscriptions[user.ID][trip.ID] = future
	require.NoError(t, store.TouchSubscribedTrip(ctx, user.ID, trip.ID))
	list, err := store.SubscribedTrips(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, list[0].LastViewedAt.Equal(future))
}
