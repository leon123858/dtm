package domain

import (
	"github.com/google/uuid"
	"time"
)

type User struct {
	ID   uuid.UUID
	GCID string
}

// SubscribedTrip is a user-specific trip subscription.
type SubscribedTrip struct {
	ID           uuid.UUID
	Name         string
	LastViewedAt time.Time
}
