package testutil

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func CheckAuthContract(t *testing.T, a, b db.AuthDBWrapper) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	member, err := a.UpsertUser(ctx, uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, a.PutLock(ctx, "transaction", "key", "binding", []byte("secret"), now.Add(time.Minute)))
	require.Error(t, a.PutLock(ctx, "transaction", "key", "binding", []byte("replacement"), now.Add(time.Minute)))
	_, err = b.TakeLock(ctx, "transaction", "key", "wrong", now)
	require.ErrorIs(t, err, db.ErrInvalidAuthEntry)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, e := b.TakeLock(ctx, "transaction", "key", "binding", now)
			if e == nil {
				require.Equal(t, []byte("secret"), data)
				wins.Add(1)
			} else if e != db.ErrInvalidAuthEntry {
				t.Errorf("unexpected store failure: %v", e)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), wins.Load())
	require.NoError(t, a.PutLock(ctx, "login", "expired", "", []byte("token"), now))
	_, err = b.TakeLock(ctx, "login", "expired", "", now)
	require.ErrorIs(t, err, db.ErrInvalidAuthEntry)
	require.NoError(t, a.CleanupLock(ctx, now))
	found, err := b.UserGCID2UserId(ctx, member.GCID)
	require.NoError(t, err)
	require.Equal(t, member.ID, found, "lock cleanup must preserve permanent users")
	require.NoError(t, a.PutLock(ctx, "login", "expired", "", []byte("new"), now.Add(time.Minute)))
	data, err := b.TakeLock(ctx, "login", "expired", "", now)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), data)
}

// CheckUserContract runs against shared memory or independent PostgreSQL connections.
func CheckUserContract(t *testing.T, store, other db.AuthDBWrapper, trips db.TripDBWrapper) {
	ctx := context.Background()
	_, err := store.UpsertUser(ctx, " ")
	require.ErrorIs(t, err, db.ErrInvalidGCID)
	gcid := uuid.NewString()
	u, err := store.UpsertUser(ctx, gcid)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, u.ID)
	require.Equal(t, gcid, u.GCID)
	resolved, err := other.UserGCID2UserId(ctx, gcid)
	require.NoError(t, err)
	require.Equal(t, u.ID, resolved)
	_, err = other.UserGCID2UserId(ctx, "missing")
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	again, err := other.UpsertUser(ctx, gcid)
	require.NoError(t, err)
	require.Equal(t, u, again)
	var wg sync.WaitGroup
	concurrentGCID := uuid.NewString()
	results := make(chan domain.User, 32)
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := other.UpsertUser(ctx, concurrentGCID); results <- v; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	var concurrentID uuid.UUID
	for v := range results {
		if concurrentID == uuid.Nil {
			concurrentID = v.ID
		}
		require.Equal(t, concurrentID, v.ID)
	}
	v, err := store.UpsertUser(ctx, uuid.NewString())
	require.NoError(t, err)
	list, err := store.SubscribedTrips(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, list)
	require.Empty(t, list)
	a := &domain.TripInfo{ID: uuid.New(), Name: "first"}
	b := &domain.TripInfo{ID: uuid.New(), Name: "second"}
	require.NoError(t, trips.CreateTrip(a))
	require.NoError(t, store.CreateSubscribedTrip(ctx, u.ID, b))
	require.NoError(t, store.SubscribeTrip(ctx, u.ID, a.ID))
	require.NoError(t, other.SubscribeTrip(ctx, u.ID, a.ID))
	list, err = store.SubscribedTrips(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Less(t, list[0].ID.String(), list[1].ID.String())
	initial := map[uuid.UUID]time.Time{}
	for _, subscription := range list {
		require.False(t, subscription.LastViewedAt.IsZero())
		initial[subscription.ID] = subscription.LastViewedAt
	}
	require.NoError(t, other.SubscribeTrip(ctx, u.ID, a.ID))
	unchanged, err := store.SubscribedTrips(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, list, unchanged)
	require.ErrorIs(t, other.TouchSubscribedTrip(ctx, v.ID, a.ID), domain.ErrSubscriptionNotFound)
	require.ErrorIs(t, store.TouchSubscribedTrip(ctx, u.ID, uuid.New()), domain.ErrTripNotFound)
	require.NoError(t, other.SubscribeTrip(ctx, v.ID, a.ID))
	isolated, err := other.SubscribedTrips(ctx, v.ID)
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, other.TouchSubscribedTrip(ctx, u.ID, a.ID))
	touched, err := store.SubscribedTrips(ctx, u.ID)
	require.NoError(t, err)
	for _, subscription := range touched {
		if subscription.ID == a.ID {
			require.True(t, subscription.LastViewedAt.After(initial[a.ID]))
		} else {
			require.True(t, subscription.LastViewedAt.Equal(initial[b.ID]))
		}
	}
	isolatedAfter, err := other.SubscribedTrips(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, isolated, isolatedAfter)
	require.NoError(t, other.UnsubscribeTrip(ctx, v.ID, a.ID))

	empty, err := other.SubscribedTrips(ctx, v.ID)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.ErrorIs(t, store.SubscribeTrip(ctx, u.ID, uuid.New()), domain.ErrTripNotFound)
	require.NoError(t, store.UnsubscribeTrip(ctx, u.ID, uuid.New()))
	require.NoError(t, store.UnsubscribeTrip(ctx, u.ID, a.ID))
	require.NoError(t, other.UnsubscribeTrip(ctx, u.ID, a.ID))
	require.NoError(t, store.SubscribeTrip(ctx, u.ID, a.ID))

	resubscribed, err := store.SubscribedTrips(ctx, u.ID)
	require.NoError(t, err)
	for _, subscription := range resubscribed {
		if subscription.ID == a.ID {
			require.True(t, subscription.LastViewedAt.After(initial[a.ID]))
		}
	}
	require.NoError(t, trips.DeleteTrip(context.Background(), a.ID))
	require.ErrorIs(t, store.TouchSubscribedTrip(ctx, u.ID, a.ID), domain.ErrTripNotFound)

	list, err = store.SubscribedTrips(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, b.ID, list[0].ID)
	require.Equal(t, b.Name, list[0].Name)
	failed := &domain.TripInfo{ID: uuid.New(), Name: "rollback"}
	require.ErrorIs(t, store.CreateSubscribedTrip(ctx, uuid.New(), failed), domain.ErrUserNotFound)
	infos, _ := trips.DataLoaderGetTripInfoList(ctx, []uuid.UUID{failed.ID})
	require.Nil(t, infos[failed.ID])
	require.Error(t, store.CreateSubscribedTrip(ctx, u.ID, b))
	require.NoError(t, store.SubscribeTrip(ctx, v.ID, b.ID))
	require.NoError(t, trips.DeleteTrip(context.Background(), b.ID))
	list, err = store.SubscribedTrips(ctx, v.ID)
	require.NoError(t, err)
	require.Empty(t, list)
}
