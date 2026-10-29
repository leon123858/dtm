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
)

type Record interface {
	ID() uuid.UUID
	TripID() uuid.UUID
	Info() domain.RecordInfo
	DomainRecord() domain.Record
	IsActive() bool
	GetShouldPay() []domain.ExtendAddress
	Validate() (bool, error)
}

type RecordFactory interface {
	New(context.Context, uuid.UUID, domain.Record) (Record, error)
	Update(context.Context, uuid.UUID, domain.RecordPatch) (Record, error)
	ByID(context.Context, uuid.UUID) (Record, error)
	FromRecord(domain.Record) Record
}

type Trip interface {
	ID() uuid.UUID
	Info(context.Context) (*domain.TripInfo, error)
	UpdateInfo(context.Context, string) (*domain.TripInfo, error)
	Addresses(context.Context) ([]domain.Address, error)
	CreateAddressWithOptions(context.Context, *string, *bool, bool) (*domain.Address, error)
	PatchAddress(context.Context, uuid.UUID, *string, *bool) (*domain.Address, bool, error)
	CreateAddress(context.Context, string) (*domain.Address, error)
	UpdateAddress(context.Context, uuid.UUID, string) (*domain.Address, error)
	DeleteAddress(context.Context, uuid.UUID) (*domain.Address, error)
	Append(context.Context, Record) (AppendResult, error)
	List(context.Context) ([]Record, error)
	CalculateMoneyShare(context.Context) (MoneyShareResult, error)
	CalculateAddressStatistics(context.Context) (map[uuid.UUID]AddressStatistics, error)
}

// ReadOptions selects complete history or the live tails used by default.
type ReadOptions struct{ HaveHistory bool }

type TripFactory interface {
	db.BindingStore
	Create(context.Context, string) (Trip, error)
	ForTrip(uuid.UUID, ...ReadOptions) Trip
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

// AddressStatistics aggregates valid live records before settlement, including transfers.
// Addresses absent from the result have zero totals.
type AddressStatistics struct {
	TotalExpense      float64
	TotalPrepaid      float64
	ActiveRecordCount int32
}

func NewRecordFactory(readers db.ReaderProvider) RecordFactory {
	return &recordFactory{readers: readers}
}

func NewTripFactory(store db.TripStore, authStore db.AuthDBWrapper, readers db.ReaderProvider) TripFactory {
	return &tripFactory{BindingStore: store, store: store, authStore: authStore, readers: readers, policy: recordPolicy{}}
}
