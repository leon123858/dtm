package db

import (
	"context"

	"github.com/google/uuid"
)

type TripDBWrapper interface {
	// Create
	CreateTrip(info *TripInfo) error
	CreateTripRecords(id uuid.UUID, records []Record) error
	// Read
	GetTripInfo(id uuid.UUID) (*TripInfo, error)
	GetTripRecords(id uuid.UUID) ([]RecordInfo, error)
	GetTripAddressList(id uuid.UUID) ([]Address, error)
	GetRecordAddressList(recordID uuid.UUID) ([]Address, error)
	// Update
	UpdateTripInfo(info *TripInfo) error
	UpdateTripRecord(record *Record) (uuid.UUID, error)
	TripAddressListAdd(id uuid.UUID, address Address) error
	TripAddressListRemove(id uuid.UUID, address Address) error
	// Delete
	DeleteTrip(id uuid.UUID) error
	DeleteTripRecord(recordID uuid.UUID) (uuid.UUID, error)
	// Data Loader
	DataLoaderGetRecordInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]RecordInfo, error)
	DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]Address, error)
	DataLoaderGetRecordShouldPayList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]Address, error)
	DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*TripInfo, error)
}
