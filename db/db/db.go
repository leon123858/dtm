package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/r3labs/diff/v3"
)

type TripDBWrapper interface {
	// CreateTrip Create
	CreateTrip(info *TripInfo) error
	// CreateTripRecords Create
	CreateTripRecords(id uuid.UUID, records []Record) error
	// GetTripInfo Read
	GetTripInfo(id uuid.UUID) (*TripInfo, error)
	// GetTripRecords Read
	GetTripRecords(id uuid.UUID) ([]RecordInfo, error)
	// GetTripAddressList Read
	GetTripAddressList(id uuid.UUID) ([]Address, error)
	// GetRecordAddressList Read
	GetRecordAddressList(recordID uuid.UUID) ([]ExtendAddress, error)
	// UpdateTripInfo Update
	UpdateTripInfo(info *TripInfo) error
	// UpdateTripRecord	Update
	UpdateTripRecord(recordID uuid.UUID, changeLog diff.Changelog) (uuid.UUID, error)
	// TripAddressListAdd Update
	TripAddressListAdd(id uuid.UUID, address Address) error
	// TripAddressListRemove Update
	TripAddressListRemove(id uuid.UUID, address Address) error
	// DeleteTrip Delete
	DeleteTrip(id uuid.UUID) error
	// DeleteTripRecord Delete
	DeleteTripRecord(recordID uuid.UUID) (uuid.UUID, error)
	// DataLoaderGetRecordInfoList DataLoader
	DataLoaderGetRecordInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]RecordInfo, error)
	// DataLoaderGetTripAddressList DataLoader
	DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]Address, error)
	// DataLoaderGetRecordShouldPayList DataLoader
	DataLoaderGetRecordShouldPayList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]ExtendAddress, error)
	// DataLoaderGetTripInfoList DataLoader
	DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*TripInfo, error)
}
