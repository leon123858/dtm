package trip

import (
	"context"
	"errors"
	"testing"

	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAddressStatisticsSplitStrategies(t *testing.T) {
	a, b, c := domain.Address{ID: uuid.New()}, domain.Address{ID: uuid.New()}, domain.Address{ID: uuid.New()}
	cases := []struct {
		name     string
		category domain.RecordCategory
		amount   float64
		weights  []float64
		expenses []float64
	}{
		{"normal", domain.CategoryNormal, 900, []float64{0, 0, 0}, []float64{300, 300, 300}},
		{"fixed with zero share", domain.CategoryFix, 900, []float64{200, 700, 0}, []float64{200, 700, 0}},
		{"parts", domain.CategoryPart, 900, []float64{1, 2, 3}, []float64{150, 300, 450}},
		{"fixed before normal", domain.CategoryFixBeforeNormal, 900, []float64{-100, 100, 0}, []float64{100, 450, 350}},
		{"transfer", domain.CategoryTransfer, 900, []float64{0, 900, 0}, []float64{0, 900, 0}},
		{"fractional shares", domain.CategoryNormal, 1, []float64{0, 0, 0}, []float64{1.0 / 3, 1.0 / 3, 1.0 / 3}},
		{"below settlement threshold", domain.CategoryNormal, 0.009, []float64{0, 0, 0}, []float64{0.003, 0.003, 0.003}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := domain.RecordInfo{ID: uuid.New(), Name: "expense", Amount: tc.amount, PrePayAddress: a, Category: tc.category}
			members := []domain.Address{a, b, c}
			shares := make([]domain.ExtendAddress, 3)
			for i, member := range members {
				shares[i] = domain.ExtendAddress{Address: member, ExtendMsg: tc.weights[i]}
			}
			reader := projectionReader{records: []domain.RecordInfo{record}, shouldPay: map[uuid.UUID][]domain.ExtendAddress{record.ID: shares}}
			trip := NewTripFactory(nil, staticReader(reader)).ForTrip(uuid.New())
			got, err := trip.CalculateAddressStatistics(context.Background())
			require.NoError(t, err)
			require.Len(t, got, 3)
			for i, member := range members {
				require.InDelta(t, tc.expenses[i], got[member.ID].TotalExpense, 1e-12)
				require.Equal(t, int32(1), got[member.ID].ActiveRecordCount)
				expectedPrepaid := 0.0
				if i == 0 {
					expectedPrepaid = tc.amount
				}
				require.Equal(t, expectedPrepaid, got[member.ID].TotalPrepaid)
			}
		})
	}
}

func TestAddressStatisticsUsesValidLiveTails(t *testing.T) {
	a, b := domain.Address{ID: uuid.New()}, domain.Address{ID: uuid.New()}
	root, tail, deleted, invalid, transfer := testPayment(uuid.New(), a, b), testPayment(uuid.New(), a, b), testPayment(uuid.New(), a, b), testPayment(uuid.New(), a, b), testPayment(uuid.New(), b, a)
	root.ChildRecordID = &tail.ID
	tail.ParentRecordID = &root.ID
	root.Amount = 999
	tail.Amount = 10
	tail.Category = domain.CategoryFix
	invalid.Category = domain.CategoryFix
	transfer.Amount = 10
	deleted.IsDeleted = true
	invalid.Amount = 123 // Fixed shares still total 10, so the record is invalid.
	transfer.Category = domain.CategoryTransfer
	reader := projectionReader{shouldPay: map[uuid.UUID][]domain.ExtendAddress{}}
	for _, record := range []domain.Record{root, tail, deleted, invalid, transfer} {
		reader.records = append(reader.records, record.RecordInfo)
		reader.shouldPay[record.ID] = record.ShouldPayAddress
	}
	factory := NewTripFactory(nil, staticReader(reader))
	id := uuid.New()
	expected := map[uuid.UUID]AddressStatistics{
		a.ID: {TotalExpense: 10, TotalPrepaid: 10, ActiveRecordCount: 2},
		b.ID: {TotalExpense: 10, TotalPrepaid: 10, ActiveRecordCount: 2},
	}
	for _, history := range []bool{false, true} {
		got, err := factory.ForTrip(id, ReadOptions{HaveHistory: history}).CalculateAddressStatistics(context.Background())
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	settlement, err := factory.ForTrip(id).CalculateMoneyShare(context.Background())
	require.NoError(t, err)
	require.False(t, settlement.Valid, "statistics must not change settlement validation")
}

type statisticsErrorReader struct {
	db.Reader
	err error
}

func (r statisticsErrorReader) LoadTripRecords(context.Context, uuid.UUID, db.RecordReadOptions) ([]db.RecordSnapshot, error) {
	return nil, r.err
}

func TestAddressStatisticsEmptyAndReadErrors(t *testing.T) {
	trip := NewTripFactory(nil, staticReader(projectionReader{})).ForTrip(uuid.New())
	result, err := trip.CalculateAddressStatistics(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = trip.CalculateAddressStatistics(ctx)
	require.ErrorIs(t, err, context.Canceled)
	failure := errors.New("record read failed")
	trip = NewTripFactory(nil, staticReader(statisticsErrorReader{err: failure})).ForTrip(uuid.New())
	_, err = trip.CalculateAddressStatistics(context.Background())
	require.ErrorIs(t, err, failure)
}
