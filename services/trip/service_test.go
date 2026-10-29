package trip

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/domain"
	"dtm/libs/chainlist"
	"dtm/libs/recordpatch"
	"dtm/libs/textvalidate"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPayment(id uuid.UUID, payer, member domain.Address) domain.Record {
	return domain.Record{RecordInfo: domain.RecordInfo{ID: id, Name: "meal", Amount: 20, Time: time.Unix(1, 0), PrePayAddress: payer, Category: domain.CategoryNormal}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: member, ExtendMsg: 10}}}}
}

func TestRecordPolicyApplyPatchFieldsDeletionAndDeepCopy(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	tail := testPayment(uuid.New(), domain.Address{ID: payer.ID, Name: "stale"}, domain.Address{ID: member.ID, Name: "stale"})
	name, deleted := "dinner", true
	should := []domain.ExtendAddress{{Address: domain.Address{ID: payer.ID}, ExtendMsg: 20}}
	merged, changed, err := (recordPolicy{}).ApplyPatch(tail, policyTestPatch(t, tail, func(next *domain.Record) { next.Name = name; next.IsDeleted = deleted; next.ShouldPayAddress = should }), []domain.Address{payer, member})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "dinner", merged.Name)
	assert.True(t, merged.IsDeleted)
	assert.Equal(t, payer, merged.PrePayAddress)
	assert.Equal(t, payer, merged.ShouldPayAddress[0].Address)
	should[0].ExtendMsg = 99
	assert.Equal(t, float64(20), merged.ShouldPayAddress[0].ExtendMsg)
	assert.Equal(t, "meal", tail.Name)

	unchanged, changed, err := (recordPolicy{}).ApplyPatch(merged, domain.RecordPatch{}, []domain.Address{payer, member})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, merged, unchanged)
}

func TestRecordPolicyApplyPatchRejectsForeignAddress(t *testing.T) {
	payer := domain.Address{ID: uuid.New()}
	member := domain.Address{ID: uuid.New()}
	tail := testPayment(uuid.New(), payer, member)
	foreign := uuid.New()
	_, _, err := (recordPolicy{}).ApplyPatch(tail, policyTestPatch(t, tail, func(next *domain.Record) { next.PrePayAddress.ID = foreign }), []domain.Address{payer, member})
	assert.ErrorIs(t, err, ErrInvalidRecordAddresses)
}

type factoryReader struct {
	node db.RecordSnapshot
	err  error
}

func (factoryReader) LoadTrip(context.Context, uuid.UUID) (*domain.TripInfo, error) {
	return nil, nil
}
func (r factoryReader) LoadRecord(context.Context, uuid.UUID) (db.RecordSnapshot, error) {
	return r.node, r.err
}
func (factoryReader) LoadTripRecords(context.Context, uuid.UUID, db.RecordReadOptions) ([]db.RecordSnapshot, error) {
	return nil, nil
}
func (factoryReader) LoadTripAddresses(context.Context, uuid.UUID) ([]domain.Address, error) {
	return nil, nil
}

func TestUpdateOnlyResolvesTargetIdentity(t *testing.T) {
	target, tripID := uuid.New(), uuid.New()
	intent, err := NewRecordFactory(staticReader(factoryReader{node: db.RecordSnapshot{TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: target}}}})).Update(context.Background(), target, domain.RecordPatch{})
	require.NoError(t, err)
	assert.Equal(t, tripID, intent.TripID())
	assert.Equal(t, uuid.Nil, intent.ID())

	_, err = NewRecordFactory(staticReader(factoryReader{err: fmt.Errorf("%w: missing", chainlist.ErrNodeNotFound)})).Update(context.Background(), target, domain.RecordPatch{})
	require.ErrorIs(t, err, domain.ErrRecordNotFound)
	assert.ErrorIs(t, err, chainlist.ErrNodeNotFound)
}

type trackingTripStore struct {
	db.BindingStore
	created       *domain.TripInfo
	updated       *domain.TripInfo
	createdAt     uuid.UUID
	updatedAt     [2]uuid.UUID
	deletedAt     [2]uuid.UUID
	storedAddress domain.Address
	createOption  domain.CreateAddressOption
	updateOption  domain.AddressOption
	err           error
}

