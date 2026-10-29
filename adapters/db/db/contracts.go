package db

import (
	"context"
	"errors"
	"time"

	"dtm/domain"

	"github.com/google/uuid"
)

var (
	ErrInvalidGCID          = errors.New("invalid gcid")
	ErrInvalidAuthEntry     = errors.New("invalid_login_code")
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

type BindingStore interface {
	PatchTripInfo(context.Context, uuid.UUID, *string, *bool) (*domain.TripInfo, error)
	CheckTripAccess(context.Context, uuid.UUID) error
	Binding(context.Context, uuid.UUID) (*domain.TripBinding, error)
	Bind(context.Context, uuid.UUID, *uuid.UUID) error
	Unbind(context.Context, uuid.UUID) error
	SetTripPrivate(context.Context, uuid.UUID, bool) (*domain.TripInfo, error)
}
type TripStore interface {
	BindingStore
	RecordStore
	CreateTrip(*domain.TripInfo) error
	UpdateTripInfo(context.Context, *domain.TripInfo) error
	CreateAddress(context.Context, uuid.UUID, domain.CreateAddressOption) (*domain.Address, error)
	UpdateAddress(context.Context, uuid.UUID, uuid.UUID, domain.AddressOption) (*domain.Address, bool, error)
	DeleteAddress(context.Context, uuid.UUID, uuid.UUID) (*domain.Address, error)
}

// DataLoaderStore is the read-only batching contract used by request-scoped
// GraphQL loaders and the chain Reader adapter.
type BoundUser struct {
	TripID    uuid.UUID
	AddressID *uuid.UUID
	UserID    uuid.UUID
	UserName  *string
}

// TripUsers contains the member list and an index of claimed addresses.
// Adapters assemble both projections before the loader caches them.
type TripUsers struct {
	Users     []BoundUser
	Addresses map[string]BoundUser
}

// NewTripUsers indexes members without excluding members lacking an address.
func NewTripUsers(users []BoundUser) TripUsers {
	if users == nil {
		users = []BoundUser{}
	}
	result := TripUsers{Users: users, Addresses: make(map[string]BoundUser)}
	for _, user := range users {
		if user.AddressID != nil {
			result.Addresses[user.AddressID.String()] = user
		}
	}
	return result
}

type DataLoaderStore interface {
	DataLoaderGetTripUsers(context.Context, []uuid.UUID) (map[uuid.UUID]TripUsers, error)
	DataLoaderGetTripRecords(ctx context.Context, tripIds []uuid.UUID, options RecordReadOptions) (map[uuid.UUID][]RecordSnapshot, error)
	DataLoaderGetRecordList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID]RecordSnapshot, error)
	DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error)
	DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error)
}

// AuthDBWrapper stores permanent users and subscriptions alongside temporary
// OAuth flow locks. Lock keys and browser bindings are hashes, never credentials.
// Implementations share trip storage with TripDBWrapper.
type AuthDBWrapper interface {
	// PutLock creates a unique, expiring flow record; duplicate keys fail.
	PutLock(ctx context.Context, kind, key, binding string, data []byte, expires time.Time) error
	// TakeLock atomically checks binding/expiry and consumes the record once.
	// These locks coordinate requests; they do not provide waiting or lease renewal.
	TakeLock(ctx context.Context, kind, key, binding string, now time.Time) ([]byte, error)
	// CleanupLock removes expired flow records, never permanent user data.
	CleanupLock(ctx context.Context, now time.Time) error
	// UpsertUser returns the same UUID for a GCID, including concurrent calls.
	UpsertUser(ctx context.Context, gcid string, email ...string) (domain.User, error)

	GetUser(context.Context, uuid.UUID) (domain.User, error)
	UpdateUser(context.Context, uuid.UUID, *string, *bool) (domain.User, error)
	UserGCID2UserId(ctx context.Context, gcid string) (uuid.UUID, error)
	SubscribedTrips(ctx context.Context, userID uuid.UUID) ([]domain.SubscribedTrip, error)
	// TouchSubscribedTrip advances the timestamp without creating a subscription.
	TouchSubscribedTrip(ctx context.Context, userID, tripID uuid.UUID) error
	// Subscription changes are idempotent; subscribing again preserves the timestamp.
	SubscribeTrip(ctx context.Context, userID, tripID uuid.UUID) error
	UnsubscribeTrip(ctx context.Context, userID, tripID uuid.UUID) error
	// CreateSubscribedTrip atomically creates the trip and its subscription.
	CreateSubscribedTrip(ctx context.Context, userID uuid.UUID, info *domain.TripInfo) error
}

type TripDBWrapper interface {
	TripStore
	DataLoaderStore
	DeleteTrip(ctx context.Context, id uuid.UUID) error
}
