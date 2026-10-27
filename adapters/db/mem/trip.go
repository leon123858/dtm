package mem

import (
	"context"
	"fmt"

	dbpkg "dtm/adapters/db/db"
	"dtm/adapters/db/utils"
	"dtm/domain"
	"dtm/libs/chainlist"
	"dtm/libs/textvalidate"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
)

// inMemoryTripDBWrapper is an in-memory implementation of dbpkg.TripDBWrapper.
// It uses maps to store data for quick lookups.
type inMemoryTripDBWrapper struct{ *memoryState }

func NewInMemoryTripDBWrapper() dbpkg.TripDBWrapper {
	return &inMemoryTripDBWrapper{newMemoryState()}
}

// --- Create Operations ---

// CreateTrip creates a new trip entry in memory.
func (db *inMemoryTripDBWrapper) CreateTrip(info *domain.TripInfo) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tripsInfo[info.ID]; exists {
		return fmt.Errorf("trip with ID %s already exists", info.ID)
	}

	return db.createTripLocked(info)
}

func (db *memoryState) createTripLocked(info *domain.TripInfo) error {
	if _, exists := db.tripsInfo[info.ID]; exists {
		return fmt.Errorf("trip already exists")
	}
	// Store a copy to prevent external modification of the original info pointer
	infoCopy := *info
	infoCopy.Private = false
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
	if err := db.accessLocked(ctx, tripID); err != nil {
		return domain.Record{}, err
	}
	tripData, ok := db.tripsData[tripID]
	if !ok {
		return domain.Record{}, fmt.Errorf("%w: %s", domain.ErrTripNotFound, tripID)
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
	if err := canonicalizeRecordAddresses(tripData, &record); err != nil {
		return domain.Record{}, err
	}
	tripData.Records = append(tripData.Records, record)
	return db.displayRecordLocked(tripID, record), nil
}

func (db *inMemoryTripDBWrapper) AppendPatch(ctx context.Context, tripID, targetID uuid.UUID, patch domain.RecordPatch, policy dbpkg.RecordMaterializer) (uuid.UUID, domain.Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, domain.Record{}, false, err
	}
	if policy == nil {
		return uuid.Nil, domain.Record{}, false, dbpkg.ErrMaterializerRequired
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := db.accessLocked(ctx, tripID); err != nil {
		return uuid.Nil, domain.Record{}, false, err
	}
	var tripData *domain.TripData
	if candidate := db.tripsData[tripID]; candidate != nil {
		for _, record := range candidate.Records {
			if record.ID == targetID {
				tripData = candidate
				break
			}
		}
	}
	if tripData == nil {
		return uuid.Nil, domain.Record{}, false, fmt.Errorf("%w: %s: %w", domain.ErrRecordNotFound, targetID, chainlist.ErrNodeNotFound)
	}
	nodes := make([]chainlist.Node[uuid.UUID, domain.RecordInfo], len(tripData.Records))
	byID := make(map[uuid.UUID]int, len(tripData.Records))
	for i, record := range tripData.Records {
		nodes[i] = chainlist.Node[uuid.UUID, domain.RecordInfo]{ID: record.ID, ParentID: record.ParentRecordID, ChildID: record.ChildRecordID, Value: record.RecordInfo}
		byID[record.ID] = i
	}
	source, err := chainlist.NewMemorySource(nodes)
	if err != nil {
		return uuid.Nil, domain.Record{}, false, fmt.Errorf("%w: %w", domain.ErrInvalidChain, err)
	}
	var tail domain.Record
	for node, walkErr := range source.WalkCanonical(ctx, targetID, true) {
		if walkErr != nil {
			return uuid.Nil, domain.Record{}, false, fmt.Errorf("%w: %w", domain.ErrInvalidChain, walkErr)
		}
		tail = cloneRecord(tripData.Records[byID[node.ID]])
	}
	materialized, changed, err := policy.ApplyPatch(tail, patch, tripData.AddressList)
	if err != nil {
		return uuid.Nil, domain.Record{}, false, err
	}
	if !changed {
		return tripID, db.displayRecordLocked(tripID, tail), false, nil
	}
	if err := canonicalizeRecordAddresses(tripData, &materialized); err != nil {
		return uuid.Nil, domain.Record{}, false, err
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
	return tripID, db.displayRecordLocked(tripID, materialized), true, nil
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
func (db *inMemoryTripDBWrapper) UpdateTripInfo(ctx context.Context, info *domain.TripInfo) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := db.accessLocked(ctx, info.ID); err != nil {
		return err
	}

	if _, exists := db.tripsInfo[info.ID]; !exists {
		return fmt.Errorf("trip with ID %s not found for update", info.ID)
	}

	info.Private = db.tripsInfo[info.ID].Private
	// Update the existing info
	infoCopy := *info
	db.tripsInfo[info.ID] = &infoCopy
	return nil
}

func (db *inMemoryTripDBWrapper) CreateAddress(ctx context.Context, id uuid.UUID, name string) (*domain.Address, error) {
	returnValue, _, err := db.CreateAddressWithOptions(ctx, id, &name, nil, false)
	return returnValue, err
}

func (db *inMemoryTripDBWrapper) UpdateAddress(ctx context.Context, id, addressID uuid.UUID, name string) (*domain.Address, error) {
	returnValue, _, err := db.PatchAddress(ctx, id, addressID, &name, nil)
	return returnValue, err
}

func (db *inMemoryTripDBWrapper) DeleteAddress(ctx context.Context, id, addressID uuid.UUID) (*domain.Address, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := db.accessLocked(ctx, id); err != nil {
		return nil, err
	}

	for _, aid := range db.bindings[id] {
		if aid != nil && *aid == addressID {
			return nil, domain.ErrConflict
		}
	}

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
	return nil, fmt.Errorf("%w: %s", domain.ErrAddressNotFound, addressID)
}

// --- Delete Operations ---

// DeleteTrip deletes a trip and all its associated data (info, records, address list).
func (db *inMemoryTripDBWrapper) DeleteTrip(ctx context.Context, id uuid.UUID) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if err := db.accessLocked(ctx, id); err != nil {
		return err
	}

	// check if the trip exists
	if _, exists := db.tripsInfo[id]; !exists {
		return fmt.Errorf("trip with ID %s not found for deletion", id)
	}
	// Delete the trip info and data
	if _, exists := db.tripsData[id]; !exists {
		return fmt.Errorf("trip data with ID %s not found for deletion", id)
	}

	delete(db.bindings, id)
	delete(db.tripsInfo, id)
	for _, trips := range db.subscriptions {
		delete(trips, id)
	}
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

func (db *inMemoryTripDBWrapper) DataLoaderGetTripRecords(ctx context.Context, tripIds []uuid.UUID, options dbpkg.RecordReadOptions) (map[uuid.UUID][]dbpkg.RecordSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	result := make(map[uuid.UUID][]dbpkg.RecordSnapshot, len(tripIds))
	errors := make(map[uuid.UUID]error)
	for _, id := range tripIds {
		if err := db.accessLocked(ctx, id); err != nil {
			errors[id] = err
			continue
		}
		result[id] = []dbpkg.RecordSnapshot{}
		data, exists := db.tripsData[id]
		if !exists {
			errors[id] = fmt.Errorf("%w: %s", domain.ErrTripNotFound, id)
			continue
		}
		for _, record := range data.Records {
			if !options.HaveHistory && (record.ChildRecordID != nil || record.IsDeleted) {
				continue
			}
			result[id] = append(result[id], dbpkg.RecordSnapshot{TripID: id, Record: db.displayRecordLocked(id, record)})
		}
	}
	return result, mappedFetchError(errors)
}

func (db *inMemoryTripDBWrapper) DataLoaderGetRecordList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID]dbpkg.RecordSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	result := make(map[uuid.UUID]dbpkg.RecordSnapshot, len(recordIds))
	errorsByID := make(map[uuid.UUID]error)
	for _, id := range recordIds {
		found := false
		for tripID, tripData := range db.tripsData {
			for _, record := range tripData.Records {
				if record.ID == id {
					if err := db.accessLocked(ctx, tripID); err != nil {
						errorsByID[id] = err
					} else {
						result[id] = dbpkg.RecordSnapshot{TripID: tripID, Record: db.displayRecordLocked(tripID, record)}
					}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			errorsByID[id] = fmt.Errorf("%w: %w: %s", domain.ErrRecordNotFound, chainlist.ErrNodeNotFound, id)
		}
	}
	return result, mappedFetchError(errorsByID)
}

// DataLoaderGetTripAddressList retrieves a map of Address lists for given trip IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID][]domain.Address)
	errors := make(map[uuid.UUID]error)

	for _, tripID := range tripIds {
		if err := db.accessLocked(ctx, tripID); err != nil {
			errors[tripID] = err
			continue
		}
		if tripData, exists := db.tripsData[tripID]; exists {
			// Return a copy of the slice to prevent external modification
			addressListCopy := make([]domain.Address, len(tripData.AddressList))
			for i, a := range tripData.AddressList {
				addressListCopy[i] = db.displayLocked(tripID, a)
			}
			result[tripID] = addressListCopy
		} else {
			result[tripID] = []domain.Address{}
			errors[tripID] = fmt.Errorf("%w: %s", domain.ErrTripNotFound, tripID)
		}
	}
	return result, mappedFetchError(errors)
}

// DataLoaderGetTripInfoList retrieves a map of TripInfo pointers for given trip IDs.
func (db *inMemoryTripDBWrapper) DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	result := make(map[uuid.UUID]*domain.TripInfo)
	errors := make(map[uuid.UUID]error)

	for _, tripID := range tripIds {
		if err := db.accessLocked(ctx, tripID); err != nil {
			errors[tripID] = err
			continue
		}
		if tripInfo, exists := db.tripsInfo[tripID]; exists {
			// Return a copy to prevent external modification
			infoCopy := *tripInfo
			result[tripID] = &infoCopy
		} else {
			// If a trip ID is not found, typically nil is returned for that specific key.
			result[tripID] = nil
			errors[tripID] = fmt.Errorf("%w: %s", domain.ErrTripNotFound, tripID)
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

func (s *memoryState) displayLocked(tid uuid.UUID, a domain.Address) domain.Address {
	for uid, aid := range s.bindings[tid] {
		if aid != nil && *aid == a.ID {
			return a.Display(s.users[uid].Name)
		}
	}
	return a
}
func (s *memoryState) displayRecordLocked(tid uuid.UUID, r domain.Record) domain.Record {
	r = cloneRecord(r)
	for _, a := range s.tripsData[tid].AddressList {
		if r.PrePayAddress.ID == a.ID {
			r.PrePayAddress = s.displayLocked(tid, a)
		}
		for i := range r.ShouldPayAddress {
			if r.ShouldPayAddress[i].Address.ID == a.ID {
				r.ShouldPayAddress[i].Address = s.displayLocked(tid, a)
			}
		}
	}
	return r
}
func (s *inMemoryTripDBWrapper) CreateAddressWithOptions(ctx context.Context, tid uuid.UUID, name *string, useNickname *bool, bind bool) (*domain.Address, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var uid uuid.UUID
	var user domain.User
	if bind {
		var err error
		uid, err = s.memberUser(ctx)
		if err != nil {
			return nil, false, err
		}
		user = s.users[uid]
	}
	if err := s.accessLocked(ctx, tid); err != nil {
		return nil, false, err
	}
	data := s.tripsData[tid]
	if bind {
		if aid := s.bindings[tid][uid]; aid != nil {
			for _, a := range data.AddressList {
				if a.ID == *aid {
					a = s.displayLocked(tid, a)
					return &a, false, nil
				}
			}
		}
	}
	exists := func(n string) bool {
		for _, a := range data.AddressList {
			if a.Name == n {
				return true
			}
		}
		return false
	}
	n, err := utils.AddressName(name, user.Name, bind, exists)
	if err != nil {
		return nil, false, err
	}
	if exists(n) {
		return nil, false, domain.ErrConflict
	}
	a := domain.Address{ID: uuid.New(), Name: n}
	if useNickname != nil {
		a.UseNickname = *useNickname
	}
	data.AddressList = append(data.AddressList, a)
	if bind {
		if s.bindings[tid] == nil {
			s.bindings[tid] = map[uuid.UUID]*uuid.UUID{}
		}
		id := a.ID
		s.bindings[tid][uid] = &id
	}
	a = s.displayLocked(tid, a)
	return &a, true, nil
}
func (s *inMemoryTripDBWrapper) PatchAddress(ctx context.Context, tid, aid uuid.UUID, name *string, useNickname *bool) (*domain.Address, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.accessLocked(ctx, tid); err != nil {
		return nil, false, err
	}
	if name != nil {
		if err := textvalidate.ValidateName(*name); err != nil {
			return nil, false, fmt.Errorf("%w: %v", domain.ErrBadUserInput, err)
		}
	}
	data := s.tripsData[tid]
	for _, a := range data.AddressList {
		if name != nil && a.ID != aid && a.Name == *name {
			return nil, false, domain.ErrConflict
		}
	}
	for i, a := range data.AddressList {
		if a.ID == aid {
			old := a
			if name != nil {
				a.Name = *name
			}
			if useNickname != nil {
				a.UseNickname = *useNickname
			}
			data.AddressList[i] = a
			changed := old != a
			a = s.displayLocked(tid, a)
			return &a, changed, nil
		}
	}
	return nil, false, domain.ErrAddressNotFound
}
