package db

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"dtm/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// All fixtures are immutable once workers start.
type concurrentLoaderStore struct{ tripID, recordID uuid.UUID }

func (s concurrentLoaderStore) DataLoaderGetRecordList(context.Context, []uuid.UUID) (map[uuid.UUID]RecordSnapshot, error) {
	return map[uuid.UUID]RecordSnapshot{s.recordID: {TripID: s.tripID, Record: domain.Record{RecordInfo: domain.RecordInfo{ID: s.recordID}}}}, nil
}
func (s concurrentLoaderStore) DataLoaderGetTripRecords(context.Context, []uuid.UUID, RecordReadOptions) (map[uuid.UUID][]RecordSnapshot, error) {
	records, _ := s.DataLoaderGetRecordList(context.Background(), nil)
	return map[uuid.UUID][]RecordSnapshot{s.tripID: {records[s.recordID]}}, nil
}
func (s concurrentLoaderStore) DataLoaderGetTripAddressList(context.Context, []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	return map[uuid.UUID][]domain.Address{s.tripID: {}}, nil
}
func (s concurrentLoaderStore) DataLoaderGetTripInfoList(context.Context, []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	return map[uuid.UUID]*domain.TripInfo{s.tripID: {ID: s.tripID, Name: "benchmark"}}, nil
}

func concurrentLoads(loader *TripDataLoader, s concurrentLoaderStore, workers int) error {
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
				records, err := loader.LoadTripRecords(ctx, s.tripID, RecordReadOptions{HaveHistory: history})
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
			s := concurrentLoaderStore{uuid.New(), uuid.New()}
			DataLoaderDebug.Reset()
			for request := int64(1); request <= 2; request++ {
				require.NoError(t, concurrentLoads(NewTripDataLoader(s), s, workers))
				require.Equal(t, DataLoaderDebugSnapshot{Trips: DataLoadCount{request, request}, TripAddresses: DataLoadCount{request, request}, TripRecords: DataLoadCount{2 * request, 2 * request}}, DataLoaderDebug.Snapshot())
			}
		})
	}
}

func BenchmarkTripDataLoaderConcurrent(b *testing.B) {
	for _, workers := range []int{1, 32, 128, 256} {
		b.Run(fmt.Sprint(workers), func(b *testing.B) {
			s := concurrentLoaderStore{uuid.New(), uuid.New()}
			DataLoaderDebug.Reset()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := concurrentLoads(NewTripDataLoader(s), s, workers); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			n := int64(b.N)
			want := DataLoaderDebugSnapshot{Trips: DataLoadCount{n, n}, TripAddresses: DataLoadCount{n, n}, TripRecords: DataLoadCount{2 * n, 2 * n}}
			if got := DataLoaderDebug.Snapshot(); got != want {
				b.Fatalf("fetches: got %+v want %+v", got, want)
			}
			b.ReportMetric(4, "fetches/request")
		})
	}
}

func TestTripDataLoaderConcurrentRecordMiss(t *testing.T) {
	s := concurrentLoaderStore{uuid.New(), uuid.New()}
	loader := NewTripDataLoader(s)
	DataLoaderDebug.Reset()
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
	require.Equal(t, DataLoaderDebugSnapshot{Records: DataLoadCount{Batches: 1, Keys: 1}}, DataLoaderDebug.Snapshot())
}
