package mem

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"

	// Assuming this library is used for dataloaders
	dbt "dtm/db/db" // Alias the db package as dbt
)

// inMemoryTripDBWrapper is an in-memory implementation of dbt.TripDBWrapper.
// It uses maps to store data for quick lookups.
type inMemoryTripDBWrapper struct {
	// Using maps to store dbt.TripInfo and TripData by Trip ID.
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

// --- Create Operations ---

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

	// Append new records and also add them to the flat recordsByID map.
	for _, record := range records {
		recordCopy := record // Create a copy for the map
		tripData.Records = append(tripData.Records, recordCopy)
	}
	return nil
}

// --- Read Operations ---

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
func (db *inMemoryTripDBWrapper) GetTripRecords(id uuid.UUID) ([]dbt.RecordInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip data with ID %s not found", id)
	}

	// Convert Record to RecordInfo for the return type
	recordInfos := make([]dbt.RecordInfo, len(tripData.Records))
	for i, r := range tripData.Records {
		recordInfos[i] = r.RecordInfo
	}
	return recordInfos, nil
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

// GetRecordAddressList retrieves the ShouldPayAddress list for a given record ID.
func (db *inMemoryTripDBWrapper) GetRecordAddressList(recordID uuid.UUID) ([]dbt.ExtendAddress, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	for _, tripData := range db.tripsData {
		for _, record := range tripData.Records {
			if record.ID == recordID {
				// Return a copy of the ShouldPayAddress list to prevent external modification
				addressListCopy := make([]dbt.ExtendAddress, len(record.ShouldPayAddress))
				copy(addressListCopy, record.ShouldPayAddress)
				return addressListCopy, nil
			}
		}
	}

	// If we reach here, the record was not found in any trip
	return nil, fmt.Errorf("record with ID %s not found", recordID)
}

// --- Update Operations ---

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
// This function updates both the RecordInfo and RecordData parts.
// Return trip ID if the record was found and updated, or an error if not found.
func (db *inMemoryTripDBWrapper) UpdateTripRecord(record *dbt.Record) (uuid.UUID, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Update the RecordInfo in trip data
	for tripID, tripData := range db.tripsData {
		foundIdx := -1
		for i, rec := range tripData.Records {
			if rec.ID == record.ID {
				foundIdx = i
				break
			}
		}
		if foundIdx != -1 {
			// Update the record in the trip data
			tripData.Records[foundIdx] = *record
			return tripID, nil // Record found and updated, exit early
		}
	}
	return uuid.Nil, fmt.Errorf("record with ID %s not found in any trip for update", record.ID)
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

	// scan all records to simulate delete cascade
	for idx, record := range tripData.Records {
		// println("Removing address from record", record.ID.String())
		for i, addr := range record.ShouldPayAddress {
			// println("Checking address in record", addr.Address)
			if addr.Address == address {
				// Remove the address from ShouldPayAddress
				tripData.Records[idx].ShouldPayAddress = append(record.ShouldPayAddress[:i], record.ShouldPayAddress[i+1:]...)
				break // Exit after removing the first occurrence
			}
		}
	}
	return nil
}

// --- Delete Operations ---

// DeleteTrip deletes a trip and all its associated data (info, records, address list).
func (db *inMemoryTripDBWrapper) DeleteTrip(id uuid.UUID) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// check if the trip exists
	if _, exists := db.tripsInfo[id]; !exists {
		return fmt.Errorf("trip with ID %s not found for deletion", id)
	}
	// Delete the trip info and data
	if _, exists := db.tripsData[id]; !exists {
		return fmt.Errorf("trip data with ID %s not found for deletion", id)
	}

	delete(db.tripsInfo, id)
	delete(db.tripsData, id)
	return nil
}

