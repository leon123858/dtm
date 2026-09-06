package trip

import (
	"context"
	"fmt"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/domain"
	"dtm/libs/chainlist"
	"dtm/libs/recordpatch"

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
	require.ErrorIs(t, err, ErrRecordNotFound)
	assert.ErrorIs(t, err, chainlist.ErrNodeNotFound)
}

type trackingTripStore struct {
	created       *domain.TripInfo
	updated       *domain.TripInfo
	createdAt     uuid.UUID
	updatedAt     [2]uuid.UUID
	deletedAt     [2]uuid.UUID
	storedAddress domain.Address
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
func (s *trackingTripStore) UpdateTripInfo(info *domain.TripInfo) error {
	copy := *info
	s.updated = &copy
	return s.err
}
func (s *trackingTripStore) CreateAddress(tripID uuid.UUID, name string) (*domain.Address, error) {
	s.createdAt = tripID
	s.storedAddress = domain.Address{ID: uuid.New(), Name: name}
	return &s.storedAddress, s.err
}
func (s *trackingTripStore) UpdateAddress(tripID, addressID uuid.UUID, name string) (*domain.Address, error) {
	s.updatedAt = [2]uuid.UUID{tripID, addressID}
	s.storedAddress = domain.Address{ID: addressID, Name: name}
	return &s.storedAddress, s.err
}
func (s *trackingTripStore) DeleteAddress(tripID, addressID uuid.UUID) (*domain.Address, error) {
	s.deletedAt = [2]uuid.UUID{tripID, addressID}
	s.storedAddress = domain.Address{ID: addressID, Name: "deleted"}
	return &s.storedAddress, s.err
}

func TestTripFactoryAndTripOwnTripMutations(t *testing.T) {
	store := &trackingTripStore{}
	factory := NewTripFactory(store, nil)
	trip, err := factory.Create(context.Background(), "holiday")
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, trip.ID())
	require.Equal(t, &domain.TripInfo{ID: trip.ID(), Name: "holiday"}, store.created)

	info, err := trip.UpdateInfo(context.Background(), "updated")
	require.NoError(t, err)
	assert.Equal(t, &domain.TripInfo{ID: trip.ID(), Name: "updated"}, info)
	assert.Equal(t, info, store.updated)

	created, err := trip.CreateAddress(context.Background(), "member")
	require.NoError(t, err)
	assert.Equal(t, trip.ID(), store.createdAt)
	created.Name = "caller mutation"
	assert.Equal(t, "member", store.storedAddress.Name)

	addressID := created.ID
	updated, err := trip.UpdateAddress(context.Background(), addressID, "renamed")
	require.NoError(t, err)
	assert.Equal(t, [2]uuid.UUID{trip.ID(), addressID}, store.updatedAt)
	assert.Equal(t, "renamed", updated.Name)

	deleted, err := trip.DeleteAddress(context.Background(), addressID)
	require.NoError(t, err)
	assert.Equal(t, [2]uuid.UUID{trip.ID(), addressID}, store.deletedAt)
	assert.Equal(t, "deleted", deleted.Name)
}

func TestTripMutationsRejectUnavailableStoreAndCanceledContext(t *testing.T) {
	_, err := NewTripFactory(nil, nil).Create(context.Background(), "trip")
	assert.EqualError(t, err, "trip store is not available")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &trackingTripStore{}
	_, err = NewTripFactory(store, nil).Create(ctx, "trip")
	assert.ErrorIs(t, err, context.Canceled)
	_, err = NewTripFactory(store, nil).ForTrip(uuid.New()).CreateAddress(ctx, "member")
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
	next := domain.RecordFields{ShouldPayAddress: domain.RecordShares{{AddressID: uuid.NewString(), ExtendMsg: 20}}}
	patch, err := recordpatch.Diff(old, next)
	require.NoError(t, err)
	intent, err := NewRecordFactory(staticReader(factoryReader{node: db.RecordSnapshot{TripID: tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: target}}}})).Update(context.Background(), target, patch)
	require.NoError(t, err)
	patch.Changes[0].Path[0] = "bad"
	patch.Changes[0].To.(domain.RecordShares)[0].ExtendMsg = 99
	stored := intent.(*record).patch
	assert.Equal(t, []string{"ShouldPayAddress"}, stored.Changes[0].Path)
	assert.Equal(t, float64(20), stored.Changes[0].To.(domain.RecordShares)[0].ExtendMsg)
}
