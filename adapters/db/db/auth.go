package db

import (
	"context"
	"dtm/domain"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidGCID      = errors.New("invalid gcid")
	ErrInvalidAuthEntry = errors.New("invalid_login_code")
)

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
	UpsertUser(ctx context.Context, gcid string) (domain.User, error)

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
