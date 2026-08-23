package mem

import (
	"context"
	"fmt"
	"sync"

	"dtm/db/db"
	"dtm/domain"
	cdiff "dtm/libs/diff"

	"github.com/google/uuid"
	"github.com/r3labs/diff/v3"
	"github.com/vikstrous/dataloadgen"
)

// inMemoryTripDBWrapper is an in-memory implementation of db.TripDBWrapper.
// It uses maps to store data for quick lookups.
type inMemoryTripDBWrapper struct {
	// Using maps to store domain.TripInfo and TripData by Trip ID.
	tripsInfo map[uuid.UUID]*domain.TripInfo
	tripsData map[uuid.UUID]*domain.TripData // Stores records and address lists for each trip

	// Mutex for thread-safety, important for concurrent access in a real application.
	mu sync.RWMutex
}

// NewInMemoryTripDBWrapper creates and returns a new instance of inMemoryTripDBWrapper.
func NewInMemoryTripDBWrapper() db.TripDBWrapper {
	return &inMemoryTripDBWrapper{
		tripsInfo: make(map[uuid.UUID]*domain.TripInfo),
		tripsData: make(map[uuid.UUID]*domain.TripData),
	}
}

// --- Create Operations ---

// CreateTrip creates a new trip entry in memory.
func (db *inMemoryTripDBWrapper) CreateTrip(info *domain.TripInfo) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tripsInfo[info.ID]; exists {
		return fmt.Errorf("trip with ID %s already exists", info.ID)
	}

	// Store a copy to prevent external modification of the original info pointer
	infoCopy := *info
	db.tripsInfo[info.ID] = &infoCopy
	db.tripsData[info.ID] = &domain.TripData{
		Records:     []domain.Record{},
		AddressList: []domain.Address{},
	}
	return nil
}

// CreateTripRecords adds a slice of records to an existing trip.
func (db *inMemoryTripDBWrapper) CreateTripRecords(id uuid.UUID, records []domain.Record) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return fmt.Errorf("trip with ID %s not found", id)
	}

	canonicalRecords := make([]domain.Record, len(records))
	for i := range records {
		recordCopy := records[i]
		recordCopy.ShouldPayAddress = append([]domain.ExtendAddress(nil), records[i].ShouldPayAddress...)
		if err := canonicalizeRecordAddresses(tripData, &recordCopy); err != nil {
			return err
		}
		canonicalRecords[i] = recordCopy
	}

	// Persist only after the complete batch has passed validation.
	tripData.Records = append(tripData.Records, canonicalRecords...)
	return nil
}

// --- Read Operations ---

// GetTripInfo retrieves trip information by ID.
func (db *inMemoryTripDBWrapper) GetTripInfo(id uuid.UUID) (*domain.TripInfo, error) {
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
func (db *inMemoryTripDBWrapper) GetTripRecords(id uuid.UUID) ([]domain.RecordInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip data with ID %s not found", id)
	}

	// Convert Record to RecordInfo for the return type
	recordInfos := make([]domain.RecordInfo, len(tripData.Records))
	for i, r := range tripData.Records {
		recordInfos[i] = r.RecordInfo
	}
	return recordInfos, nil
}

// GetTripAddressList retrieves the address list for a given trip ID.
func (db *inMemoryTripDBWrapper) GetTripAddressList(id uuid.UUID) ([]domain.Address, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip data with ID %s not found", id)
	}

	// Return a copy of the slice to prevent external modification
	addressListCopy := make([]domain.Address, len(tripData.AddressList))
	copy(addressListCopy, tripData.AddressList)
	return addressListCopy, nil
}

// GetRecordAddressList retrieves the ShouldPayAddress list for a given record ID.
func (db *inMemoryTripDBWrapper) GetRecordAddressList(recordID uuid.UUID) ([]domain.ExtendAddress, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	for _, tripData := range db.tripsData {
		for _, record := range tripData.Records {
			if record.ID == recordID {
				// Return a copy of the ShouldPayAddress list to prevent external modification
				addressListCopy := make([]domain.ExtendAddress, len(record.ShouldPayAddress))
				copy(addressListCopy, record.ShouldPayAddress)
				return addressListCopy, nil
			}
		}
	}

	// If we reach here, the record was not found in any trip
	return nil, fmt.Errorf("record with ID %s not found", recordID)
}

