// Package testutil provides store instrumentation for tests and benchmarks.
package testutil

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"sync/atomic"
)

type DataLoadCounter struct {
	Batches atomic.Int64
	Keys    atomic.Int64
}

// LoaderCounters records backing-store fetches for one test or benchmark.
// Reset only while fetches are idle; read Snapshot after workers finish.
type LoaderCounters struct {
	Records       DataLoadCounter
	TripRecords   DataLoadCounter
	TripAddresses DataLoadCounter
	Trips         DataLoadCounter
}

type DataLoadCount struct {
	Batches int64
	Keys    int64
}

type LoaderSnapshot struct {
	Records       DataLoadCount
	TripRecords   DataLoadCount
	TripAddresses DataLoadCount
	Trips         DataLoadCount
}

func (d *LoaderCounters) Reset() {
	d.Records.reset()
	d.TripRecords.reset()
	d.TripAddresses.reset()
	d.Trips.reset()
}

func (d *LoaderCounters) Snapshot() LoaderSnapshot {
	return LoaderSnapshot{
		Records:       d.Records.snapshot(),
		TripRecords:   d.TripRecords.snapshot(),
		TripAddresses: d.TripAddresses.snapshot(),
		Trips:         d.Trips.snapshot(),
	}
}

func (c *DataLoadCounter) reset() {
	c.Batches.Store(0)
	c.Keys.Store(0)
}

func (c *DataLoadCounter) snapshot() DataLoadCount {
	return DataLoadCount{Batches: c.Batches.Load(), Keys: c.Keys.Load()}
}

// Wrap returns an instrumented view without modifying the backing store.
func (d *LoaderCounters) Wrap(store db.DataLoaderStore) db.DataLoaderStore {
	return &countingStore{store, d}
}

type countingStore struct {
	db.DataLoaderStore
	counters *LoaderCounters
}

func count(counter *DataLoadCounter, n int) {
	counter.Batches.Add(1)
	counter.Keys.Add(int64(n))
}
func (s *countingStore) DataLoaderGetRecordList(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]db.RecordSnapshot, error) {
	count(&s.counters.Records, len(ids))
	return s.DataLoaderStore.DataLoaderGetRecordList(ctx, ids)
}
func (s *countingStore) DataLoaderGetTripRecords(ctx context.Context, ids []uuid.UUID, options db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	count(&s.counters.TripRecords, len(ids))
	return s.DataLoaderStore.DataLoaderGetTripRecords(ctx, ids, options)
}
func (s *countingStore) DataLoaderGetTripAddressList(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	count(&s.counters.TripAddresses, len(ids))
	return s.DataLoaderStore.DataLoaderGetTripAddressList(ctx, ids)
}
func (s *countingStore) DataLoaderGetTripInfoList(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	count(&s.counters.Trips, len(ids))
	return s.DataLoaderStore.DataLoaderGetTripInfoList(ctx, ids)
}
