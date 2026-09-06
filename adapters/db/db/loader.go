package db

import (
	"context"
	"fmt"

	"dtm/domain"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
)

type tripDataLoaderContextKey struct{}

type TripDataLoader struct {
	store              DataLoaderStore
	getRecord          *dataloadgen.Loader[uuid.UUID, RecordSnapshot]
	getTripRecords     [2]*dataloadgen.Loader[uuid.UUID, []RecordSnapshot]
	getTripAddressList *dataloadgen.Loader[uuid.UUID, []domain.Address]
	getTripInfoList    *dataloadgen.Loader[uuid.UUID, *domain.TripInfo]
}

var _ Reader = (*TripDataLoader)(nil)

// NewTripDataLoader creates request-scoped loaders over the read-only store.
func NewTripDataLoader(dbWrapper DataLoaderStore) *TripDataLoader {
	loader := &TripDataLoader{
		store:              dbWrapper,
		getRecord:          dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.Records, dbWrapper.DataLoaderGetRecordList)),
		getTripAddressList: dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.TripAddresses, dbWrapper.DataLoaderGetTripAddressList)),
		getTripInfoList:    dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.Trips, dbWrapper.DataLoaderGetTripInfoList)),
	}
	for i := range loader.getTripRecords {
		// get history records or not
		options := RecordReadOptions{HaveHistory: i == 1}
		loader.getTripRecords[i] = dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.TripRecords,
			func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]RecordSnapshot, error) {
				return dbWrapper.DataLoaderGetTripRecords(ctx, ids, options)
			}))
	}
	return loader
}

// LoadTrip returns trip metadata through the request-scoped cache.
func (l *TripDataLoader) LoadTrip(ctx context.Context, tripID uuid.UUID) (*domain.TripInfo, error) {
	return l.getTripInfoList.Load(ctx, tripID)
}

// LoadRecord implements Reader through the request-scoped cache.
func (l *TripDataLoader) LoadRecord(ctx context.Context, recordID uuid.UUID) (RecordSnapshot, error) {
	return l.getRecord.Load(ctx, recordID)
}

// LoadTripRecords implements Reader through the request-scoped cache.
func (l *TripDataLoader) LoadTripRecords(ctx context.Context, tripID uuid.UUID, options RecordReadOptions) ([]RecordSnapshot, error) {
	index := 0
	if options.HaveHistory {
		index = 1
	}
	records, err := l.getTripRecords[index].Load(ctx, tripID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		l.getRecord.Prime(record.ID, record)
	}
	return records, nil
}

// LoadTripAddresses implements Reader through the request-scoped cache.
func (l *TripDataLoader) LoadTripAddresses(ctx context.Context, tripID uuid.UUID) ([]domain.Address, error) {
	return l.getTripAddressList.Load(ctx, tripID)
}

// Reset drops every cached request snapshot. Call it only between resolver
// phases, after outstanding loader calls have completed.
func (l *TripDataLoader) Reset() {
	if l == nil || l.store == nil {
		return
	}
	fresh := NewTripDataLoader(l.store)
	*l = *fresh
}

// WithTripDataLoader attaches a request-scoped loader to ctx.
func WithTripDataLoader(ctx context.Context, loader *TripDataLoader) context.Context {
	return context.WithValue(ctx, tripDataLoaderContextKey{}, loader)
}

// TripDataLoaderFromContext returns the request-scoped loader attached to ctx.
func TripDataLoaderFromContext(ctx context.Context) (*TripDataLoader, error) {
	loader, ok := ctx.Value(tripDataLoaderContextKey{}).(*TripDataLoader)
	if !ok || loader == nil {
		return nil, fmt.Errorf("data loader is not available")
	}
	return loader, nil
}
