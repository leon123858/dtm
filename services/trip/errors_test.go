package trip

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"dtm/adapters/db/db"
	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestObjectBoundariesPreserveDomainErrors(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	for _, tc := range []struct {
		name string
		kind error
	}{
		{"trip", domain.ErrTripNotFound},
		{"user", domain.ErrUserNotFound},
		{"record", domain.ErrRecordNotFound},
		{"chain", domain.ErrInvalidChain},
		{"canceled", context.Canceled},
		{"deadline", context.DeadlineExceeded},
		{"unknown", errors.New("connection lost")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := fmt.Errorf("storage failure: %w", tc.kind)
			store := &trackingTripStore{err: cause}
			readers := staticReader(projectionReader{tripErr: cause, addressErr: cause})
			factory := NewTripFactory(store, nil, readers)
			trip := factory.ForTrip(id)
			check := func(err error) {
				t.Helper()
				require.ErrorIs(t, err, tc.kind)
				require.ErrorIs(t, err, cause)
				if tc.name == "unknown" || tc.name == "canceled" || tc.name == "deadline" {
					require.Same(t, cause, err)
				}
			}
			_, err := factory.Create(ctx, "trip")
			check(err)
			_, err = trip.Info(ctx)
			check(err)
			_, err = trip.Addresses(ctx)
			check(err)
			_, err = trip.UpdateInfo(ctx, "name")
			check(err)
			_, err = trip.CreateAddress(ctx, "member")
			check(err)
			_, err = trip.Append(ctx, &record{tripID: id, intent: intentCreate})
			check(err)
			_, err = trip.Append(ctx, &record{tripID: id, intent: intentPatch})
			check(err)
			_, err = NewRecordFactory(staticReader(factoryReader{err: cause})).ByID(ctx, id)
			check(err)
		})
	}
}

func TestAppendPassesPrivatePolicyToStore(t *testing.T) {
	store := &validatingTripStore{}
	factory := NewTripFactory(store, nil, nil)
	id := uuid.New()
	intent, err := NewRecordFactory(nil).New(context.Background(), id, domain.Record{})
	require.NoError(t, err)
	_, err = factory.ForTrip(id).Append(context.Background(), intent)
	require.ErrorIs(t, err, ErrInvalidRecordSnapshot)
}

type validatingTripStore struct{ trackingTripStore }

func (*validatingTripStore) AppendNew(_ context.Context, _ uuid.UUID, value domain.Record, materializer db.RecordMaterializer) (domain.Record, error) {
	return materializer.PrepareNew(value, nil)
}
