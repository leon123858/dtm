package pg

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	dbt "dtm/db/db"
)

// GORMTripDBWrapper is a GORM-based PostgreSQL implementation of dbt.TripDBWrapper.
type GORMTripDBWrapper struct {
	db *gorm.DB
}

// NewGORMTripDBWrapper creates and returns a new instance of GORMTripDBWrapper.
func NewGORMTripDBWrapper(db *gorm.DB) dbt.TripDBWrapper {
	return &GORMTripDBWrapper{
		db: db,
	}
}

// CreateTrip creates a new trip entry using GORM.
func (pgdb *GORMTripDBWrapper) CreateTrip(info *dbt.TripInfo) error {
	tripModel := TripInfoModel{
		ID:   info.ID,
		Name: info.Name,
	}
	result := pgdb.db.Create(&tripModel)
	if result.Error != nil {
		if strings.Contains(result.Error.Error(), "duplicate key value violates unique constraint") {
			return fmt.Errorf("trip with ID %s already exists: %w", info.ID, result.Error)
		}
		return fmt.Errorf("failed to create trip: %w", result.Error)
	}
	return nil
}

// CreateTripRecords adds a slice of records to an existing trip using GORM.
func (pgdb *GORMTripDBWrapper) CreateTripRecords(tripID uuid.UUID, records []dbt.Record) error {
	if len(records) == 0 {
		return nil
	}

	var recordModels []RecordModel
	for _, record := range records {
		var shouldPayAddresses []string

		for _, addr := range record.ShouldPayAddress {
			shouldPayAddresses = append(shouldPayAddresses, string(addr))
		}
		recordModels = append(recordModels, RecordModel{
			ID:               record.ID,
			TripID:           tripID,
			Name:             record.Name,
			Amount:           record.Amount,
			PrePayAddress:    string(record.PrePayAddress),
			ShouldPayAddress: shouldPayAddresses,
		})

	}

	// GORM Create In Batches
	result := pgdb.db.Create(&recordModels)
	if result.Error != nil {
		// Check if tripID exists (foreign key constraint violation)
		if strings.Contains(result.Error.Error(), "violates foreign key constraint") {
			return fmt.Errorf("trip with ID %s not found for creating records: %w", tripID, result.Error)
		}
		return fmt.Errorf("failed to create trip records for trip %s: %w", tripID, result.Error)
	}
	return nil
}

// GetTripInfo retrieves trip information by ID using GORM.
func (pgdb *GORMTripDBWrapper) GetTripInfo(id uuid.UUID) (*dbt.TripInfo, error) {
	var tripInfoModel TripInfoModel
	result := pgdb.db.First(&tripInfoModel, "id = ?", id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("trip info with ID %s not found", id)
		}
		return nil, fmt.Errorf("failed to get trip info for ID %s: %w", id, result.Error)
	}
	return &dbt.TripInfo{
		ID:   tripInfoModel.ID,
		Name: tripInfoModel.Name,
	}, nil
}

// GetTripRecords retrieves all records for a given trip ID using GORM.
func (pgdb *GORMTripDBWrapper) GetTripRecords(id uuid.UUID) ([]dbt.Record, error) {
	var recordModels []RecordModel
	result := pgdb.db.Where("trip_id = ?", id).Find(&recordModels)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get trip records for trip ID %s: %w", id, result.Error)
	}

	var records []dbt.Record
	for _, rm := range recordModels {
		var shouldPayAddresses []dbt.Address
		for _, addr := range rm.ShouldPayAddress {
			shouldPayAddresses = append(shouldPayAddresses, dbt.Address(addr))
		}
		records = append(records, dbt.Record{
			ID:               rm.ID,
			Name:             rm.Name,
			Amount:           rm.Amount,
			PrePayAddress:    dbt.Address(rm.PrePayAddress),
			ShouldPayAddress: shouldPayAddresses,
		})
	}
	return records, nil
}

// GetTripAddressList retrieves the address list for a given trip ID using GORM.
func (pgdb *GORMTripDBWrapper) GetTripAddressList(id uuid.UUID) ([]dbt.Address, error) {
	var addressListModels []TripAddressListModel
	result := pgdb.db.Where("trip_id = ?", id).Find(&addressListModels)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get trip address list for trip ID %s: %w", id, result.Error)
	}

	var addresses []dbt.Address
	for _, alm := range addressListModels {
		addresses = append(addresses, dbt.Address(alm.Address))
	}
	return addresses, nil
}

// GetRecordAddressList retrieves the ShouldPayAddress list for a given record ID using GORM.
func (pgdb *GORMTripDBWrapper) GetRecordAddressList(recordID uuid.UUID) ([]dbt.Address, error) {
	var recordModel RecordModel
	result := pgdb.db.Select("should_pay_address").First(&recordModel, "id = ?", recordID)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("record with ID %s not found", recordID)
		}
		return nil, fmt.Errorf("failed to get record address list for record ID %s: %w", recordID, result.Error)
	}

	var addresses []dbt.Address
	for _, addr := range recordModel.ShouldPayAddress {
		addresses = append(addresses, dbt.Address(addr))
	}
	return addresses, nil
}