func (db *inMemoryTripDBWrapper) GetRecordTripID(recordID uuid.UUID) (uuid.UUID, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	for tripID, tripData := range db.tripsData {
		for _, record := range tripData.Records {
			if record.ID == recordID {
				return tripID, nil
			}
		}
	}
	return uuid.Nil, fmt.Errorf("record with ID %s not found", recordID)
}

// --- Update Operations ---

// UpdateTripInfo updates the information of an existing trip.
func (db *inMemoryTripDBWrapper) UpdateTripInfo(info *domain.TripInfo) error {
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
func (db *inMemoryTripDBWrapper) UpdateTripRecord(recordID uuid.UUID, changeLog diff.Changelog) (uuid.UUID, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Update the RecordInfo in trip data
	for tripID, tripData := range db.tripsData {
		foundIdx := -1
		for i, rec := range tripData.Records {
			if rec.ID == recordID {
				foundIdx = i
				break
			}
		}
		if foundIdx != -1 {
			// Apply and validate against a copy so a rejected update is atomic.
			updatedRecord := tripData.Records[foundIdx]
			updatedRecord.ShouldPayAddress = append(
				[]domain.ExtendAddress(nil),
				tripData.Records[foundIdx].ShouldPayAddress...,
			)
			pl := cdiff.GetCustomDiffer().Patch(changeLog, &updatedRecord)
			if pl.HasErrors() {
				return uuid.Nil, fmt.Errorf("trip with ID %s update fail", recordID)
			}
			// Remove empty UUIDs (patch cannot decrease array/map length).
			tmpAddrArray := make([]domain.ExtendAddress, 0, len(updatedRecord.ShouldPayAddress))
			for _, extAddr := range updatedRecord.ShouldPayAddress {
				if extAddr.Address.ID != uuid.Nil {
					tmpAddrArray = append(tmpAddrArray, extAddr)
				}
			}
			// set new array
			updatedRecord.ShouldPayAddress = tmpAddrArray
			if err := canonicalizeRecordAddresses(tripData, &updatedRecord); err != nil {
				return uuid.Nil, err
			}
			tripData.Records[foundIdx] = updatedRecord

			return tripID, nil // Record found and updated, exit early
		}
	}
	return uuid.Nil, fmt.Errorf("record with ID %s not found in any trip for update", recordID)
}

func (db *inMemoryTripDBWrapper) CreateAddress(id uuid.UUID, name string) (*domain.Address, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip with ID %s not found", id)
	}

	// Check if address already exists to avoid duplicates
	for _, addr := range tripData.AddressList {
		if addr.Name == name {
			return nil, fmt.Errorf("address name %q already exists in trip %s", name, id)
		}
	}

	address := domain.Address{ID: uuid.New(), Name: name}
	tripData.AddressList = append(tripData.AddressList, address)
	return &address, nil
}

