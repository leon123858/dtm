package pg

import (
	"context"
	"dtm/db/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// pgDBWrapper is an implementation of TripDBWrapper using GORM.
type pgDBWrapper struct {
	db *gorm.DB
}

// NewPgDBWrapper creates a new instance of pgDBWrapper.
func NewPgDBWrapper(db *gorm.DB) db.TripDBWrapper { // Assuming db.TripDBWrapper is the interface type
	return &pgDBWrapper{db: db}
}

// Create
func (p *pgDBWrapper) CreateTrip(info *db.TripInfo) error { // Assuming db.TripInfo is the type from db/types.go
	tripModel := TripInfoModel{
		ID:   info.ID,
		Name: info.Name,
	}
	return p.db.Create(&tripModel).Error
}

func (p *pgDBWrapper) CreateTripRecords(id uuid.UUID, records []db.Record) error { // Assuming db.Record
	// This can be done in a transaction for atomicity
	return p.db.Transaction(func(tx *gorm.DB) error {
		for _, rec := range records {
			recordModel := RecordModel{
				ID:            rec.RecordInfo.ID,
				TripID:        id, // Link to the trip
				Name:          rec.RecordInfo.Name,
				Amount:        rec.RecordInfo.Amount,
				PrePayAddress: string(rec.RecordInfo.PrePayAddress),
			}
			if err := tx.Create(&recordModel).Error; err != nil {
				return err
			}

			// Create entries in RecordShouldPayAddressListModel
			for _, addr := range rec.RecordData.ShouldPayAddress {
				shouldPayModel := RecordShouldPayAddressListModel{
					RecordID: rec.RecordInfo.ID,
					TripID:   id, // Link to the trip
					Address:  string(addr),
				}
				if err := tx.Create(&shouldPayModel).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Read
func (p *pgDBWrapper) GetTripInfo(id uuid.UUID) (*db.TripInfo, error) {
	var tripModel TripInfoModel
	if err := p.db.First(&tripModel, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &db.TripInfo{
		ID:   tripModel.ID,
		Name: tripModel.Name,
	}, nil
}

func (p *pgDBWrapper) GetTripRecords(id uuid.UUID) ([]db.RecordInfo, error) {
	var recordModels []RecordModel
	if err := p.db.Where("trip_id = ?", id).Find(&recordModels).Error; err != nil {
		return nil, err
	}

	var recordInfos []db.RecordInfo
	for _, rm := range recordModels {
		recordInfos = append(recordInfos, db.RecordInfo{
			ID:            rm.ID,
			Name:          rm.Name,
			Amount:        rm.Amount,
			PrePayAddress: db.Address(rm.PrePayAddress),
		})
	}
	return recordInfos, nil
}

func (p *pgDBWrapper) GetTripAddressList(id uuid.UUID) ([]db.Address, error) {
	var addressModels []TripAddressListModel
	if err := p.db.Where("trip_id = ?", id).Find(&addressModels).Error; err != nil {
		return nil, err
	}

	var addresses []db.Address
	for _, am := range addressModels {
		addresses = append(addresses, db.Address(am.Address))
	}
	return addresses, nil
}

func (p *pgDBWrapper) GetRecordAddressList(recordID uuid.UUID) ([]db.Address, error) {
	var shouldPayModels []RecordShouldPayAddressListModel
	if err := p.db.Where("record_id = ?", recordID).Find(&shouldPayModels).Error; err != nil {
		return nil, err
	}

	var addresses []db.Address
	for _, spm := range shouldPayModels {
		addresses = append(addresses, db.Address(spm.Address))
	}
	return addresses, nil
}

// Update
func (p *pgDBWrapper) UpdateTripInfo(info *db.TripInfo) error {
	tripModel := TripInfoModel{
		ID:   info.ID,
		Name: info.Name,
	}
	return p.db.Model(&TripInfoModel{}).Where("id = ?", info.ID).Updates(tripModel).Error
}

func (p *pgDBWrapper) UpdateTripRecord(record *db.Record) (uuid.UUID, error) {
	// use transaction to update info and data
	tripId := uuid.Nil
	ret := p.db.Transaction(func(tx *gorm.DB) error {
		// read recordModel.TripId and update recordModel
		var recordModel RecordModel
		if err := tx.First(&recordModel, "id = ?", record.RecordInfo.ID).Error; err != nil {
			return err
		}

		recordModel.Name = record.RecordInfo.Name
		recordModel.Amount = record.RecordInfo.Amount
		recordModel.PrePayAddress = string(record.RecordInfo.PrePayAddress)
		if err := tx.Model(&RecordModel{}).Where("id = ?", record.RecordInfo.ID).Updates(recordModel).Error; err != nil {
			return err
		}

		// Update RecordShouldPayAddressListModel
		if err := tx.Where("record_id = ?", record.RecordInfo.ID).Delete(&RecordShouldPayAddressListModel{}).Error; err != nil {
			return err
		}

		// insert batch
		models := make([]RecordShouldPayAddressListModel, 0, len(record.RecordData.ShouldPayAddress))
		for _, addr := range record.RecordData.ShouldPayAddress {
			shouldPayModel := RecordShouldPayAddressListModel{
				RecordID: record.RecordInfo.ID,
				TripID:   recordModel.TripID, // Link to the trip
				Address:  string(addr),
			}
			models = append(models, shouldPayModel)
		}
		if err := tx.Create(&models).Error; err != nil {
			return err
		}
		tripId = recordModel.TripID // Store the trip ID for return
		// If everything is successful, return nil to commit the transaction
		return nil
	})
	if ret != nil {
		return uuid.Nil, ret
	}
	return tripId, nil
}

func (p *pgDBWrapper) TripAddressListAdd(id uuid.UUID, address db.Address) error {
	addressModel := TripAddressListModel{
		TripID:  id,
		Address: string(address),
	}
	// Using FirstOrCreate to avoid duplicate entries if the address already exists for the trip.
	return p.db.FirstOrCreate(&addressModel, TripAddressListModel{TripID: id, Address: string(address)}).Error
}

func (p *pgDBWrapper) TripAddressListRemove(id uuid.UUID, address db.Address) error {
	return p.db.Where("trip_id = ? AND address = ?", id, string(address)).Delete(&TripAddressListModel{}).Error
}

// Delete
func (p *pgDBWrapper) DeleteTrip(id uuid.UUID) error {
	// GORM's CASCADE constraint should handle deleting associated records, trip_address_lists,
	// and record_should_pay_address_lists.
	return p.db.Delete(&TripInfoModel{}, "id = ?", id).Error
}

func (p *pgDBWrapper) DeleteTripRecord(recordID uuid.UUID) (uuid.UUID, error) {
	// first fetch the trip ID for the record
	var recordModel RecordModel
	if err := p.db.First(&recordModel, "id = ?", recordID).Error; err != nil {
		return uuid.Nil, err // Record not found or other error
	}

	// GORM's CASCADE constraint on RecordModel should handle deleting associated
	// record_should_pay_address_lists.
	if err := p.db.Delete(&RecordModel{}, "id = ?", recordID).Error; err != nil {
		return uuid.Nil, err
	}
	return recordModel.TripID, nil
}

// Data Loader
// These are more complex and often involve custom SQL or optimized GORM queries
// to avoid N+1 problems. The implementations below are basic.

func (p *pgDBWrapper) DataLoaderGetRecordInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]db.RecordInfo, error) {
	var records []RecordModel
	if err := p.db.WithContext(ctx).Where("trip_id IN ?", tripIds).Find(&records).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]db.RecordInfo)
	for _, r := range records {
		result[r.TripID] = append(result[r.TripID], db.RecordInfo{
			ID:            r.ID,
			Name:          r.Name,
			Amount:        r.Amount,
			PrePayAddress: db.Address(r.PrePayAddress),
		})
	}
	// Ensure all requested tripIds have an entry in the map, even if empty
	for _, tripID := range tripIds {
		if _, ok := result[tripID]; !ok {
			result[tripID] = []db.RecordInfo{}
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]db.Address, error) {
	var addresses []TripAddressListModel
	if err := p.db.WithContext(ctx).Where("trip_id IN ?", tripIds).Find(&addresses).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]db.Address)
	for _, a := range addresses {
		result[a.TripID] = append(result[a.TripID], db.Address(a.Address))
	}
	// Ensure all requested tripIds have an entry in the map, even if empty
	for _, tripID := range tripIds {
		if _, ok := result[tripID]; !ok {
			result[tripID] = []db.Address{}
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetRecordShouldPayList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]db.Address, error) {
	var shouldPayAddresses []RecordShouldPayAddressListModel
	// Assuming RecordShouldPayAddressListModel has RecordID and Address
	if err := p.db.WithContext(ctx).Where("record_id IN ?", recordIds).Find(&shouldPayAddresses).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]db.Address)
	for _, sp := range shouldPayAddresses {
		result[sp.RecordID] = append(result[sp.RecordID], db.Address(sp.Address))
	}
	// Ensure all requested recordIds have an entry in the map, even if empty
	for _, recordID := range recordIds {
		if _, ok := result[recordID]; !ok {
			result[recordID] = []db.Address{}
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*db.TripInfo, error) {
	var trips []TripInfoModel
	if err := p.db.WithContext(ctx).Where("id IN ?", tripIds).Find(&trips).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID]*db.TripInfo)
	for _, t := range trips {
		result[t.ID] = &db.TripInfo{
			ID:   t.ID,
			Name: t.Name,
		}
	}
	// Ensure all requested tripIds have an entry in the map, even if nil
	for _, tripID := range tripIds {
		if _, ok := result[tripID]; !ok {
			result[tripID] = nil // Or an empty TripInfo if that's preferred
		}
	}
	return result, nil
}
