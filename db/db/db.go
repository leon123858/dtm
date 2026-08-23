package db

import (
	"context"
	"dtm/domain"

	"github.com/google/uuid"
	"github.com/r3labs/diff/v3"
)

type TripDBWrapper interface {
	// CreateTrip Create
	CreateTrip(info *domain.TripInfo) error
	// CreateTripRecords Create
	CreateTripRecords(id uuid.UUID, records []domain.Record) error
	// GetTripInfo Read
	GetTripInfo(id uuid.UUID) (*domain.TripInfo, error)
	// GetTripRecords Read
	GetTripRecords(id uuid.UUID) ([]domain.RecordInfo, error)
	// GetTripAddressList Read
	GetTripAddressList(id uuid.UUID) ([]domain.Address, error)
	// GetRecordAddressList Read
	GetRecordAddressList(recordID uuid.UUID) ([]domain.ExtendAddress, error)
	// GetRecordTripID returns the owning trip without mutating the record.
	GetRecordTripID(recordID uuid.UUID) (uuid.UUID, error)
	// UpdateTripInfo Update
	UpdateTripInfo(info *domain.TripInfo) error
	// UpdateTripRecord	Update
	UpdateTripRecord(recordID uuid.UUID, changeLog diff.Changelog) (uuid.UUID, error)
	// CreateAddress creates a trip-scoped address.
	CreateAddress(tripID uuid.UUID, name string) (*domain.Address, error)
	// UpdateAddress renames a trip-scoped address.
	UpdateAddress(tripID, addressID uuid.UUID, name string) (*domain.Address, error)
	// DeleteAddress deletes an unreferenced trip-scoped address.
	DeleteAddress(tripID, addressID uuid.UUID) (*domain.Address, error)
	// DeleteTrip Delete
	DeleteTrip(id uuid.UUID) error
	// DeleteTripRecord Delete
	DeleteTripRecord(recordID uuid.UUID) (uuid.UUID, error)
	// DataLoaderGetRecordInfoList DataLoader
	DataLoaderGetRecordInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.RecordInfo, error)
	// DataLoaderGetTripAddressList DataLoader
	DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error)
	// DataLoaderGetRecordShouldPayList DataLoader
	DataLoaderGetRecordShouldPayList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]domain.ExtendAddress, error)
	// DataLoaderGetTripInfoList DataLoader
	DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error)
}