// UpdateTripInfo updates the information of an existing trip using GORM.
func (pgdb *GORMTripDBWrapper) UpdateTripInfo(info *dbt.TripInfo) error {
	result := pgdb.db.Model(&TripInfoModel{}).Where("id = ?", info.ID).Update("name", info.Name)
	if result.Error != nil {
		return fmt.Errorf("failed to update trip info for ID %s: %w", info.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("trip with ID %s not found for update", info.ID)
	}
	return nil
}

// UpdateTripRecord updates a specific record within a trip using GORM.
func (pgdb *GORMTripDBWrapper) UpdateTripRecord(record dbt.Record) error {
	var shouldPayAddresses []string
	for _, addr := range record.ShouldPayAddress {
		shouldPayAddresses = append(shouldPayAddresses, string(addr))
	}

	recordModel := RecordModel{
		ID:               record.ID,
		Name:             record.Name,
		Amount:           record.Amount,
		PrePayAddress:    string(record.PrePayAddress),
		ShouldPayAddress: shouldPayAddresses,
	}

	// 使用 Select 選擇要更新的欄位，避免更新 CreatedAt 等 GORM 自動欄位
	result := pgdb.db.Model(&recordModel).Select("name", "amount", "pre_pay_address", "should_pay_address").Where("id = ?", record.ID).Updates(recordModel)
	if result.Error != nil {
		return fmt.Errorf("failed to update record with ID %s: %w", record.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("record with ID %s not found for update", record.ID)
	}
	return nil
}

// TripAddressListAdd adds an address to a trip's address list using GORM.
func (pgdb *GORMTripDBWrapper) TripAddressListAdd(id uuid.UUID, address dbt.Address) error {
	addressModel := TripAddressListModel{
		TripID:  id,
		Address: string(address),
	}
	result := pgdb.db.Create(&addressModel)
	if result.Error != nil {
		if strings.Contains(result.Error.Error(), "duplicate key value violates unique constraint") {
			return fmt.Errorf("address %s already exists in trip %s", address, id)
		}
		// Check if the trip_id exists (foreign key constraint violation)
		if strings.Contains(result.Error.Error(), "violates foreign key constraint") {
			return fmt.Errorf("trip with ID %s not found: %w", id, result.Error)
		}
		return fmt.Errorf("failed to add address %s to trip %s: %w", address, id, result.Error)
	}
	return nil
}

// TripAddressListRemove removes an address from a trip's address list using GORM.
func (pgdb *GORMTripDBWrapper) TripAddressListRemove(id uuid.UUID, address dbt.Address) error {
	result := pgdb.db.Where("trip_id = ? AND address = ?", id, string(address)).Delete(&TripAddressListModel{})
	if result.Error != nil {
		return fmt.Errorf("failed to remove address %s from trip %s: %w", address, id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("address %s not found in trip %s", address, id)
	}
	return nil
}

// DeleteTrip deletes a trip and all its associated data using GORM.
// GORM will respect ON DELETE CASCADE if configured in the database,
// otherwise, you might need to handle deletions of associated records/address lists manually or via GORM hooks.
func (pgdb *GORMTripDBWrapper) DeleteTrip(id uuid.UUID) error {
	result := pgdb.db.Delete(&TripInfoModel{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete trip with ID %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("trip with ID %s not found for deletion", id)
	}
	return nil
}

// DeleteTripRecord deletes a specific record using GORM.
func (pgdb *GORMTripDBWrapper) DeleteTripRecord(recordID uuid.UUID) error {
	result := pgdb.db.Delete(&RecordModel{}, "id = ?", recordID)
	if result.Error != nil {
		return fmt.Errorf("failed to delete record with ID %s: %w", recordID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("record with ID %s not found for deletion", recordID)
	}
	return nil
}

// DataLoaderGetRecordList retrieves multiple records for a given set of record IDs using GORM.
// This method is designed to be used with a DataLoader for batching queries.
func (pgdb *GORMTripDBWrapper) DataLoaderGetRecordList(ctx context.Context, keys []uuid.UUID) (map[uuid.UUID]dbt.Record, map[uuid.UUID]error) {
	// Initialize slices for results and errors
	records := make(map[uuid.UUID]dbt.Record, len(keys))
	errors := make(map[uuid.UUID]error, len(keys))

	tmpResult := []RecordModel{}

	result := pgdb.db.WithContext(ctx).Where("id IN ?", keys).Find(&tmpResult)

	if result.Error != nil {
		// If there's a global error during the query, set this error for all keys
		for _, uid := range keys {
			errors[uid] = fmt.Errorf("failed to retrieve records: %w", result.Error)
		}
		return records, errors
	}

	// Populate the map for quick lookup
	for _, record := range tmpResult {
		if record.ID != uuid.Nil {
			records[record.ID] = dbt.Record{
				ID:               record.ID,
				Name:             record.Name,
				Amount:           record.Amount,
				PrePayAddress:    dbt.Address(record.PrePayAddress),
				ShouldPayAddress: make([]dbt.Address, len(record.ShouldPayAddress)),
			}
			for i, addr := range record.ShouldPayAddress {
				records[record.ID].ShouldPayAddress[i] = dbt.Address(addr)
			}
		}
	}

	// handle error
	for _, key := range keys {
		if _, ok := records[key]; !ok {
			errors[key] = fmt.Errorf("key %s not found", key)
		}
	}

	return records, errors
}
