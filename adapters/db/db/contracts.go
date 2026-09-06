package db

import (
	"context"
	"errors"

	"dtm/domain"

	"github.com/google/uuid"
)

var (
	ErrRecordNotFound       = errors.New("record not found")
	ErrTripNotFound         = errors.New("trip not found")
	ErrInvalidChain         = errors.New("invalid record chain")
	ErrMaterializerRequired = errors.New("record materializer is required")
)

// RecordSnapshot is fully materialized, including payer and all shares.
type RecordSnapshot struct {
	TripID uuid.UUID
	domain.Record
}

type RecordReadOptions struct{ HaveHistory bool }

type Reader interface {
	LoadTrip(context.Context, uuid.UUID) (*domain.TripInfo, error)
	LoadRecord(context.Context, uuid.UUID) (RecordSnapshot, error)
	LoadTripRecords(context.Context, uuid.UUID, RecordReadOptions) ([]RecordSnapshot, error)
	LoadTripAddresses(context.Context, uuid.UUID) ([]domain.Address, error)
}

type ReaderProvider func(context.Context) (Reader, error)

// RecordMaterializer runs inside the store's append lock or transaction.
// Errors abort the write and are returned unchanged to the caller.
type RecordMaterializer interface {
	PrepareNew(domain.Record, []domain.Address) (domain.Record, error)
	ApplyPatch(domain.Record, domain.RecordPatch, []domain.Address) (domain.Record, bool, error)
}

type RecordStore interface {
	AppendNew(context.Context, uuid.UUID, domain.Record, RecordMaterializer) (domain.Record, error)
	AppendPatch(context.Context, uuid.UUID, uuid.UUID, domain.RecordPatch, RecordMaterializer) (uuid.UUID, domain.Record, bool, error)
}

type TripStore interface {
	RecordStore
	CreateTrip(*domain.TripInfo) error
	UpdateTripInfo(*domain.TripInfo) error
	CreateAddress(uuid.UUID, string) (*domain.Address, error)
	UpdateAddress(uuid.UUID, uuid.UUID, string) (*domain.Address, error)
	DeleteAddress(uuid.UUID, uuid.UUID) (*domain.Address, error)
}

// DataLoaderStore is the read-only batching contract used by request-scoped
// GraphQL loaders and the chain Reader adapter.
type DataLoaderStore interface {
	DataLoaderGetTripRecords(ctx context.Context, tripIds []uuid.UUID, options RecordReadOptions) (map[uuid.UUID][]RecordSnapshot, error)
	DataLoaderGetRecordList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID]RecordSnapshot, error)
	DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error)
	DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error)
}
