package pg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"dtm/adapters/db/testutil"
	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBindingContract(t *testing.T) {
	s, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	testutil.CheckBindingContract(t, s, NewPgAuthDBWrapper(s.(*pgTripDBWrapper).db))
}

// A writer queued behind a privacy change must authorize the committed state,
// even if the trip was public when the request began.
func TestWriteWaitsForPrivateChange(t *testing.T) {
	store, cleanup, id, _, _ := setupTrip(t)
	defer cleanup()
	p := store.(*pgTripDBWrapper)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	users := NewPgAuthDBWrapper(p.db)
	user, err := users.UpsertUser(ctx, uuid.NewString())
	require.NoError(t, err)
	owner := domain.WithUserID(ctx, user.ID)
	require.NoError(t, store.Bind(owner, id, nil))
	privacy := p.db.WithContext(ctx).Begin()
	require.NoError(t, privacy.Error)
	defer privacy.Rollback()
	require.NoError(t, lockAccess(privacy, owner, id, tripExclusive))
	require.NoError(t, privacy.Model(&TripInfoModel{}).Where("id = ?", id).Update("private", true).Error)
	pids := make(chan int, 1)
	require.NoError(t, p.db.Callback().Query().Before("gorm:query").Register("test:binding_wait", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(waitingWriterKey{}) == true {
			var pid int
			if tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error == nil {
				select {
				case pids <- pid:
				default:
				}
			}
		}
	}))
	defer p.db.Callback().Query().Remove("test:binding_wait")
	done := make(chan error, 1)
	go func() {
		_, err := store.CreateAddress(context.WithValue(ctx, waitingWriterKey{}, true), id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("denied")}})
		done <- err
	}()
	var pid int
	select {
	case pid = <-pids:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Eventually(t, func() bool {
		var blocked bool
		return p.db.Raw("SELECT cardinality(pg_blocking_pids(?)) > 0", pid).Scan(&blocked).Error == nil && blocked
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, privacy.Commit().Error)
	require.ErrorIs(t, <-done, domain.ErrUnauthenticated)
	addresses, err := store.DataLoaderGetTripAddressList(owner, []uuid.UUID{id})
	require.NoError(t, err)
	require.Len(t, addresses[id], 2)
}

func TestDeleteAddressPreservesMetadata(t *testing.T) {
	s, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	for _, useNickname := range []bool{false, true} {
		t.Run(fmt.Sprint(useNickname), func(t *testing.T) {
			tripID := uuid.New()
			require.NoError(t, s.CreateTrip(&domain.TripInfo{ID: tripID, Name: "deletion snapshot"}))
			t.Cleanup(func() { require.NoError(t, s.DeleteTrip(ctx, tripID)) })
			name := "participant"
			address, err := s.CreateAddress(ctx, tripID, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: &name, UseNickname: &useNickname}, BindToMe: false})
			require.NoError(t, err)
			deleted, err := s.DeleteAddress(ctx, tripID, address.ID)
			require.NoError(t, err)
			require.Equal(t, address, deleted)
		})
	}
}

func TestProfileContract(t *testing.T) {
	s, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	testutil.CheckProfileContract(t, s, NewPgAuthDBWrapper(s.(*pgTripDBWrapper).db))
}
