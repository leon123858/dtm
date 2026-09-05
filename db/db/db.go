package db

import (
	"github.com/google/uuid"
)

type TripDBWrapper interface {
	TripStore
	DataLoaderStore
	DeleteTrip(id uuid.UUID) error
}