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
	Records       DataLoadCounter
	TripRecords   DataLoadCounter
	TripAddresses DataLoadCounter
	Trips         DataLoadCounter
}

type DataLoadCount struct {
	Batches int64
	Keys    int64
}

type DataLoaderDebugSnapshot struct {
	Records       DataLoadCount
	TripRecords   DataLoadCount
	TripAddresses DataLoadCount
	Trips         DataLoadCount
}

// DataLoaderDebug records actual backing-store fetches, not Loader.Load calls.
// Its individual fields are atomic, but Snapshot and Reset are not atomic as a
// whole. For exact measurements, reset only while fetches are idle and snapshot
// after all measured requests finish. Production snapshots are approximate.
var DataLoaderDebug DataLoaderDebugCounters

func (d *DataLoaderDebugCounters) Reset() {
	d.Records.reset()
	d.TripRecords.reset()
	d.TripAddresses.reset()
	d.Trips.reset()
}

func (d *DataLoaderDebugCounters) Snapshot() DataLoaderDebugSnapshot {
	return DataLoaderDebugSnapshot{
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

func instrumentMappedFetch[K comparable, V any](counter *DataLoadCounter, fetch func(context.Context, []K) (map[K]V, error)) func(context.Context, []K) (map[K]V, error) {
	return func(ctx context.Context, keys []K) (map[K]V, error) {
		counter.Batches.Add(1)
		counter.Keys.Add(int64(len(keys)))
		return fetch(ctx, keys)
	}
}
