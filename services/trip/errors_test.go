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

func TestObjectBoundariesTranslatePersistenceErrors(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	for _, tc := range []struct {
		name              string
		storage, business error
	}{
		{"trip", db.ErrTripNotFound, ErrTripNotFound},
		{"record", db.ErrRecordNotFound, ErrRecordNotFound},
		{"chain", db.ErrInvalidChain, ErrInvalidChain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := fmt.Errorf("storage failure: %w", tc.storage)
			store := &trackingTripStore{err: cause}
			readers := staticReader(projectionReader{tripErr: cause, addressErr: cause})
			factory := NewTripFactory(store, readers)
			trip := factory.ForTrip(id)
			check := func(err error) {
				t.Helper()
				require.ErrorIs(t, err, tc.business)
				require.ErrorIs(t, err, cause)
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

func TestStoreErrorPreservesBusinessAndUnknownErrors(t *testing.T) {
	for _, err := range []error{nil, context.Canceled, errors.New("connection lost"), ErrInvalidRecordSnapshot} {
		require.Equal(t, err, fromStoreError(err))
	}
	err := fromStoreError(db.ErrTripNotFound)
	require.Same(t, err, fromStoreError(err))
}

func TestAppendPassesPrivatePolicyToStore(t *testing.T) {
	store := &validatingTripStore{}
	factory := NewTripFactory(store, nil)
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
