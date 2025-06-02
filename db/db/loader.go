package db

import (
	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
)

type dataLoaderKey string

const (
	DataLoaderKeyTripData dataLoaderKey = "trip_data_loader"
)

// dataLoader, ok := ctx.Value(db.DataLoaderKeyTripData).(db.TripDataLoader)
//
//	if !ok {
//		return nil, fmt.Errorf("data loader is not available")
//	}
type TripDataLoader struct {
	GetRecordInfoList      *dataloadgen.Loader[uuid.UUID, []RecordInfo]
	GetTripAddressList     *dataloadgen.Loader[uuid.UUID, []Address]
	GetRecordShouldPayList *dataloadgen.Loader[uuid.UUID, []Address]
	GetTripInfoList        *dataloadgen.Loader[uuid.UUID, *TripInfo]
}

func NewTripDataLoader(dbWrapper TripDBWrapper) *TripDataLoader {
	return &TripDataLoader{
		GetRecordInfoList:      dataloadgen.NewMappedLoader(dbWrapper.DataLoaderGetRecordInfoList),
		GetTripAddressList:     dataloadgen.NewMappedLoader(dbWrapper.DataLoaderGetTripAddressList),
		GetRecordShouldPayList: dataloadgen.NewMappedLoader(dbWrapper.DataLoaderGetRecordShouldPayList),
		GetTripInfoList:        dataloadgen.NewMappedLoader(dbWrapper.DataLoaderGetTripInfoList),
	}
}