func (db *inMemoryTripDBWrapper) UpdateAddress(id, addressID uuid.UUID, name string) (*domain.Address, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip with ID %s not found", id)
	}
	for _, addr := range tripData.AddressList {
		if addr.Name == name && addr.ID != addressID {
			return nil, fmt.Errorf("address name %q already exists in trip %s", name, id)
		}
	}
	for i := range tripData.AddressList {
		if tripData.AddressList[i].ID == addressID {
			tripData.AddressList[i].Name = name
			updated := tripData.AddressList[i]
			for recordIdx := range tripData.Records {
				if tripData.Records[recordIdx].PrePayAddress.ID == addressID {
					tripData.Records[recordIdx].PrePayAddress = updated
				}
				for shouldIdx := range tripData.Records[recordIdx].ShouldPayAddress {
					if tripData.Records[recordIdx].ShouldPayAddress[shouldIdx].Address.ID == addressID {
						tripData.Records[recordIdx].ShouldPayAddress[shouldIdx].Address = updated
					}
				}
			}
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("address %s not found in trip %s", addressID, id)
}

func (db *inMemoryTripDBWrapper) DeleteAddress(id, addressID uuid.UUID) (*domain.Address, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	tripData, exists := db.tripsData[id]
	if !exists {
		return nil, fmt.Errorf("trip with ID %s not found", id)
	}
	for _, record := range tripData.Records {
		if record.PrePayAddress.ID == addressID {
			return nil, fmt.Errorf("address %s is referenced by record %s", addressID, record.ID)
		}
		for _, address := range record.ShouldPayAddress {
			if address.Address.ID == addressID {
				return nil, fmt.Errorf("address %s is referenced by record %s", addressID, record.ID)
			}
		}
	}
	for i, address := range tripData.AddressList {
		if address.ID == addressID {
			tripData.AddressList = append(tripData.AddressList[:i], tripData.AddressList[i+1:]...)
			return &address, nil
		}
	}
	return nil, fmt.Errorf("address %s not found in trip %s", addressID, id)
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

func canonicalizeRecordAddresses(tripData *domain.TripData, record *domain.Record) error {
	addresses := make(map[uuid.UUID]domain.Address, len(tripData.AddressList))
	for _, address := range tripData.AddressList {
		addresses[address.ID] = address
	}
	prePay, ok := addresses[record.PrePayAddress.ID]
	if !ok {
		return fmt.Errorf("pre-pay address %s does not belong to trip", record.PrePayAddress.ID)
	}
	record.PrePayAddress = prePay
	seen := make(map[uuid.UUID]struct{}, len(record.ShouldPayAddress))
	for i := range record.ShouldPayAddress {
		address, ok := addresses[record.ShouldPayAddress[i].Address.ID]
		if !ok {
			return fmt.Errorf("should-pay address %s does not belong to trip", record.ShouldPayAddress[i].Address.ID)
		}
		if _, duplicate := seen[address.ID]; duplicate {
			return fmt.Errorf("duplicate should-pay address %s", address.ID)
		}
		seen[address.ID] = struct{}{}
		record.ShouldPayAddress[i].Address = address
	}
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
func (db *inMemoryTripDBWrapper) DataLoaderGetRecordInfoList(_ context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.RecordInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID][]domain.RecordInfo)
	errors := make(map[uuid.UUID]error)

	for _, tripID := range tripIds {
		if tripData, exists := db.tripsData[tripID]; exists {
			recordInfos := make([]domain.RecordInfo, len(tripData.Records))
			for i, r := range tripData.Records {
				recordInfos[i] = r.RecordInfo
			}
			result[tripID] = recordInfos
			errors[tripID] = nil // No error for this trip ID
		} else {
			// If a trip ID is not found, you might choose to return an empty slice or an error.
			// For a data loader, typically an empty slice is returned if no data exists for the key.
			result[tripID] = []domain.RecordInfo{}
			errors[tripID] = fmt.Errorf("trip with ID %s not found", tripID)
		}
	}
	return result, dataloadgen.MappedFetchError[uuid.UUID](errors)
}

// DataLoaderGetTripAddressList retrieves a map of Address lists for given trip IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetTripAddressList(_ context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID][]domain.Address)
	errors := make(map[uuid.UUID]error)

	for _, tripID := range tripIds {
		if tripData, exists := db.tripsData[tripID]; exists {
			// Return a copy of the slice to prevent external modification
			addressListCopy := make([]domain.Address, len(tripData.AddressList))
			copy(addressListCopy, tripData.AddressList)
			result[tripID] = addressListCopy
			errors[tripID] = nil // No error for this trip ID
		} else {
			result[tripID] = []domain.Address{}
			errors[tripID] = fmt.Errorf("trip with ID %s not found", tripID)
		}
	}
	return result, dataloadgen.MappedFetchError[uuid.UUID](errors)
}

// DataLoaderGetRecordShouldPayList retrieves a map of ShouldPayAddress lists for given record IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetRecordShouldPayList(_ context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]domain.ExtendAddress, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID][]domain.ExtendAddress)
	errors := make(map[uuid.UUID]error)

	for _, recordID := range recordIds {
		found := false
		for _, tripData := range db.tripsData {
			for _, record := range tripData.Records {
				if record.ID == recordID {
					// Return a copy of the ShouldPayAddress list
					addressListCopy := make([]domain.ExtendAddress, len(record.ShouldPayAddress))
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
			result[recordID] = []domain.ExtendAddress{}
			errors[recordID] = fmt.Errorf("record with ID %s not found", recordID)
		}
	}
	return result, dataloadgen.MappedFetchError[uuid.UUID](errors)
}

// DataLoaderGetTripInfoList retrieves a map of TripInfo pointers for given trip IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetTripInfoList(_ context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID]*domain.TripInfo)
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
