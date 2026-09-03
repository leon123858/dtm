package db

import (
	"dtm/chain"
	"dtm/domain"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
)

type dataLoaderKey string

const (
	DataLoaderKeyTripData dataLoaderKey = "trip_data_loader"
)

type TripDataLoader struct {
	store                  DataLoaderStore
	GetRecord              *dataloadgen.Loader[uuid.UUID, chain.RecordNode]
	GetRecordInfoList      *dataloadgen.Loader[uuid.UUID, []domain.RecordInfo]
	GetTripAddressList     *dataloadgen.Loader[uuid.UUID, []domain.Address]
	GetRecordShouldPayList *dataloadgen.Loader[uuid.UUID, []domain.ExtendAddress]
	GetTripInfoList        *dataloadgen.Loader[uuid.UUID, *domain.TripInfo]
}

// NewTripDataLoader creates request-scoped loaders over the read-only store.
func NewTripDataLoader(dbWrapper DataLoaderStore) *TripDataLoader {
	return &TripDataLoader{
		store:                  dbWrapper,
		GetRecord:              dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.Records, dbWrapper.DataLoaderGetRecordList)),
		GetRecordInfoList:      dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.TripRecords, dbWrapper.DataLoaderGetRecordInfoList)),
		GetTripAddressList:     dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.TripAddresses, dbWrapper.DataLoaderGetTripAddressList)),
		GetRecordShouldPayList: dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.RecordShouldPays, dbWrapper.DataLoaderGetRecordShouldPayList)),
		GetTripInfoList:        dataloadgen.NewMappedLoader(instrumentMappedFetch(&DataLoaderDebug.Trips, dbWrapper.DataLoaderGetTripInfoList)),
	}
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
