package chain

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPayment(id uuid.UUID, payer, member domain.Address) domain.Record {
	return domain.Record{RecordInfo: domain.RecordInfo{ID: id, Name: "meal", Amount: 20, Time: time.Unix(1, 0), PrePayAddress: payer, Category: domain.CategoryNormal}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: member, ExtendMsg: 10}}}}
}

func TestMergeRecordPatchFieldsDeletionAndDeepCopy(t *testing.T) {
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	tail := testPayment(uuid.New(), domain.Address{ID: payer.ID, Name: "stale"}, domain.Address{ID: member.ID, Name: "stale"})
	name, deleted := "dinner", true
	should := []domain.ExtendAddress{{Address: domain.Address{ID: payer.ID}, ExtendMsg: 20}}
	merged, changed, err := MergeRecordPatch(tail, domain.RecordPatch{Name: &name, IsDeleted: &deleted, ShouldPayAddress: &should}, []domain.Address{payer, member})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "dinner", merged.Name)
	assert.True(t, merged.IsDeleted)
	assert.Equal(t, payer, merged.PrePayAddress)
	assert.Equal(t, payer, merged.ShouldPayAddress[0].Address)
	should[0].ExtendMsg = 99
	assert.Equal(t, float64(20), merged.ShouldPayAddress[0].ExtendMsg)
	assert.Equal(t, "meal", tail.Name)

	unchanged, changed, err := MergeRecordPatch(merged, domain.RecordPatch{}, []domain.Address{payer, member})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, merged, unchanged)
}

func TestMergeRecordPatchRejectsForeignAddress(t *testing.T) {
	payer := domain.Address{ID: uuid.New()}
	member := domain.Address{ID: uuid.New()}
	tail := testPayment(uuid.New(), payer, member)
	foreign := uuid.New()
	_, _, err := MergeRecordPatch(tail, domain.RecordPatch{PrePayAddressID: &foreign}, []domain.Address{payer, member})
	var addressErr *InvalidRecordAddressesError
	assert.ErrorAs(t, err, &addressErr)
}

type factoryReader struct {
	node RecordNode
	err  error
}

func (factoryReader) LoadTrip(context.Context, uuid.UUID) (*domain.TripInfo, error) {
	return nil, nil
}
func (r factoryReader) LoadRecord(context.Context, uuid.UUID) (RecordNode, error) {
	return r.node, r.err
}
func (factoryReader) LoadTripRecords(context.Context, uuid.UUID) ([]domain.RecordInfo, error) {
	return nil, nil
}
func (factoryReader) LoadTripAddresses(context.Context, uuid.UUID) ([]domain.Address, error) {
	return nil, nil
}
func (factoryReader) LoadRecordShouldPay(context.Context, uuid.UUID) ([]domain.ExtendAddress, error) {
	return nil, nil
}

func TestUpdateOnlyResolvesTargetIdentity(t *testing.T) {
	target, tripID := uuid.New(), uuid.New()
	intent, err := NewRecordFactory(nil, factoryReader{node: RecordNode{TripID: tripID, Info: domain.RecordInfo{ID: target}}}).Update(context.Background(), target, domain.RecordPatch{})
	require.NoError(t, err)
	assert.Equal(t, tripID, intent.TripID())
	assert.Equal(t, uuid.Nil, intent.ID())

	_, err = NewRecordFactory(nil, factoryReader{err: fmt.Errorf("%w: missing", chainlist.ErrNodeNotFound)}).Update(context.Background(), target, domain.RecordPatch{})
	var resolution *TailResolutionError
	require.ErrorAs(t, err, &resolution)
	assert.ErrorIs(t, err, chainlist.ErrNodeNotFound)
}

type projectionReader struct {
	tripInfo   *domain.TripInfo
	records    []domain.RecordInfo
	addresses  []domain.Address
	shouldPay  map[uuid.UUID][]domain.ExtendAddress
	tripErr    error
	addressErr error
}

func (r projectionReader) LoadTrip(context.Context, uuid.UUID) (*domain.TripInfo, error) {
	return r.tripInfo, r.tripErr
}
func (r projectionReader) LoadRecord(context.Context, uuid.UUID) (RecordNode, error) {
	return RecordNode{}, errors.New("unused")
}
func (r projectionReader) LoadTripRecords(context.Context, uuid.UUID) ([]domain.RecordInfo, error) {
	return r.records, nil
}
func (r projectionReader) LoadTripAddresses(context.Context, uuid.UUID) ([]domain.Address, error) {
	return r.addresses, r.addressErr
}
func (r projectionReader) LoadRecordShouldPay(_ context.Context, id uuid.UUID) ([]domain.ExtendAddress, error) {
	return r.shouldPay[id], nil
}

func TestTripReadsInfoAndAddressesThroughReader(t *testing.T) {
	tripID := uuid.New()
	info := &domain.TripInfo{ID: tripID, Name: "trip"}
	addresses := []domain.Address{{ID: uuid.New(), Name: "member"}}
	trip := NewTrip(tripID, nil, projectionReader{tripInfo: info, addresses: addresses})

	actualInfo, err := trip.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, info, actualInfo)
	actualInfo.Name = "changed"
	assert.Equal(t, "trip", info.Name)

	actualAddresses, err := trip.Addresses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, addresses, actualAddresses)
	actualAddresses[0].Name = "changed"
	assert.Equal(t, "member", addresses[0].Name)
}

func TestTripReadMethodsPropagateErrors(t *testing.T) {
	tripErr := errors.New("trip read failed")
	addressErr := errors.New("address read failed")
	trip := NewTrip(uuid.New(), nil, projectionReader{tripErr: tripErr, addressErr: addressErr})

	_, err := trip.Info(context.Background())
	assert.ErrorIs(t, err, tripErr)
	_, err = trip.Addresses(context.Background())
	assert.ErrorIs(t, err, addressErr)
}

func TestSettlementUsesOnlyNonDeletedCanonicalTail(t *testing.T) {
	tripID := uuid.New()
	payer := domain.Address{ID: uuid.New(), Name: "payer"}
	member := domain.Address{ID: uuid.New(), Name: "member"}
	rootID, tailID := uuid.New(), uuid.New()
	root := testPayment(rootID, payer, member).RecordInfo
	root.Amount = 0 // Invalid history must not poison a repaired tail.
	tail := testPayment(tailID, payer, member).RecordInfo
	tail.ParentRecordID = &rootID
	root.ChildRecordID = &tailID
	reader := projectionReader{records: []domain.RecordInfo{root, tail}, shouldPay: map[uuid.UUID][]domain.ExtendAddress{tailID: {{Address: member, ExtendMsg: 10}}}}

	result, err := NewTrip(tripID, nil, reader).CalculateMoneyShare(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Valid)

	reader.records[1].Amount = 0
	result, err = NewTrip(tripID, nil, reader).CalculateMoneyShare(context.Background())
	require.NoError(t, err)
	assert.False(t, result.Valid)

	reader.records[1].IsDeleted = true
	result, err = NewTrip(tripID, nil, reader).CalculateMoneyShare(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Valid)
}
