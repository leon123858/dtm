package trip

import (
	"context"
	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestListHistoryAndLiveTails(t *testing.T) {
	payer, member := domain.Address{ID: uuid.New()}, domain.Address{ID: uuid.New()}
	root, tail, deleted := testPayment(uuid.New(), payer, member), testPayment(uuid.New(), payer, member), testPayment(uuid.New(), payer, member)
	root.ChildRecordID = &tail.ID
	tail.ParentRecordID = &root.ID
	deleted.IsDeleted = true
	reader := projectionReader{records: []domain.RecordInfo{tail.RecordInfo, deleted.RecordInfo, root.RecordInfo}, shouldPay: map[uuid.UUID][]domain.ExtendAddress{root.ID: root.ShouldPayAddress, tail.ID: tail.ShouldPayAddress, deleted.ID: deleted.ShouldPayAddress}}
	factory := NewTripFactory(nil, staticReader(reader))
	tripID := uuid.New()
	live, err := factory.ForTrip(tripID).List(context.Background())
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, tail.ID, live[0].ID())
	require.True(t, live[0].IsActive())
	history, err := factory.ForTrip(tripID, ReadOptions{HaveHistory: true}).List(context.Background())
	require.NoError(t, err)
	require.Len(t, history, 3)
	for _, r := range history {
		require.Equal(t, tripID, r.TripID())
		require.Equal(t, r.ID() != root.ID, r.IsActive())
		info := r.Info()
		if info.ParentRecordID != nil {
			*info.ParentRecordID = uuid.Nil
			require.Equal(t, root.ID, *r.Info().ParentRecordID)
		}
		if info.ChildRecordID != nil {
			*info.ChildRecordID = uuid.Nil
			require.Equal(t, tail.ID, *r.Info().ChildRecordID)
		}
		shares := r.GetShouldPay()
		shares[0].Address.ID = uuid.Nil
		require.Equal(t, member.ID, r.GetShouldPay()[0].Address.ID)
	}
	require.Equal(t, member.ID, reader.shouldPay[tail.ID][0].Address.ID)
}

func TestListHistoryRejectsCorruptChains(t *testing.T) {
	a, b, missing := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name    string
		records []domain.RecordInfo
	}{
		{"duplicate", []domain.RecordInfo{{ID: a}, {ID: a}}},
		{"missing child", []domain.RecordInfo{{ID: a, ChildRecordID: &missing}}},
		{"missing parent", []domain.RecordInfo{{ID: a, ParentRecordID: &missing}}},
		{"cycle", []domain.RecordInfo{{ID: a, ParentRecordID: &b, ChildRecordID: &b}, {ID: b, ParentRecordID: &a, ChildRecordID: &a}}},
		{"non reciprocal", []domain.RecordInfo{{ID: a, ChildRecordID: &b}, {ID: b}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trip := NewTripFactory(nil, staticReader(projectionReader{records: tt.records})).ForTrip(uuid.New(), ReadOptions{HaveHistory: true})
			result, err := trip.List(context.Background())
			require.ErrorIs(t, err, ErrInvalidChain)
			require.Nil(t, result)
			_, err = trip.CalculateMoneyShare(context.Background())
			require.ErrorIs(t, err, ErrInvalidChain)
		})
	}
}
