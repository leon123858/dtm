package db_test

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/adapters/db/testutil"
	"fmt"
	"sync"
	"testing"

	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// All fixtures are immutable once workers start.
type concurrentLoaderStore struct{ tripID, recordID uuid.UUID }

func (s concurrentLoaderStore) DataLoaderGetRecordList(context.Context, []uuid.UUID) (map[uuid.UUID]db.RecordSnapshot, error) {
	return map[uuid.UUID]db.RecordSnapshot{s.recordID: {TripID: s.tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: s.recordID}}}}, nil
}
func (s concurrentLoaderStore) DataLoaderGetTripRecords(context.Context, []uuid.UUID, db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	records, _ := s.DataLoaderGetRecordList(context.Background(), nil)
	return map[uuid.UUID][]db.RecordSnapshot{s.tripID: {records[s.recordID]}}, nil
}
func (s concurrentLoaderStore) DataLoaderGetTripAddressList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	return map[uuid.UUID][]domain.Address{s.tripID: {}}, nil
}
func (s concurrentLoaderStore) DataLoaderGetTripInfoList(context.Context, []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	return map[uuid.UUID]*domain.TripInfo{s.tripID: {ID: s.tripID, Name: "benchmark"}}, nil
}

func concurrentLoads(loader *db.TripDataLoader, s concurrentLoaderStore, workers int) error {
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx := context.Background()
			if _, err := loader.LoadTrip(ctx, s.tripID); err != nil {
				errs <- err
				return
			}
			if _, err := loader.LoadTripAddresses(ctx, s.tripID); err != nil {
				errs <- err
				return
			}
			for _, history := range []bool{false, true} {
				records, err := loader.LoadTripRecords(ctx, s.tripID, db.RecordReadOptions{HaveHistory: history})
				if err != nil {
					errs <- err
					return
				}
				if len(records) != 1 || records[0].ID != s.recordID {
					errs <- fmt.Errorf("incorrect records")
					return
				}
			}
			record, err := loader.LoadRecord(ctx, s.recordID)
			if err != nil {
				errs <- err
				return
			}
			if record.ID != s.recordID {
				errs <- fmt.Errorf("incorrect primed record")
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return nil
}

func TestTripDataLoaderConcurrentDeduplication(t *testing.T) {
	for _, workers := range []int{1, 32, 128, 256} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			counts := new(testutil.LoaderCounters)
			s := concurrentLoaderStore{uuid.New(), uuid.New()}
			counts.Reset()
			for request := int64(1); request <= 2; request++ {
				require.NoError(t, concurrentLoads(db.NewTripDataLoader(counts.Wrap(s)), s, workers))
				require.Equal(t, testutil.LoaderSnapshot{Trips: testutil.DataLoadCount{Batches: request, Keys: request}, TripAddresses: testutil.DataLoadCount{Batches: request, Keys: request}, TripRecords: testutil.DataLoadCount{Batches: 2 * request, Keys: 2 * request}}, counts.Snapshot())
			}
		})
	}
}

func BenchmarkTripDataLoaderConcurrent(b *testing.B) {
	for _, workers := range []int{1, 32, 128, 256} {
		b.Run(fmt.Sprint(workers), func(b *testing.B) {
			counts := new(testutil.LoaderCounters)
			s := concurrentLoaderStore{uuid.New(), uuid.New()}
			counts.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := concurrentLoads(db.NewTripDataLoader(counts.Wrap(s)), s, workers); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			n := int64(b.N)
			want := testutil.LoaderSnapshot{Trips: testutil.DataLoadCount{Batches: n, Keys: n}, TripAddresses: testutil.DataLoadCount{Batches: n, Keys: n}, TripRecords: testutil.DataLoadCount{Batches: 2 * n, Keys: 2 * n}}
			if got := counts.Snapshot(); got != want {
				b.Fatalf("fetches: got %+v want %+v", got, want)
			}
			b.ReportMetric(4, "fetches/request")
		})
	}
}

func TestTripDataLoaderConcurrentRecordMiss(t *testing.T) {
	counts := new(testutil.LoaderCounters)
	s := concurrentLoaderStore{uuid.New(), uuid.New()}
	loader := db.NewTripDataLoader(counts.Wrap(s))
	counts.Reset()
	start := make(chan struct{})
	errs := make(chan error, 256)
	var wg sync.WaitGroup
	for range 256 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			record, err := loader.LoadRecord(context.Background(), s.recordID)
			if err == nil && record.ID != s.recordID {
				err = fmt.Errorf("incorrect record")
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, testutil.LoaderSnapshot{Records: testutil.DataLoadCount{Batches: 1, Keys: 1}}, counts.Snapshot())
}

func (concurrentLoaderStore) DataLoaderGetTripUsers(context.Context, []uuid.UUID) (map[uuid.UUID]db.TripUsers, error) {
	return map[uuid.UUID]db.TripUsers{}, nil
}
