package db

import (
	"context"
	"sync/atomic"
)

type DataLoadCounter struct {
	Batches atomic.Int64
	Keys    atomic.Int64
}

type DataLoaderDebugCounters struct {
	Records          DataLoadCounter
	TripRecords      DataLoadCounter
	TripAddresses    DataLoadCounter
	RecordShouldPays DataLoadCounter
	Trips            DataLoadCounter
}

type DataLoadCount struct {
	Batches int64
	Keys    int64
}

type DataLoaderDebugSnapshot struct {
	Records          DataLoadCount
	TripRecords      DataLoadCount
	TripAddresses    DataLoadCount
	RecordShouldPays DataLoadCount
	Trips            DataLoadCount
}

// DataLoaderDebug records actual backing-store fetches, not Loader.Load calls.
// It is always safe to read and reset in tests; production code may also use a
// snapshot for temporary diagnostics.
var DataLoaderDebug DataLoaderDebugCounters

func (d *DataLoaderDebugCounters) Reset() {
	d.Records.reset()
	d.TripRecords.reset()
	d.TripAddresses.reset()
	d.RecordShouldPays.reset()
	d.Trips.reset()
}

func (d *DataLoaderDebugCounters) Snapshot() DataLoaderDebugSnapshot {
	return DataLoaderDebugSnapshot{
		Records:          d.Records.snapshot(),
		TripRecords:      d.TripRecords.snapshot(),
		TripAddresses:    d.TripAddresses.snapshot(),
		RecordShouldPays: d.RecordShouldPays.snapshot(),
		Trips:            d.Trips.snapshot(),
	}
}

func (c *DataLoadCounter) reset() {
	c.Batches.Store(0)
	c.Keys.Store(0)
}

func (c *DataLoadCounter) snapshot() DataLoadCount {
	return DataLoadCount{Batches: c.Batches.Load(), Keys: c.Keys.Load()}
}

func instrumentMappedFetch[K comparable, V any](counter *DataLoadCounter, fetch func(context.Context, []K) (map[K]V, error)) func(context.Context, []K) (map[K]V, error) {
	return func(ctx context.Context, keys []K) (map[K]V, error) {
		counter.Batches.Add(1)
		counter.Keys.Add(int64(len(keys)))
		return fetch(ctx, keys)
	}
}