func (s *trackingTripStore) AppendNew(context.Context, uuid.UUID, domain.Record, db.RecordMaterializer) (domain.Record, error) {
	return domain.Record{}, s.err
}
func (s *trackingTripStore) AppendPatch(context.Context, uuid.UUID, uuid.UUID, domain.RecordPatch, db.RecordMaterializer) (uuid.UUID, domain.Record, bool, error) {
	return uuid.Nil, domain.Record{}, false, s.err
}
func (s *trackingTripStore) CreateTrip(info *domain.TripInfo) error {
	copy := *info
	s.created = &copy
	return s.err
}
func (s *trackingTripStore) UpdateTripInfo(ctx context.Context, info *domain.TripInfo) error {
	copy := *info
	s.updated = &copy
	return s.err
}
func (s *trackingTripStore) CreateAddress(ctx context.Context, tripID uuid.UUID, option domain.CreateAddressOption) (*domain.Address, error) {
	s.createOption = option
	name := *option.Name
	s.createdAt = tripID
	s.storedAddress = domain.Address{ID: uuid.New(), Name: name}
	return &s.storedAddress, s.err
}
func (s *trackingTripStore) UpdateAddress(ctx context.Context, tripID, addressID uuid.UUID, option domain.AddressOption) (*domain.Address, bool, error) {
	s.updateOption = option
	name := *option.Name
	s.updatedAt = [2]uuid.UUID{tripID, addressID}
	s.storedAddress = domain.Address{ID: addressID, Name: name}
	return &s.storedAddress, s.err == nil, s.err
}
func (s *trackingTripStore) DeleteAddress(ctx context.Context, tripID, addressID uuid.UUID) (*domain.Address, error) {
	s.deletedAt = [2]uuid.UUID{tripID, addressID}
	s.storedAddress = domain.Address{ID: addressID, Name: "deleted"}
	return &s.storedAddress, s.err
}

func TestTripFactoryAndTripOwnTripMutations(t *testing.T) {
	store := &trackingTripStore{}
	factory := NewTripFactory(store, nil, nil)
	trip, err := factory.Create(context.Background(), "holiday")
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, trip.ID())
	require.Equal(t, &domain.TripInfo{ID: trip.ID(), Name: "holiday"}, store.created)

	info, err := trip.UpdateInfo(context.Background(), "updated")
	require.NoError(t, err)
	assert.Equal(t, &domain.TripInfo{ID: trip.ID(), Name: "updated"}, info)
	assert.Equal(t, info, store.updated)

	useNickname := true
	option := domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("member"), UseNickname: &useNickname}, BindToMe: true}
	created, err := trip.CreateAddress(context.Background(), option)
	assert.Equal(t, option, store.createOption)
	require.NoError(t, err)
	assert.Equal(t, trip.ID(), store.createdAt)
	created.Name = "caller mutation"
	assert.Equal(t, "member", store.storedAddress.Name)

	addressID := created.ID
	useNickname = false
	update := domain.AddressOption{Name: addressNamePtr("renamed"), UseNickname: &useNickname}
	updated, changed, err := trip.UpdateAddress(context.Background(), addressID, update)
	assert.True(t, changed)
	assert.Equal(t, update, store.updateOption)
	require.NoError(t, err)
	assert.Equal(t, [2]uuid.UUID{trip.ID(), addressID}, store.updatedAt)
	assert.Equal(t, "renamed", updated.Name)
	updated.Name = "caller mutation"
	assert.Equal(t, "renamed", store.storedAddress.Name)

	deleted, err := trip.DeleteAddress(context.Background(), addressID)
	require.NoError(t, err)
	assert.Equal(t, [2]uuid.UUID{trip.ID(), addressID}, store.deletedAt)
	assert.Equal(t, "deleted", deleted.Name)
	deleted.Name = "caller mutation"
	assert.Equal(t, "deleted", store.storedAddress.Name)
}

func TestTripMutationsRejectUnavailableStoreAndCanceledContext(t *testing.T) {
	_, err := NewTripFactory(nil, nil, nil).Create(context.Background(), "trip")
	assert.EqualError(t, err, "trip store is not available")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &trackingTripStore{}
	_, err = NewTripFactory(store, nil, nil).Create(ctx, "trip")
	assert.ErrorIs(t, err, context.Canceled)
	_, err = NewTripFactory(store, nil, nil).ForTrip(uuid.New()).CreateAddress(ctx, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("member")}})
	assert.ErrorIs(t, err, context.Canceled)
}

func staticReader(reader db.Reader) db.ReaderProvider {
	return func(context.Context) (db.Reader, error) { return reader, nil }
}

func policyTestPatch(t *testing.T, old domain.Record, edit func(*domain.Record)) domain.RecordPatch {
	t.Helper()
	next := cloneDomainRecord(old)
	edit(&next)
	patch, err := recordpatch.Diff(old.EditableFields(), next.EditableFields())
	require.NoError(t, err)
	return patch
}

