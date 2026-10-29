package testutil

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
)

func profilePtr[T any](v T) *T { return &v }
func CheckProfileContract(t *testing.T, s db.TripDBWrapper, users db.AuthDBWrapper) {
	ctx := context.Background()
	u, e := users.UpsertUser(ctx, uuid.NewString(), "first@example.com")
	require.NoError(t, e)
	require.Equal(t, "first@example.com", *u.Email)
	require.Nil(t, u.Name)
	require.False(t, u.SubMail)
	u, e = users.UpdateUser(ctx, u.ID, profilePtr("小明"), profilePtr(true))
	require.NoError(t, e)
	next, e := users.UpsertUser(ctx, u.GCID, "new@example.com")
	require.NoError(t, e)
	require.Equal(t, u.ID, next.ID)
	require.Equal(t, "小明", *next.Name)
	require.True(t, next.SubMail)
	next, e = users.UpsertUser(ctx, u.GCID, "")
	require.NoError(t, e)
	require.Equal(t, "new@example.com", *next.Email)
	next, e = users.UpdateUser(ctx, u.ID, nil, profilePtr(false))
	require.NoError(t, e)
	require.False(t, next.SubMail)
	_, e = users.UpdateUser(ctx, u.ID, profilePtr(" \t"), profilePtr(true))
	require.ErrorIs(t, e, domain.ErrBadUserInput)
	next, e = users.GetUser(ctx, u.ID)
	require.NoError(t, e)
	require.False(t, next.SubMail)
	require.Equal(t, "小明", *next.Name)
	unnamed, e := users.UpsertUser(ctx, uuid.NewString())
	require.NoError(t, e)
	_, e = users.UpdateUser(ctx, unnamed.ID, nil, profilePtr(true))
	require.ErrorIs(t, e, domain.ErrBadUserInput)
	// A nameless member may create with an explicit original, but missing input is rejected atomically.
	emptyTrip := uuid.New()
	require.NoError(t, s.CreateTrip(&domain.TripInfo{ID: emptyTrip, Name: "empty"}))
	// Missing users must not leave an address behind when the binding FK rejects the write.
	missingCtx := domain.WithUserID(ctx, uuid.New())
	for _, name := range []*string{nil, profilePtr("orphan")} {
		_, err := s.CreateAddress(missingCtx, emptyTrip, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: name, UseNickname: nil}, BindToMe: true})
		require.ErrorIs(t, err, domain.ErrUserNotFound)
		remaining, err := s.DataLoaderGetTripAddressList(ctx, []uuid.UUID{emptyTrip})
		require.NoError(t, err)
		require.Empty(t, remaining[emptyTrip])
	}
	emptyCtx := domain.WithUserID(ctx, unnamed.ID)
	_, e = s.CreateAddress(emptyCtx, emptyTrip, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: nil, UseNickname: nil}, BindToMe: true})
	require.ErrorIs(t, e, domain.ErrBadUserInput)
	addresses, e := s.DataLoaderGetTripAddressList(emptyCtx, []uuid.UUID{emptyTrip})
	require.NoError(t, e)
	require.Empty(t, addresses[emptyTrip])
	claimed, e := s.CreateAddress(emptyCtx, emptyTrip, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: profilePtr("Guest"), UseNickname: nil}, BindToMe: true})
	require.NoError(t, e)
	require.Equal(t, "Guest", claimed.Name)
	// Explicit names rely on uniqueness within the trip; failure preserves the existing address.
	_, err := s.CreateAddress(ctx, emptyTrip, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: profilePtr("Guest"), UseNickname: nil}, BindToMe: false})
	require.ErrorIs(t, err, domain.ErrConflict)
	duplicates, err := s.DataLoaderGetTripAddressList(ctx, []uuid.UUID{emptyTrip})
	require.NoError(t, err)
	require.Len(t, duplicates[emptyTrip], 1)

	subscriptions, e := users.SubscribedTrips(emptyCtx, unnamed.ID)
	require.NoError(t, e)
	require.Empty(t, subscriptions)
	_, e = s.SetTripPrivate(emptyCtx, emptyTrip, true)
	require.NoError(t, e)
	_, e = s.CreateAddress(domain.WithUserID(ctx, u.ID), emptyTrip, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: profilePtr("denied"), UseNickname: nil}, BindToMe: true})
	require.ErrorIs(t, e, domain.ErrForbidden)
	_, e = s.CreateAddress(ctx, emptyTrip, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: profilePtr("denied"), UseNickname: nil}, BindToMe: false})
	require.ErrorIs(t, e, domain.ErrUnauthenticated)
	tid := uuid.New()
	require.NoError(t, s.CreateTrip(&domain.TripInfo{ID: tid, Name: "profile"}))
	owner := domain.WithUserID(ctx, u.ID)
	_, e = s.CreateAddress(ctx, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: nil, UseNickname: nil}, BindToMe: false})
	require.ErrorIs(t, e, domain.ErrBadUserInput)
	_, e = s.CreateAddress(ctx, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: profilePtr("A"), UseNickname: nil}, BindToMe: true})
	require.ErrorIs(t, e, domain.ErrUnauthenticated)
	collision, e := s.CreateAddress(ctx, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("小明")}})
	require.NoError(t, e)
	require.NoError(t, s.Bind(owner, tid, nil))
	const count = 12
	ids := make(chan uuid.UUID, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := s.CreateAddress(owner, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: nil, UseNickname: nil}, BindToMe: true})
			errs <- e
			if e == nil {
				ids <- a.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		} else {
			require.ErrorIs(t, e, domain.ErrConflict)
		}
	}
	require.Equal(t, 1, successes)
	var aid uuid.UUID
	for id := range ids {
		if aid == uuid.Nil {
			aid = id
		}
		require.Equal(t, aid, id)
	}
	list, e := s.DataLoaderGetTripAddressList(owner, []uuid.UUID{tid})
	require.NoError(t, e)
	require.Len(t, list[tid], 2)
	_, e = s.CreateAddress(owner, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: profilePtr("another"), UseNickname: profilePtr(true)}, BindToMe: true})
	require.ErrorIs(t, e, domain.ErrConflict)
	unchanged, e := s.DataLoaderGetTripAddressList(owner, []uuid.UUID{tid})
	require.NoError(t, e)
	require.Len(t, unchanged[tid], 2)
	a, c, e := s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: nil, UseNickname: profilePtr(true)})
	require.NoError(t, e)
	require.True(t, c)
	require.Equal(t, "小明 (2)", a.Name)
	a, c, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: nil, UseNickname: nil})
	require.NoError(t, e)
	require.False(t, c)
	require.True(t, a.UseNickname)
	_, _, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: profilePtr(collision.Name), UseNickname: profilePtr(false)})
	require.Error(t, e)
	a, _, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: nil, UseNickname: nil})
	require.NoError(t, e)
	require.True(t, a.UseNickname)
	require.Equal(t, "小明 (2)", a.Name)
	a, _, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: profilePtr("原名"), UseNickname: profilePtr(false)})
	require.NoError(t, e)
	require.Equal(t, "原名", a.Name)

	// Equal values and empty options still validate access and address existence.
	same := domain.AddressOption{Name: profilePtr("原名"), UseNickname: profilePtr(false)}
	a, c, e = s.UpdateAddress(owner, tid, aid, same)
	require.NoError(t, e)
	require.False(t, c)
	require.False(t, a.UseNickname)
	require.Equal(t, "原名", *same.Name)
	require.False(t, *same.UseNickname)
	_, c, e = s.UpdateAddress(owner, tid, uuid.New(), domain.AddressOption{})
	require.ErrorIs(t, e, domain.ErrAddressNotFound)
	require.False(t, c)
	_, c, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: profilePtr(" ")})
	require.ErrorIs(t, e, domain.ErrBadUserInput)
	require.False(t, c)
	_, c, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: profilePtr(collision.Name)})
	require.ErrorIs(t, e, domain.ErrConflict)
	require.False(t, c)
	root := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "meal", Amount: 30, Time: time.Now().Truncate(time.Millisecond), PrePayAddress: domain.Address{ID: aid}}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: domain.Address{ID: aid}, ExtendMsg: 2}}}}
	written, e := s.AppendNew(owner, tid, root, Materializer{})
	require.NoError(t, e)
	require.Equal(t, "原名", written.PrePayAddress.Name)
	_, e = users.UpdateUser(ctx, u.ID, profilePtr("新名"), nil)
	require.NoError(t, e)
	binding, e := s.Binding(owner, tid)
	require.NoError(t, e)
	require.Equal(t, "原名", binding.Address.Name)
	records, e := s.DataLoaderGetRecordList(owner, []uuid.UUID{root.ID})
	require.NoError(t, e)
	require.Equal(t, "原名", records[root.ID].PrePayAddress.Name)
	require.Equal(t, "原名", records[root.ID].ShouldPayAddress[0].Address.Name)
	require.Equal(t, root.Amount, records[root.ID].Amount)
	require.Nil(t, records[root.ID].ParentRecordID)
	require.Nil(t, records[root.ID].ChildRecordID)
	_, _, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: nil, UseNickname: profilePtr(true)})
	require.NoError(t, e)
	history, e := s.DataLoaderGetTripRecords(owner, []uuid.UUID{tid}, db.RecordReadOptions{HaveHistory: true})
	require.NoError(t, e)
	require.Equal(t, "原名", history[tid][0].PrePayAddress.Name)
	require.True(t, history[tid][0].PrePayAddress.UseNickname)
	_, _, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: nil, UseNickname: profilePtr(false)})
	require.NoError(t, e)
	// No name is required to claim an existing address, and duplicate user names are allowed.
	other := domain.WithUserID(ctx, unnamed.ID)
	require.NoError(t, s.Bind(other, tid, &collision.ID))
	binding, e = s.Binding(other, tid)
	require.NoError(t, e)
	require.Equal(t, "小明", binding.Address.Name)
	_, e = users.UpdateUser(ctx, unnamed.ID, profilePtr("新名"), nil)
	require.NoError(t, e)
	binding, e = s.Binding(other, tid)
	require.NoError(t, e)
	require.Equal(t, "小明", binding.Address.Name)
	require.NoError(t, s.Unbind(owner, tid))
	a, _, e = s.UpdateAddress(owner, tid, aid, domain.AddressOption{Name: nil, UseNickname: nil})
	require.NoError(t, e)
	require.Equal(t, "原名", a.Name)
	require.False(t, a.UseNickname)
	// Auto-created originals are truncated by code points before adding suffixes.
	long := strings.Repeat("中", 100)
	_, e = users.UpdateUser(ctx, u.ID, &long, nil)
	require.NoError(t, e)
	_, e = s.CreateAddress(ctx, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr(long)}})
	require.NoError(t, e)
	a, e = s.CreateAddress(owner, tid, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: nil, UseNickname: profilePtr(true)}, BindToMe: true})
	require.NoError(t, e)
	require.Equal(t, strings.Repeat("中", 96)+" (2)", a.Name)
}
