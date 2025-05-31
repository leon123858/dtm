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
	GetTripRecords(id uuid.UUID) ([]Record, error)
	GetTripAddressList(id uuid.UUID) ([]Address, error)
	GetRecordAddressList(recordID uuid.UUID) ([]Address, error)
	// Update
	UpdateTripInfo(info *TripInfo) error
	UpdateTripRecord(record Record) error
	TripAddressListAdd(id uuid.UUID, address Address) error
	TripAddressListRemove(id uuid.UUID, address Address) error
	// Delete
	DeleteTrip(id uuid.UUID) error
	DeleteTripRecord(recordID uuid.UUID) error
	// Data Loader
	DataLoaderGetRecordList(ctx context.Context, keys []uuid.UUID) (map[uuid.UUID]Record, error)
}
