package db

import (
	"context"
	"dtm/chain"
	"dtm/domain"

	"github.com/google/uuid"
)

type TripDBWrapper interface {
	chain.Store
	DataLoaderStore
	CreateTrip(info *domain.TripInfo) error
	UpdateTripInfo(info *domain.TripInfo) error
	CreateAddress(tripID uuid.UUID, name string) (*domain.Address, error)
	UpdateAddress(tripID, addressID uuid.UUID, name string) (*domain.Address, error)
	DeleteAddress(tripID, addressID uuid.UUID) (*domain.Address, error)
	DeleteTrip(id uuid.UUID) error
}

// DataLoaderStore is the read-only batching contract used by request-scoped
// GraphQL loaders and the chain Reader adapter.
type DataLoaderStore interface {
	DataLoaderGetRecordInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.RecordInfo, error)
	DataLoaderGetRecordList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID]chain.RecordNode, error)
	DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error)
	DataLoaderGetRecordShouldPayList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]domain.ExtendAddress, error)
	DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error)
}