func TestUpdateIntentOwnsPatch(t *testing.T) {
	target, tripID := uuid.New(), uuid.New()
	old := domain.RecordFields{ShouldPayAddress: domain.RecordShares{{AddressID: uuid.NewString()}}}
	next := domain.RecordFields{ShouldPayAddress: domain.RecordShares{{AddressID: old.ShouldPayAddress[0].AddressID, ExtendMsg: 20}}}
	patch, err := recordpatch.Diff(old, next)
	require.NoError(t, err)
	intent, err := NewRecordFactory(staticReader(factoryReader{node: db.RecordSnapshot{TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: target}}}})).Update(context.Background(), target, patch)
	require.NoError(t, err)
	patch.Changes[0].Path[0] = "bad"
	patch.Changes[0].To = float64(99)
	stored := intent.(*record).patch
	assert.Equal(t, []string{"ShouldPayAddress", old.ShouldPayAddress[0].AddressID}, stored.Changes[0].Path)
	assert.Equal(t, next.ShouldPayAddress[0], stored.Changes[0].To)
}

type trackingAuthStore struct {
	db.AuthDBWrapper
	calls   int
	userID  uuid.UUID
	created domain.TripInfo
	err     error
}

func (s *trackingAuthStore) CreateSubscribedTrip(_ context.Context, userID uuid.UUID, info *domain.TripInfo) error {
	s.calls++
	s.userID = userID
	s.created = *info
	return s.err
}

func TestCreateTripSelectsAtomicStoreFromIdentity(t *testing.T) {
	failure := errors.New("transaction failed")
	for _, tc := range []struct {
		name         string
		member       bool
		missingStore bool
		canceled     bool
		invalidName  bool
		storeErr     error
	}{
		{name: "anonymous"},
		{name: "member", member: true},
		{name: "missing auth store", member: true, missingStore: true},
		{name: "deleted user", member: true, storeErr: domain.ErrUserNotFound},
		{name: "transaction failure", member: true, storeErr: failure},
		{name: "canceled", member: true, canceled: true},
		{name: "invalid member name", member: true, invalidName: true},
		{name: "invalid anonymous name", invalidName: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tripStore := &trackingTripStore{}
			authStore := &trackingAuthStore{err: tc.storeErr}
			var authDB db.AuthDBWrapper = authStore
			if tc.missingStore {
				authDB = nil
			}
			ctx := context.Background()
			userID := uuid.New()
			if tc.member {
				ctx = domain.WithUserID(ctx, userID)
			}
			if tc.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			name := "holiday"
			if tc.invalidName {
				name = ""
			}
			trip, err := NewTripFactory(tripStore, authDB, nil).Create(ctx, name)
			if tc.missingStore || tc.canceled || tc.invalidName || tc.storeErr != nil {
				require.Error(t, err)
				require.Nil(t, trip)
				require.Nil(t, tripStore.created, "must not fall back to anonymous creation")
				if tc.storeErr != nil {
					require.ErrorIs(t, err, tc.storeErr)
					require.Equal(t, 1, authStore.calls)
				} else {
					require.Zero(t, authStore.calls)
				}
				if tc.invalidName {
					require.ErrorIs(t, err, textvalidate.ErrEmptyName)
					require.EqualError(t, err, "trip name "+textvalidate.ErrEmptyName.Error())
				}
				if tc.canceled {
					require.ErrorIs(t, err, context.Canceled)
				}
				return
			}
			require.NoError(t, err)
			if tc.member {
				require.Nil(t, tripStore.created)
				require.Equal(t, 1, authStore.calls)
				require.Equal(t, userID, authStore.userID)
				require.Equal(t, domain.TripInfo{ID: trip.ID(), Name: name}, authStore.created)
			} else {
				require.Zero(t, authStore.calls)
				require.Equal(t, &domain.TripInfo{ID: trip.ID(), Name: name}, tripStore.created)
			}
		})
	}
}

func (s *trackingTripStore) CheckTripAccess(context.Context, uuid.UUID) error { return nil }

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
			_, err = trip.CreateAddress(ctx, domain.CreateAddressOption{AddressOption: domain.AddressOption{Name: addressNamePtr("member")}})
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

func addressNamePtr(name string) *string { return &name }

func TestAddressMutationsRequireWritableTrip(t *testing.T) {
	for _, tc := range []struct {
		name     string
		store    db.TripStore
		canceled bool
	}{
		{"read only", nil, false},
		{"canceled", &trackingTripStore{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			trip := NewTripFactory(tc.store, nil, nil).ForTrip(uuid.New())
			_, err := trip.CreateAddress(ctx, domain.CreateAddressOption{})
			require.Error(t, err)
			_, changed, err := trip.UpdateAddress(ctx, uuid.New(), domain.AddressOption{})
			require.Error(t, err)
			require.False(t, changed)
			_, err = trip.DeleteAddress(ctx, uuid.New())
			require.Error(t, err)
		})
	}
}
