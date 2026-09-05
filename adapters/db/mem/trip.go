package mem

import (
	"context"
	"fmt"
	"sync"

	dbpkg "dtm/adapters/db/db"
	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
)

// inMemoryTripDBWrapper is an in-memory implementation of dbpkg.TripDBWrapper.
// It uses maps to store data for quick lookups.
type inMemoryTripDBWrapper struct {
	// Using maps to store domain.TripInfo and TripData by Trip ID.
	tripsInfo map[uuid.UUID]*domain.TripInfo
	tripsData map[uuid.UUID]*domain.TripData // Stores records and address lists for each trip

	// Mutex for thread-safety, important for concurrent access in a real application.
	mu sync.RWMutex
}

// NewInMemoryTripDBWrapper creates and returns a new instance of inMemoryTripDBWrapper.
func NewInMemoryTripDBWrapper() dbpkg.TripDBWrapper {
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

func (db *inMemoryTripDBWrapper) AppendNew(ctx context.Context, tripID uuid.UUID, record domain.Record, policy dbpkg.RecordMaterializer) (domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return domain.Record{}, err
	}
	if policy == nil {
		return domain.Record{}, dbpkg.ErrMaterializerRequired
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	tripData, ok := db.tripsData[tripID]
	if !ok {
		return domain.Record{}, fmt.Errorf("%w: %s", dbpkg.ErrTripNotFound, tripID)
	}
	for _, existing := range tripData.Records {
		if existing.ID == record.ID {
			return domain.Record{}, fmt.Errorf("record %s already exists", record.ID)
		}
	}
	record, err := policy.PrepareNew(record, tripData.AddressList)
	if err != nil {
		return domain.Record{}, err
	}
	tripData.Records = append(tripData.Records, record)
	return cloneRecord(record), nil
}

func (db *inMemoryTripDBWrapper) AppendPatch(ctx context.Context, targetID uuid.UUID, patch domain.RecordPatch, policy dbpkg.RecordMaterializer) (uuid.UUID, domain.Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, domain.Record{}, false, err
	}
	if policy == nil {
		return uuid.Nil, domain.Record{}, false, dbpkg.ErrMaterializerRequired
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	var tripID uuid.UUID
	var tripData *domain.TripData
	for id, candidate := range db.tripsData {
		for _, record := range candidate.Records {
			if record.ID == targetID {
				tripID, tripData = id, candidate
				break
			}
		}
		if tripData != nil {
			break
		}
	}
	if tripData == nil {
		return uuid.Nil, domain.Record{}, false, fmt.Errorf("%w: %s: %w", dbpkg.ErrRecordNotFound, targetID, chainlist.ErrNodeNotFound)
	}
	nodes := make([]chainlist.Node[uuid.UUID, domain.RecordInfo], len(tripData.Records))
	byID := make(map[uuid.UUID]int, len(tripData.Records))
	for i, record := range tripData.Records {
		nodes[i] = chainlist.Node[uuid.UUID, domain.RecordInfo]{ID: record.ID, ParentID: record.ParentRecordID, ChildID: record.ChildRecordID, Value: record.RecordInfo}
		byID[record.ID] = i
	}
	source, err := chainlist.NewMemorySource(nodes)
	if err != nil {
		return uuid.Nil, domain.Record{}, false, fmt.Errorf("%w: %w", dbpkg.ErrInvalidChain, err)
	}
	var tail domain.Record
	for node, walkErr := range source.WalkCanonical(ctx, targetID) {
		if walkErr != nil {
			return uuid.Nil, domain.Record{}, false, fmt.Errorf("%w: %w", dbpkg.ErrInvalidChain, walkErr)
		}
		tail = cloneRecord(tripData.Records[byID[node.ID]])
	}
	materialized, changed, err := policy.ApplyPatch(tail, patch, tripData.AddressList)
	if err != nil {
		return uuid.Nil, domain.Record{}, false, err
	}
	if !changed {
		return tripID, cloneRecord(tail), false, nil
	}
	parentID := tail.ID
	materialized.ID = uuid.New()
	materialized.ParentRecordID = &parentID
	materialized.ChildRecordID = nil
	childID := materialized.ID
	tailIndex := byID[tail.ID]
	if tripData.Records[tailIndex].ChildRecordID != nil {
		return uuid.Nil, domain.Record{}, false, fmt.Errorf("record chain invariant violated: resolved tail %s has a child", tail.ID)
	}
	tripData.Records[tailIndex].ChildRecordID = &childID
	tripData.Records = append(tripData.Records, cloneRecord(materialized))
	return tripID, cloneRecord(materialized), true, nil
}

func cloneRecord(record domain.Record) domain.Record {
	if record.ParentRecordID != nil {
		parent := *record.ParentRecordID
		record.ParentRecordID = &parent
	}
	if record.ChildRecordID != nil {
		child := *record.ChildRecordID
		record.ChildRecordID = &child
	}
	record.ShouldPayAddress = append([]domain.ExtendAddress(nil), record.ShouldPayAddress...)
	return record
}

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
				recordInfos[i] = cloneRecord(r).RecordInfo
			}
			result[tripID] = recordInfos
		} else {
			// If a trip ID is not found, you might choose to return an empty slice or an error.
			// For a data loader, typically an empty slice is returned if no data exists for the key.
			result[tripID] = []domain.RecordInfo{}
			errors[tripID] = fmt.Errorf("%w: %s", dbpkg.ErrTripNotFound, tripID)
		}
	}
	return result, mappedFetchError(errors)
}

func (db *inMemoryTripDBWrapper) DataLoaderGetRecordList(_ context.Context, recordIds []uuid.UUID) (map[uuid.UUID]dbpkg.RecordNode, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	result := make(map[uuid.UUID]dbpkg.RecordNode, len(recordIds))
	errorsByID := make(map[uuid.UUID]error)
	for _, id := range recordIds {
		found := false
		for tripID, tripData := range db.tripsData {
			for _, record := range tripData.Records {
				if record.ID == id {
					result[id] = dbpkg.RecordNode{TripID: tripID, Info: cloneRecord(record).RecordInfo}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			errorsByID[id] = fmt.Errorf("%w: %w: %s", dbpkg.ErrRecordNotFound, chainlist.ErrNodeNotFound, id)
		}
	}
	return result, mappedFetchError(errorsByID)
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
		} else {
			result[tripID] = []domain.Address{}
			errors[tripID] = fmt.Errorf("%w: %s", dbpkg.ErrTripNotFound, tripID)
		}
	}
	return result, mappedFetchError(errors)
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
			errors[recordID] = fmt.Errorf("%w: %s", dbpkg.ErrRecordNotFound, recordID)
		}
	}
	return result, mappedFetchError(errors)
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
		} else {
			// If a trip ID is not found, typically nil is returned for that specific key.
			result[tripID] = nil
			errors[tripID] = fmt.Errorf("%w: %s", dbpkg.ErrTripNotFound, tripID)
		}
	}

	return result, mappedFetchError(errors)
}

func mappedFetchError(errorsByID map[uuid.UUID]error) error {
	if len(errorsByID) == 0 {
		return nil
	}
	return dataloadgen.MappedFetchError[uuid.UUID](errorsByID)
}
