// Package trip owns trip management, record-chain behavior, and settlement through small
// interfaces. Concrete implementations are intentionally private.
package trip

import (
	"context"
	"errors"

	"dtm/adapters/db/db"
	"dtm/domain"
	"dtm/services/tx"

	"github.com/google/uuid"
)

var (
	ErrInvalidRecordSnapshot  = errors.New("invalid record snapshot")
	ErrInvalidRecordAddresses = errors.New("invalid record addresses")
	ErrRecordNotFound         = errors.New("record not found")
	ErrTripNotFound           = errors.New("trip not found")
	ErrInvalidChain           = errors.New("invalid record chain")
)

type Record interface {
	ID() uuid.UUID
	TripID() uuid.UUID
	Info() domain.RecordInfo
	DomainRecord() domain.Record
	IsActive() bool
	EventValid() bool
	GetShouldPay(context.Context) ([]domain.ExtendAddress, error)
	Validate(context.Context) (bool, error)
}

type RecordFactory interface {
	New(context.Context, uuid.UUID, domain.Record) (Record, error)
	Update(context.Context, uuid.UUID, domain.RecordPatch) (Record, error)
	ByID(context.Context, uuid.UUID) (Record, error)
	FromInfo(domain.RecordInfo, bool) Record
}

type Trip interface {
	ID() uuid.UUID
	Info(context.Context) (*domain.TripInfo, error)
	UpdateInfo(context.Context, string) (*domain.TripInfo, error)
	Addresses(context.Context) ([]domain.Address, error)
	CreateAddress(context.Context, string) (*domain.Address, error)
	UpdateAddress(context.Context, uuid.UUID, string) (*domain.Address, error)
	DeleteAddress(context.Context, uuid.UUID) (*domain.Address, error)
	Append(context.Context, Record) (AppendResult, error)
	List(context.Context) ([]Record, error)
	CalculateMoneyShare(context.Context) (MoneyShareResult, error)
}

type TripFactory interface {
	Create(context.Context, string) (Trip, error)
	ForTrip(uuid.UUID) Trip
}

type AppendResult struct {
	TripID   uuid.UUID
	Record   Record
	Appended bool
}

type MoneyShareResult struct {
	Package        tx.Package
	TotalRemaining float64
	Valid          bool
}

func NewRecordFactory(readers db.ReaderProvider) RecordFactory {
	return &recordFactory{readers: readers}
}

func NewTripFactory(store db.TripStore, readers db.ReaderProvider) TripFactory {
	return &tripFactory{store: store, readers: readers, policy: recordPolicy{}}
}
