package graph

import (
	"context"
	"testing"
	"time"

	"dtm/adapters/db/db"
	"dtm/adapters/db/mem"
	"dtm/domain"
	"dtm/graph/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharePatchInvalidMergeCanBeQueriedAndRepaired(t *testing.T) {
	for _, scenario := range []string{"no recipients", "unbalanced fixed amounts"} {
		t.Run(scenario, func(t *testing.T) {
			store := mem.NewInMemoryTripDBWrapper()
			tripID := uuid.New()
			require.NoError(t, store.CreateTrip(&domain.TripInfo{ID: tripID, Name: "repair"}))
			a, err := store.CreateAddress(tripID, "A")
			require.NoError(t, err)
			b, err := store.CreateAddress(tripID, "B")
			require.NoError(t, err)
			resolver := resolverWithChain(store)
			resolver.TripMessageQueueWrapper = &trackingMQ{}
			mutation := &mutationResolver{Resolver: resolver}
			category := model.RecordCategoryNormal
			if scenario == "unbalanced fixed amounts" {
				category = model.RecordCategoryFix
			}
			millis := "1234"
			baseline := model.NewRecord{Name: "meal", Amount: 20, Time: &millis, PrePayAddressID: a.ID.String(), ShouldPayAddressIds: []string{a.ID.String(), b.ID.String()}, ExtendPayMsg: []float64{10, 10}, Category: &category}
			created, err := mutation.CreateRecord(resolverContext(store), tripID.String(), baseline)
			require.NoError(t, err)
			require.True(t, created.IsValid)
			left, right := baseline, baseline
			expected := map[string]float64{}
			if scenario == "no recipients" {
				left.ShouldPayAddressIds = []string{a.ID.String()}
				left.ExtendPayMsg = []float64{10}
				right.ShouldPayAddressIds = []string{b.ID.String()}
				right.ExtendPayMsg = []float64{10}
			} else {
				left.Amount = 30
				left.ExtendPayMsg = []float64{20, 10}
				right.Amount = 40
				right.ExtendPayMsg = []float64{10, 30}
				expected = map[string]float64{a.ID.String(): 20, b.ID.String(): 30}
			}
			first, err := mutation.UpdateRecord(resolverContext(store), created.ID, model.EditRecord{Old: &baseline, New: &left})
			require.NoError(t, err)
			require.True(t, first.IsValid)
			second, err := mutation.UpdateRecord(resolverContext(store), created.ID, model.EditRecord{Old: &baseline, New: &right})
			require.NoError(t, err)
			assert.False(t, second.IsValid, "merge must succeed even when the result needs repair")
			require.NotNil(t, second.ParentRecordID)
			assert.Equal(t, first.ID, *second.ParentRecordID)
			query := &tripResolver{Resolver: resolver}
			records, err := query.Records(resolverContext(store), &model.Trip{ID: tripID.String()})
			require.NoError(t, err)
			require.Len(t, records, 1)
			current := records[0]
			assert.Equal(t, second.ID, current.ID)
			assert.False(t, current.IsValid)
			require.Len(t, current.ExtendPayMsg, len(current.ShouldPayAddress))
			actual := map[string]float64{}
			for i, address := range current.ShouldPayAddress {
				actual[address.ID] = current.ExtendPayMsg[i]
			}
			assert.Equal(t, expected, actual)
			// Build the repair baseline from the actual query payload, as a client does.
			repairOld := right
			repairOld.ShouldPayAddressIds = make([]string, len(current.ShouldPayAddress))
			repairOld.ExtendPayMsg = append([]float64(nil), current.ExtendPayMsg...)
			for i, address := range current.ShouldPayAddress {
				repairOld.ShouldPayAddressIds[i] = address.ID
			}
			repairNew := repairOld
			if scenario == "no recipients" {
				repairNew.ShouldPayAddressIds = []string{a.ID.String()}
				repairNew.ExtendPayMsg = []float64{10}
			} else {
				repairNew.Amount = 50
			}
			repaired, err := mutation.UpdateRecord(resolverContext(store), current.ID, model.EditRecord{Old: &repairOld, New: &repairNew})
			require.NoError(t, err)
			assert.True(t, repaired.IsValid)
			records, err = query.Records(resolverContext(store), &model.Trip{ID: tripID.String()})
			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, repaired.ID, records[0].ID)
			assert.True(t, records[0].IsValid)
			history, err := query.Records(resolverContext(store), &model.Trip{ID: tripID.String(), HaveHistory: true})
			require.NoError(t, err)
			assert.Len(t, history, 4)
		})
	}
}

type unsortedShareQueryDB struct {
	db.TripDBWrapper
	snapshots []db.RecordSnapshot
}

func (s *unsortedShareQueryDB) DataLoaderGetTripRecords(_ context.Context, ids []uuid.UUID, options db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	result := make(map[uuid.UUID][]db.RecordSnapshot)
	for _, id := range ids {
		for _, snapshot := range s.snapshots {
			if snapshot.TripID == id && (options.HaveHistory || snapshot.ChildRecordID == nil) {
				result[id] = append(result[id], snapshot)
			}
		}
	}
	return result, nil
}

func TestShareQuerySortsCurrentAndHistoricalRecordsWithoutMutatingSource(t *testing.T) {
	tripID := uuid.New()
	a := domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Name: "A"}
	b := domain.Address{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Name: "B"}
	root := domain.Record{RecordInfo: domain.RecordInfo{ID: uuid.New(), Name: "meal", Amount: 30, Time: time.UnixMilli(1234), PrePayAddress: a}, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{{Address: b, ExtendMsg: 20}, {Address: a, ExtendMsg: 10}}}}
	tail := root
	tail.ID = uuid.New()
	tail.ParentRecordID = &root.ID
	root.ChildRecordID = &tail.ID
	tail.ShouldPayAddress = []domain.ExtendAddress{{Address: b, ExtendMsg: 22}, {Address: a, ExtendMsg: 11}}
	store := &unsortedShareQueryDB{snapshots: []db.RecordSnapshot{{TripID: tripID, Record: root}, {TripID: tripID, Record: tail}}}
	resolver := &tripResolver{Resolver: resolverWithChain(store)}
	for _, history := range []bool{false, true} {
		name := "current"
		if history {
			name = "history"
		}
		t.Run(name, func(t *testing.T) {
			records, err := resolver.Records(resolverContext(store), &model.Trip{ID: tripID.String(), HaveHistory: history})
			require.NoError(t, err)
			count := 1
			if history {
				count = 2
			}
			require.Len(t, records, count)
			for _, record := range records {
				require.Len(t, record.ShouldPayAddress, 2)
				assert.Equal(t, []*model.Address{{ID: a.ID.String(), Name: "A"}, {ID: b.ID.String(), Name: "B"}}, record.ShouldPayAddress)
				want := []float64{10, 20}
				if record.ID == tail.ID.String() {
					want = []float64{11, 22}
				}
				assert.Equal(t, want, record.ExtendPayMsg)
			}
			records[0].ShouldPayAddress[0].Name = "caller mutation"
			records[0].ExtendPayMsg[0] = 99
			assert.Equal(t, []domain.ExtendAddress{{Address: b, ExtendMsg: 20}, {Address: a, ExtendMsg: 10}}, store.snapshots[0].ShouldPayAddress)
			assert.Equal(t, []domain.ExtendAddress{{Address: b, ExtendMsg: 22}, {Address: a, ExtendMsg: 11}}, store.snapshots[1].ShouldPayAddress)
		})
	}
}
