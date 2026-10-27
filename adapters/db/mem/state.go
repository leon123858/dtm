package mem

import (
	"dtm/adapters/db/db"
	"dtm/domain"
	"github.com/google/uuid"
	"sync"
	"time"
)

type memoryState struct {
	bindings map[uuid.UUID]map[uuid.UUID]*uuid.UUID
	// Using maps to store domain.TripInfo and TripData by Trip ID.
	users         map[uuid.UUID]domain.User
	userIDsByGCID map[string]uuid.UUID
	subscriptions map[uuid.UUID]map[uuid.UUID]time.Time
	tripsInfo     map[uuid.UUID]*domain.TripInfo
	tripsData     map[uuid.UUID]*domain.TripData // Stores records and address lists for each trip

	// Mutex for thread-safety, important for concurrent access in a real application.
	mu      sync.RWMutex
	entries map[string]authEntry
}

func newMemoryState() *memoryState {
	return &memoryState{
		bindings:      make(map[uuid.UUID]map[uuid.UUID]*uuid.UUID),
		users:         make(map[uuid.UUID]domain.User),
		userIDsByGCID: make(map[string]uuid.UUID),
		subscriptions: make(map[uuid.UUID]map[uuid.UUID]time.Time),
		tripsInfo:     make(map[uuid.UUID]*domain.TripInfo),
		tripsData:     make(map[uuid.UUID]*domain.TripData),
		entries:       make(map[string]authEntry),
	}
}

// NewInMemoryDB returns adapters sharing data and one transaction lock.
func NewInMemoryDB() (db.TripDBWrapper, db.AuthDBWrapper) {
	state := newMemoryState()
	return &inMemoryTripDBWrapper{state}, &inMemoryAuthDBWrapper{state}
}
