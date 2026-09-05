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
	store                  DataLoaderStore
	getRecord              *dataloadgen.Loader[uuid.UUID, RecordNode]
	getRecordInfoList      *dataloadgen.Loader[uuid.UUID, []domain.RecordInfo]
	getTripAddressList     *dataloadgen.Loader[uuid.UUID, []domain.Address]
	getRecordShouldPayList *dataloadgen.Loader[uuid.UUID, []domain.ExtendAddress]
	getTripInfoList        *dataloadgen.Loader[uuid.UUID, *domain.TripInfo]
}

var _ Reader = (*TripDataLoader)(nil)

// NewTripDataLoader creates request-scoped loaders over the read-only store.
func NewTripDataLoader(dbWrapper DataLoaderStore) *TripDataLoader {
	return &TripDataLoader{
		store:                  dbWrapper,
		getRecord:              dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.Records, dbWrapper.DataLoaderGetRecordList)),
		getRecordInfoList:      dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.TripRecords, dbWrapper.DataLoaderGetRecordInfoList)),
		getTripAddressList:     dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.TripAddresses, dbWrapper.DataLoaderGetTripAddressList)),
		getRecordShouldPayList: dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.RecordShouldPays, dbWrapper.DataLoaderGetRecordShouldPayList)),
		getTripInfoList:        dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.Trips, dbWrapper.DataLoaderGetTripInfoList)),
	}
}

// LoadTrip returns trip metadata through the request-scoped cache.
func (l *TripDataLoader) LoadTrip(ctx context.Context, tripID uuid.UUID) (*domain.TripInfo, error) {
	return l.getTripInfoList.Load(ctx, tripID)
}

// LoadRecord implements Reader through the request-scoped cache.
func (l *TripDataLoader) LoadRecord(ctx context.Context, recordID uuid.UUID) (RecordNode, error) {
	return l.getRecord.Load(ctx, recordID)
}

// LoadTripRecords implements Reader through the request-scoped cache.
func (l *TripDataLoader) LoadTripRecords(ctx context.Context, tripID uuid.UUID) ([]domain.RecordInfo, error) {
	return l.getRecordInfoList.Load(ctx, tripID)
}

// LoadTripAddresses implements Reader through the request-scoped cache.
func (l *TripDataLoader) LoadTripAddresses(ctx context.Context, tripID uuid.UUID) ([]domain.Address, error) {
	return l.getTripAddressList.Load(ctx, tripID)
}

// LoadRecordShouldPay implements Reader through the request-scoped cache.
func (l *TripDataLoader) LoadRecordShouldPay(ctx context.Context, recordID uuid.UUID) ([]domain.ExtendAddress, error) {
	return l.getRecordShouldPayList.Load(ctx, recordID)
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
