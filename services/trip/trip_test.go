package trip

import (
	"context"
	"errors"
	"testing"

	"dtm/adapters/db/db"
	"dtm/domain"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
func (r projectionReader) LoadRecord(context.Context, uuid.UUID) (db.RecordSnapshot, error) {
	return db.RecordSnapshot{}, errors.New("unused")
}
func (r projectionReader) LoadTripRecords(_ context.Context, tripID uuid.UUID, options db.RecordReadOptions) ([]db.RecordSnapshot, error) {
	result := []db.RecordSnapshot{}
	for _, info := range r.records {
		if !options.HaveHistory && (info.ChildRecordID != nil || info.IsDeleted) {
			continue
		}
		result = append(result, db.RecordSnapshot{TripID: tripID, Record: domain.Record{RecordInfo: info, RecordData: domain.RecordData{ShouldPayAddress: r.shouldPay[info.ID]}}})
	}
	return result, nil
}
func (r projectionReader) LoadTripAddresses(context.Context, uuid.UUID) ([]domain.Address, error) {
	return r.addresses, r.addressErr
}

func TestTripReadsInfoAndAddressesThroughReader(t *testing.T) {
	tripID := uuid.New()
	info := &domain.TripInfo{ID: tripID, Name: "trip"}
	addresses := []domain.Address{{ID: uuid.New(), Name: "member"}}
	trip := NewTripFactory(nil, staticReader(projectionReader{tripInfo: info, addresses: addresses})).ForTrip(tripID)

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
	trip := NewTripFactory(nil, staticReader(projectionReader{tripErr: tripErr, addressErr: addressErr})).ForTrip(uuid.New())

	_, err := trip.Info(context.Background())
	assert.ErrorIs(t, err, tripErr)
	_, err = trip.Addresses(context.Background())
	assert.ErrorIs(t, err, addressErr)
}

func TestTripInfoClassifiesMissingTrip(t *testing.T) {
	trip := NewTripFactory(nil, staticReader(projectionReader{})).ForTrip(uuid.New())
	_, err := trip.Info(context.Background())
	require.ErrorIs(t, err, ErrTripNotFound)
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

	tripFactory := NewTripFactory(nil, staticReader(reader))
	result, err := tripFactory.ForTrip(tripID).CalculateMoneyShare(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Valid)

	reader.records[1].Amount = 0
	tripFactory = NewTripFactory(nil, staticReader(reader))
	result, err = tripFactory.ForTrip(tripID).CalculateMoneyShare(context.Background())
	require.NoError(t, err)
	assert.False(t, result.Valid)

	reader.records[1].IsDeleted = true
	tripFactory = NewTripFactory(nil, staticReader(reader))
	result, err = tripFactory.ForTrip(tripID).CalculateMoneyShare(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Valid)
}
