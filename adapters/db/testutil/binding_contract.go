package testutil

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

// CheckBindingContract runs against shared memory state and real PostgreSQL transactions.
func CheckBindingContract(t *testing.T, s db.TripDBWrapper, auth db.AuthDBWrapper) {
	ctx := context.Background()
	user, err := auth.UpsertUser(ctx, uuid.NewString())
	require.NoError(t, err)
	other, err := auth.UpsertUser(ctx, uuid.NewString())
	require.NoError(t, err)
	a, b := domain.WithUserID(ctx, user.ID), domain.WithUserID(ctx, other.ID)
	newTrip := func() uuid.UUID {
		id := uuid.New()
		require.NoError(t, s.CreateTrip(&domain.TripInfo{ID: id, Name: "binding", Private: true}))
		t.Cleanup(func() { _ = s.DeleteTrip(a, id); _ = s.DeleteTrip(b, id) })
		return id
	}
	id := newTrip()
	require.NoError(t, s.CheckTripAccess(ctx, id), "creation always public")
	binding, err := s.Binding(a, id)
	require.NoError(t, err)
	require.Nil(t, binding)
	_, err = s.SetTripPrivate(a, id, true)
	require.ErrorIs(t, err, domain.ErrForbidden)
	require.ErrorIs(t, s.Bind(ctx, id, nil), domain.ErrUnauthenticated)
	// A combined patch must not rename when privacy authorization fails.
	renamed, private := "updated binding", true
	_, err = s.PatchTripInfo(a, id, &renamed, &private)
	require.ErrorIs(t, err, domain.ErrForbidden)
	infos, err := s.DataLoaderGetTripInfoList(ctx, []uuid.UUID{id})
	require.NoError(t, err)
	require.Equal(t, "binding", infos[id].Name)
	require.False(t, infos[id].Private)
	require.NoError(t, s.Bind(a, id, nil))
	require.NoError(t, s.Bind(a, id, nil))
	binding, err = s.Binding(a, id)
	require.NoError(t, err)
	require.NotNil(t, binding)
	require.Nil(t, binding.Address)
	members, err := s.DataLoaderGetTripUsers(a, []uuid.UUID{id})
	require.NoError(t, err)
	require.Len(t, members[id].Users, 1)
	require.Equal(t, user.ID, members[id].Users[0].UserID)
	require.Empty(t, members[id].Addresses)
	address, err := s.CreateAddress(ctx, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("A")}})
	require.NoError(t, err)
	spare, err := s.CreateAddress(ctx, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("B")}})
	require.NoError(t, err)
	require.NoError(t, s.Bind(a, id, &address.ID))
	require.NoError(t, s.Bind(b, id, &spare.ID))
	members, err = s.DataLoaderGetTripUsers(a, []uuid.UUID{id})
	require.NoError(t, err)
	require.Len(t, members[id].Users, 2)
	require.Len(t, members[id].Addresses, 2)
	require.Equal(t, user.ID, members[id].Addresses[address.ID.String()].UserID)
	require.Equal(t, other.ID, members[id].Addresses[spare.ID.String()].UserID)

	require.ErrorIs(t, s.Bind(b, id, &address.ID), domain.ErrConflict)
	binding, err = s.Binding(b, id)
	require.NoError(t, err)
	require.Equal(t, spare.ID, binding.Address.ID)
	foreignID := newTrip()
	foreign, err := s.CreateAddress(ctx, foreignID, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("foreign")}})
	require.NoError(t, err)
	require.ErrorIs(t, s.Bind(a, id, &foreign.ID), domain.ErrBadUserInput)
	binding, err = s.Binding(a, id)
	require.NoError(t, err)
	require.Equal(t, address.ID, binding.Address.ID)
	_, err = s.DeleteAddress(ctx, id, address.ID)
	require.ErrorIs(t, err, domain.ErrConflict)
	recordID := uuid.New()
	_, err = s.AppendNew(ctx, id, domain.Record{RecordInfo: domain.RecordInfo{ID: recordID, Name: "secret", Amount: 10, PrePayAddress: *spare}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: *spare}}}}, Materializer{})
	require.NoError(t, err)
	require.NoError(t, auth.SubscribeTrip(b, other.ID, id))
	require.NoError(t, s.Unbind(b, id))
	info, err := s.PatchTripInfo(a, id, &renamed, &private)
	require.NoError(t, err)
	require.True(t, info.Private)
	require.Equal(t, renamed, info.Name)
	info, err = s.PatchTripInfo(a, id, nil, nil)
	require.NoError(t, err)
	require.Equal(t, renamed, info.Name)
	require.True(t, info.Private)
	renameOnly := "still private"
	info, err = s.PatchTripInfo(a, id, &renameOnly, nil)
	require.NoError(t, err)
	require.True(t, info.Private)
	require.Equal(t, renameOnly, info.Name)

	require.ErrorIs(t, s.CheckTripAccess(ctx, id), domain.ErrUnauthenticated)
	require.ErrorIs(t, s.CheckTripAccess(b, id), domain.ErrForbidden)
	require.ErrorIs(t, s.Bind(b, id, nil), domain.ErrForbidden)
	_, err = s.DataLoaderGetRecordList(b, []uuid.UUID{recordID})
	require.Error(t, err)
	_, _, _, err = s.AppendPatch(b, id, recordID, domain.RecordPatch{}, Materializer{})
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, err = s.AppendNew(b, id, domain.Record{}, Materializer{})
	require.ErrorIs(t, err, domain.ErrForbidden)

	require.NoError(t, s.Unbind(b, id))
	require.NoError(t, s.Unbind(b, uuid.New()))
	require.ErrorIs(t, s.Unbind(a, id), domain.ErrConflict)
	require.NoError(t, s.Bind(a, id, nil), "clearing address preserves last member")
	_, err = s.DeleteAddress(a, id, address.ID)
	require.NoError(t, err)
	_, err = s.CreateAddress(b, id, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("denied")}})
	require.ErrorIs(t, err, domain.ErrForbidden)
	require.ErrorIs(t, s.UpdateTripInfo(b, &domain.TripInfo{ID: id, Name: "denied"}), domain.ErrForbidden)
	_, err = s.DataLoaderGetTripInfoList(b, []uuid.UUID{id})
	require.Error(t, err)
	_, err = s.DataLoaderGetTripAddressList(b, []uuid.UUID{id})
	require.Error(t, err)
	_, err = s.DataLoaderGetTripRecords(b, []uuid.UUID{id}, db.RecordReadOptions{HaveHistory: true})
	require.Error(t, err)
	require.ErrorIs(t, auth.SubscribeTrip(b, other.ID, id), domain.ErrForbidden)
	require.ErrorIs(t, auth.TouchSubscribedTrip(b, other.ID, id), domain.ErrForbidden)
	list, err := auth.SubscribedTrips(b, other.ID)
	require.NoError(t, err)
	require.Len(t, list, 1, "private bookmark remains visible without access")
	_, err = s.SetTripPrivate(a, id, false)
	require.NoError(t, err)
	list, err = auth.SubscribedTrips(b, other.ID)
	require.NoError(t, err)
	require.Len(t, list, 1, "bookmark retained")
	require.NoError(t, s.Bind(b, id, nil))
	_, err = s.SetTripPrivate(a, id, true)
	require.NoError(t, err)
	// Two concurrent departures cannot remove the last member.
	errors := make(chan error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, actor := range []context.Context{a, b} {
		wg.Add(1)
		go func(actor context.Context) { defer wg.Done(); <-start; errors <- s.Unbind(actor, id) }(actor)
	}
	close(start)
	wg.Wait()
	close(errors)
	successes, conflicts := 0, 0
	for err := range errors {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, domain.ErrConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	// Concurrent address acquisition has exactly one winner.
	raceID := newTrip()
	addr, err := s.CreateAddress(ctx, raceID, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("race")}})
	require.NoError(t, err)
	errors = make(chan error, 2)
	start = make(chan struct{})
	for _, actor := range []context.Context{a, b} {
		wg.Add(1)
		go func(actor context.Context) { defer wg.Done(); <-start; errors <- s.Bind(actor, raceID, &addr.ID) }(actor)
	}
	close(start)
	wg.Wait()
	close(errors)
	successes = 0
	for err := range errors {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, domain.ErrConflict)
		}
	}
	require.Equal(t, 1, successes)
	// A privacy change racing the last departure leaves either a member or a public trip.
	for range 8 {
		raceID := newTrip()
		require.NoError(t, s.Bind(a, raceID, nil))
		errors = make(chan error, 2)
		start = make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, err := s.SetTripPrivate(a, raceID, true); errors <- err }()
		go func() { defer wg.Done(); <-start; errors <- s.Unbind(a, raceID) }()
		close(start)
		wg.Wait()
		close(errors)
		successes = 0
		for err := range errors {
			if err == nil {
				successes++
			} else {
				require.True(t, err == domain.ErrConflict || err == domain.ErrForbidden)
			}
		}
		require.Equal(t, 1, successes)
	}
}