// DeleteTripRecord deletes a specific record from a trip.
func (db *inMemoryTripDBWrapper) DeleteTripRecord(recordID uuid.UUID) (uuid.UUID, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	found := false
	tripId := uuid.Nil // Initialize trip ID to return
	for id, tripData := range db.tripsData {
		foundIdx := -1
		for i, record := range tripData.Records {
			if record.ID == recordID {
				foundIdx = i
				tripId = id // Store the trip ID for return
				break
			}
		}

		if foundIdx != -1 {
			// Remove the record by slicing
			tripData.Records = append(tripData.Records[:foundIdx], tripData.Records[foundIdx+1:]...)
			found = true
			break // Record found and removed from one trip, assume unique record ID across trips
		}
	}

	if !found {
		return uuid.Nil, fmt.Errorf("record with ID %s not found for deletion", recordID)
	}

	return tripId, nil
}

// --- Data Loader Operations ---

// DataLoaderGetRecordInfoList retrieves a map of RecordInfo lists for given trip IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetRecordInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]dbt.RecordInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID][]dbt.RecordInfo)
	errors := make(map[uuid.UUID]error)

	for _, tripID := range tripIds {
		if tripData, exists := db.tripsData[tripID]; exists {
			recordInfos := make([]dbt.RecordInfo, len(tripData.Records))
			for i, r := range tripData.Records {
				recordInfos[i] = r.RecordInfo
			}
			result[tripID] = recordInfos
			errors[tripID] = nil // No error for this trip ID
		} else {
			// If a trip ID is not found, you might choose to return an empty slice or an error.
			// For a data loader, typically an empty slice is returned if no data exists for the key.
			result[tripID] = []dbt.RecordInfo{}
			errors[tripID] = fmt.Errorf("trip with ID %s not found", tripID)
		}
	}
	return result, dataloadgen.MappedFetchError[uuid.UUID](errors)
}

// DataLoaderGetTripAddressList retrieves a map of Address lists for given trip IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]dbt.Address, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID][]dbt.Address)
	errors := make(map[uuid.UUID]error)

	for _, tripID := range tripIds {
		if tripData, exists := db.tripsData[tripID]; exists {
			// Return a copy of the slice to prevent external modification
			addressListCopy := make([]dbt.Address, len(tripData.AddressList))
			copy(addressListCopy, tripData.AddressList)
			result[tripID] = addressListCopy
			errors[tripID] = nil // No error for this trip ID
		} else {
			result[tripID] = []dbt.Address{}
			errors[tripID] = fmt.Errorf("trip with ID %s not found", tripID)
		}
	}
	return result, dataloadgen.MappedFetchError[uuid.UUID](errors)
}

// DataLoaderGetRecordShouldPayList retrieves a map of ShouldPayAddress lists for given record IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetRecordShouldPayList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]dbt.ExtendAddress, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID][]dbt.ExtendAddress)
	errors := make(map[uuid.UUID]error)

	for _, recordID := range recordIds {
		found := false
		for _, tripData := range db.tripsData {
			for _, record := range tripData.Records {
				if record.ID == recordID {
					// Return a copy of the ShouldPayAddress list
					addressListCopy := make([]dbt.ExtendAddress, len(record.ShouldPayAddress))
					copy(addressListCopy, record.ShouldPayAddress)
					result[recordID] = addressListCopy
					errors[recordID] = nil // No error for this record ID
					found = true
					break // Record found, move to the next recordID
				}
			}
			if found {
				break
			}
		}
		if !found {
			result[recordID] = []dbt.ExtendAddress{}
			errors[recordID] = fmt.Errorf("record with ID %s not found", recordID)
		}
	}
	return result, dataloadgen.MappedFetchError[uuid.UUID](errors)
}

// DataLoaderGetTripInfoList retrieves a map of TripInfo pointers for given trip IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*dbt.TripInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID]*dbt.TripInfo)
	errors := make(map[uuid.UUID]error)

	for _, tripID := range tripIds {
		if tripInfo, exists := db.tripsInfo[tripID]; exists {
			// Return a copy to prevent external modification
			infoCopy := *tripInfo
			result[tripID] = &infoCopy
			errors[tripID] = nil // No error for this trip ID
		} else {
			// If a trip ID is not found, typically nil is returned for that specific key.
			result[tripID] = nil
			errors[tripID] = fmt.Errorf("trip with ID %s not found", tripID)
		}
	}

	return result, dataloadgen.MappedFetchError[uuid.UUID](errors)
}
