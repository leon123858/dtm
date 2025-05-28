package mem

import (
	"fmt"
	"sync"

	"github.com/google/uuid"

	dbt "dtm/db/db"
)

// inMemoryTripDBWrapper is an in-memory implementation of dbt.TripDBWrapper.
// It uses maps to store data for quick lookups.
type inMemoryTripDBWrapper struct {
	// Using maps to store dbt.TripInfo and TripData by Trip ID.
	// In a real application, you might want to store the full Trip struct
	// or separate components based on access patterns.
	tripsInfo map[uuid.UUID]*dbt.TripInfo
	tripsData map[uuid.UUID]*dbt.TripData // Stores records and address lists for each trip

	// Mutex for thread-safety, important for concurrent access in a real application.
	mu sync.RWMutex
}

// NewInMemoryTripDBWrapper creates and returns a new instance of inMemoryTripDBWrapper.
func NewInMemoryTripDBWrapper() dbt.TripDBWrapper {
	return &inMemoryTripDBWrapper{
		tripsInfo: make(map[uuid.UUID]*dbt.TripInfo),
		tripsData: make(map[uuid.UUID]*dbt.TripData),
	}
}

// CreateTrip creates a new trip entry in memory.
func (db *inMemoryTripDBWrapper) CreateTrip(info *dbt.TripInfo) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tripsInfo[info.ID]; exists {
		return fmt.Errorf("trip with ID %s already exists", info.ID)
	}

	// Store a copy to prevent external modification of the original info pointer
	infoCopy := *info
	db.tripsInfo[info.ID] = &infoCopy
	db.tripsData[info.ID] = &dbt.TripData{
		Records:     []dbt.Record{},
		AddressList: []dbt.Address{},
	}
	return nil
}

// CreateTripRecords adds a slice of records to an existing trip.
func (db *inMemoryTripDBWrapper) CreateTripRecords(id uuid.UUID, records []dbt.Record) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return fmt.Errorf("trip with ID %s not found", id)
	}

	// Append new records. Make a copy of records to avoid modifying the original slice
	// if it's reused elsewhere.
	tripData.Records = append(tripData.Records, records...)
	return nil
}

// GetTripInfo retrieves trip information by ID.
func (db *inMemoryTripDBWrapper) GetTripInfo(id uuid.UUID) (*dbt.TripInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	info, exists := db.tripsInfo[id]
	if !exists {
		return nil, fmt.Errorf("trip info with ID %s not found", id)
	}
	// Return a copy to prevent external modification
	infoCopy := *info
	return &infoCopy, nil
}

// GetTripRecords retrieves all records for a given trip ID.
func (db *inMemoryTripDBWrapper) GetTripRecords(id uuid.UUID) ([]dbt.Record, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip data with ID %s not found", id)
	}

	// Return a copy of the slice to prevent external modification
	recordsCopy := make([]dbt.Record, len(tripData.Records))
	copy(recordsCopy, tripData.Records)
	return recordsCopy, nil
}

// GetTripAddressList retrieves the address list for a given trip ID.
func (db *inMemoryTripDBWrapper) GetTripAddressList(id uuid.UUID) ([]dbt.Address, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip data with ID %s not found", id)
	}

	// Return a copy of the slice to prevent external modification
	addressListCopy := make([]dbt.Address, len(tripData.AddressList))
	copy(addressListCopy, tripData.AddressList)
	return addressListCopy, nil
}

// UpdateTripInfo updates the information of an existing trip.
func (db *inMemoryTripDBWrapper) UpdateTripInfo(info *dbt.TripInfo) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tripsInfo[info.ID]; !exists {
		return fmt.Errorf("trip with ID %s not found for update", info.ID)
	}

	// Update the existing info
	infoCopy := *info
	db.tripsInfo[info.ID] = &infoCopy
	return nil
}

// UpdateTripRecord updates a specific record within a trip.
// This assumes the record's ID is unique within the context of a trip's records.
func (db *inMemoryTripDBWrapper) UpdateTripRecord(record dbt.Record) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	found := false
	for _, tripData := range db.tripsData {
		for i, r := range tripData.Records {
			if r.ID == record.ID {
				tripData.Records[i] = record // Update the record
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return fmt.Errorf("record with ID %s not found for update in any trip", record.ID)
	}
	return nil
}

// TripAddressListAdd adds an address to a trip's address list.
func (db *inMemoryTripDBWrapper) TripAddressListAdd(id uuid.UUID, address dbt.Address) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return fmt.Errorf("trip with ID %s not found", id)
	}

	// Check if address already exists to avoid duplicates
	for _, addr := range tripData.AddressList {
		if addr == address {
			return fmt.Errorf("address %s already exists in trip %s", address, id)
		}
	}

	tripData.AddressList = append(tripData.AddressList, address)
	return nil
}

// TripAddressListRemove removes an address from a trip's address list.
func (db *inMemoryTripDBWrapper) TripAddressListRemove(id uuid.UUID, address dbt.Address) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return fmt.Errorf("trip with ID %s not found", id)
	}

	foundIdx := -1
	for i, addr := range tripData.AddressList {
		if addr == address {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		return fmt.Errorf("address %s not found in trip %s", address, id)
	}

	// Remove the address by slicing
	tripData.AddressList = append(tripData.AddressList[:foundIdx], tripData.AddressList[foundIdx+1:]...)
	return nil
}

// DeleteTrip deletes a trip and all its associated data (info, records, address list).
func (db *inMemoryTripDBWrapper) DeleteTrip(id uuid.UUID) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tripsInfo[id]; !exists {
		return fmt.Errorf("trip with ID %s not found for deletion", id)
	}

	delete(db.tripsInfo, id)
	delete(db.tripsData, id)
	return nil
}

// DeleteTripRecord deletes a specific record from a trip.
func (db *inMemoryTripDBWrapper) DeleteTripRecord(tripID uuid.UUID, recordID uuid.UUID) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[tripID]
	if !exists {
		return fmt.Errorf("trip with ID %s not found", tripID)
	}

	foundIdx := -1
	for i, record := range tripData.Records {
		if record.ID == recordID {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		return fmt.Errorf("record with ID %s not found in trip %s", recordID, tripID)
	}

	// Remove the record by slicing
	tripData.Records = append(tripData.Records[:foundIdx], tripData.Records[foundIdx+1:]...)
	return nil
}
