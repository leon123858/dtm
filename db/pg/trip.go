package pg

import (
	"context"
	"fmt"

	"dtm/db/db"
	"dtm/domain"
	cdiff "dtm/libs/diff"

	"github.com/google/uuid"
	"github.com/r3labs/diff/v3"
	"gorm.io/gorm"
)

// pgDBWrapper is an implementation of TripDBWrapper using GORM.
type pgDBWrapper struct {
	db *gorm.DB
}

// NewPgDBWrapper creates a new instance of pgDBWrapper.
func NewPgDBWrapper(db *gorm.DB) db.TripDBWrapper {
	return &pgDBWrapper{db: db}
}

func (p *pgDBWrapper) CreateTrip(info *domain.TripInfo) error {
	tripModel := TripInfoModel{
		ID:   info.ID,
		Name: info.Name,
	}
	return p.db.Create(&tripModel).Error
}

func (p *pgDBWrapper) CreateTripRecords(id uuid.UUID, records []domain.Record) error {
	// This can be done in a transaction for atomicity
	return p.db.Transaction(func(tx *gorm.DB) error {
		for _, rec := range records {
			recordModel := RecordModel{
				ID:              rec.RecordInfo.ID,
				TripID:          id, // Link to the trip
				Name:            rec.RecordInfo.Name,
				Amount:          rec.RecordInfo.Amount,
				Time:            rec.RecordInfo.Time,
				PrePayAddressID: rec.RecordInfo.PrePayAddress.ID,
				Category:        int(rec.RecordInfo.Category),
			}
			if err := tx.Create(&recordModel).Error; err != nil {
				return err
			}

			// Create entries in RecordShouldPayAddressListModel
			for _, addr := range rec.RecordData.ShouldPayAddress {
				shouldPayModel := RecordShouldPayAddressListModel{
					RecordID:    rec.RecordInfo.ID,
					TripID:      id, // Link to the trip
					AddressID:   addr.Address.ID,
					ExtendedMsg: addr.ExtendMsg,
				}
				if err := tx.Create(&shouldPayModel).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (p *pgDBWrapper) GetTripInfo(id uuid.UUID) (*domain.TripInfo, error) {
	var tripModel TripInfoModel
	if err := p.db.First(&tripModel, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &domain.TripInfo{
		ID:   tripModel.ID,
		Name: tripModel.Name,
	}, nil
}

func (p *pgDBWrapper) GetTripRecords(id uuid.UUID) ([]domain.RecordInfo, error) {
	var recordModels []RecordModel
	if err := p.db.Where("trip_id = ?", id).Find(&recordModels).Error; err != nil {
		return nil, err
	}

	addresses, err := p.addressesByID(recordModelsAddressIDs(recordModels))
	if err != nil {
		return nil, err
	}
	var recordInfos []domain.RecordInfo
	for _, rm := range recordModels {
		recordInfos = append(recordInfos, domain.RecordInfo{
			ID:            rm.ID,
			Name:          rm.Name,
			Amount:        rm.Amount,
			PrePayAddress: addresses[rm.PrePayAddressID],
			Time:          rm.Time,
			Category:      domain.RecordCategory(rm.Category),
		})
	}
	return recordInfos, nil
}

func (p *pgDBWrapper) GetTripAddressList(id uuid.UUID) ([]domain.Address, error) {
	var addressModels []AddressModel
	if err := p.db.Where("trip_id = ?", id).Find(&addressModels).Error; err != nil {
		return nil, err
	}

	var addresses []domain.Address
	for _, am := range addressModels {
		addresses = append(addresses, domain.Address{ID: am.ID, Name: am.Name})
	}
	return addresses, nil
}

func (p *pgDBWrapper) GetRecordAddressList(recordID uuid.UUID) ([]domain.ExtendAddress, error) {
	var shouldPayModels []RecordShouldPayAddressListModel
	if err := p.db.Where("record_id = ?", recordID).Find(&shouldPayModels).Error; err != nil {
		return nil, err
	}

	addressIDs := make([]uuid.UUID, 0, len(shouldPayModels))
	for _, spm := range shouldPayModels {
		addressIDs = append(addressIDs, spm.AddressID)
	}
	addressByID, err := p.addressesByID(addressIDs)
	if err != nil {
		return nil, err
	}
	var addresses []domain.ExtendAddress
	for _, spm := range shouldPayModels {
		addresses = append(addresses, domain.ExtendAddress{
			Address:   addressByID[spm.AddressID],
			ExtendMsg: spm.ExtendedMsg,
		})
	}
	return addresses, nil
}

func (p *pgDBWrapper) GetRecordTripID(recordID uuid.UUID) (uuid.UUID, error) {
	var record RecordModel
	if err := p.db.Select("trip_id").First(&record, "id = ?", recordID).Error; err != nil {
		return uuid.Nil, err
	}
	return record.TripID, nil
}

func (p *pgDBWrapper) UpdateTripInfo(info *domain.TripInfo) error {
	tripModel := TripInfoModel{
		ID:   info.ID,
		Name: info.Name,
	}
	return p.db.Model(&TripInfoModel{}).Where("id = ?", info.ID).Updates(tripModel).Error
}

func (p *pgDBWrapper) UpdateTripRecord(recordID uuid.UUID, changeLog diff.Changelog) (uuid.UUID, error) {
	// use transaction to update info and data
	tripId := uuid.Nil
	ret := p.db.Transaction(func(tx *gorm.DB) error {
		// load cur data
		var recordModel RecordModel
		var shouldPayModels []RecordShouldPayAddressListModel
		if err := tx.First(&recordModel, "id = ?", recordID).Error; err != nil {
			return err
		}
		if err := tx.Where("record_id = ?", recordID).Find(&shouldPayModels).Error; err != nil {
			return err
		}
		// convert to interface
		record := &domain.Record{
			RecordInfo: domain.RecordInfo{
				ID:            recordModel.ID,
				Name:          recordModel.Name,
				Amount:        recordModel.Amount,
				Time:          recordModel.Time,
				PrePayAddress: domain.Address{ID: recordModel.PrePayAddressID},
				Category:      domain.RecordCategory(recordModel.Category),
			},
			RecordData: domain.RecordData{
				ShouldPayAddress: make([]domain.ExtendAddress, len(shouldPayModels)),
			},
		}
		for i, d := range shouldPayModels {
			record.ShouldPayAddress[i] = domain.ExtendAddress{
				Address:   domain.Address{ID: d.AddressID},
				ExtendMsg: d.ExtendedMsg,
			}
		}

		// apply patch
		if pl := cdiff.GetCustomDiffer().Patch(changeLog, &record); pl.HasErrors() {
			return fmt.Errorf("record %s patch failed", recordID)
		}

		// convert back to db model
		newRecordModel := RecordModel{
			ID:              recordModel.ID,     // Keep same record ID
			TripID:          recordModel.TripID, // Keep the same trip ID
			Name:            record.RecordInfo.Name,
			Amount:          record.RecordInfo.Amount,
			Time:            record.RecordInfo.Time,
			PrePayAddressID: record.RecordInfo.PrePayAddress.ID,
			Category:        int(record.RecordInfo.Category), // Use int to store the category
		}
		// update db
		if err := tx.Model(&RecordModel{}).Where("id = ?", record.RecordInfo.ID).Updates(&newRecordModel).Error; err != nil {
			return err
		}
		if err := tx.Where("record_id = ?", record.RecordInfo.ID).Delete(&RecordShouldPayAddressListModel{}).Error; err != nil {
			return err
		}

		// insert batch
		models := make([]RecordShouldPayAddressListModel, 0, len(record.RecordData.ShouldPayAddress))
		for _, addr := range record.RecordData.ShouldPayAddress {
			if addr.Address.ID == uuid.Nil {
				continue
			}
			shouldPayModel := RecordShouldPayAddressListModel{
				RecordID:    record.RecordInfo.ID,
				TripID:      recordModel.TripID, // Link to the trip
				AddressID:   addr.Address.ID,
				ExtendedMsg: addr.ExtendMsg,
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

func (p *pgDBWrapper) CreateAddress(tripID uuid.UUID, name string) (*domain.Address, error) {
	model := AddressModel{ID: uuid.New(), TripID: tripID, Name: name}
	if err := p.db.Create(&model).Error; err != nil {
		return nil, err
	}
	return &domain.Address{ID: model.ID, Name: model.Name}, nil
}

func (p *pgDBWrapper) UpdateAddress(tripID, addressID uuid.UUID, name string) (*domain.Address, error) {
	result := p.db.Model(&AddressModel{}).Where("trip_id = ? AND id = ?", tripID, addressID).Update("name", name)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &domain.Address{ID: addressID, Name: name}, nil
}

func (p *pgDBWrapper) DeleteAddress(tripID, addressID uuid.UUID) (*domain.Address, error) {
	var model AddressModel
	if err := p.db.Where("trip_id = ? AND id = ?", tripID, addressID).First(&model).Error; err != nil {
		return nil, err
	}
	if err := p.db.Delete(&model).Error; err != nil {
		return nil, err
	}
	return &domain.Address{ID: model.ID, Name: model.Name}, nil
}

func (p *pgDBWrapper) DeleteTrip(id uuid.UUID) error {
	return p.db.Delete(&TripInfoModel{}, "id = ?", id).Error
}

func (p *pgDBWrapper) DeleteTripRecord(recordID uuid.UUID) (uuid.UUID, error) {
	// first fetch the trip ID for the record
	var recordModel RecordModel
	if err := p.db.First(&recordModel, "id = ?", recordID).Error; err != nil {
		return uuid.Nil, err // Record not found or other error
	}

	ret := p.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("record_id = ?", recordID).Delete(&RecordShouldPayAddressListModel{}).Error; err != nil {
			return err
		}

		if err := tx.Delete(&RecordModel{}, "id = ?", recordID).Error; err != nil {
			return err
		}

		return nil
	})
	if ret != nil {
		return uuid.Nil, ret
	}
	return recordModel.TripID, nil
}

// DataLoaderGetRecordInfoList Data Loader
// These are more complex and often involve custom SQL or optimized GORM queries
// to avoid N+1 problems. The implementations below are basic.
func (p *pgDBWrapper) DataLoaderGetRecordInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.RecordInfo, error) {
	var records []RecordModel
	if err := p.db.WithContext(ctx).Where("trip_id IN ?", tripIds).Find(&records).Error; err != nil {
		return nil, err
	}
	addresses, err := p.addressesByID(recordModelsAddressIDs(records))
	if err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]domain.RecordInfo)
	for _, r := range records {
		result[r.TripID] = append(result[r.TripID], domain.RecordInfo{
			ID:            r.ID,
			Name:          r.Name,
			Amount:        r.Amount,
			Time:          r.Time,
			PrePayAddress: addresses[r.PrePayAddressID],
			Category:      domain.RecordCategory(r.Category),
		})
	}
	// Ensure all requested tripIds have an entry in the map, even if empty
	for _, tripID := range tripIds {
		if _, ok := result[tripID]; !ok {
			result[tripID] = []domain.RecordInfo{}
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	var addresses []AddressModel
	if err := p.db.WithContext(ctx).Where("trip_id IN ?", tripIds).Find(&addresses).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]domain.Address)
	for _, a := range addresses {
		result[a.TripID] = append(result[a.TripID], domain.Address{ID: a.ID, Name: a.Name})
	}
	// Ensure all requested tripIds have an entry in the map, even if empty
	for _, tripID := range tripIds {
		if _, ok := result[tripID]; !ok {
			result[tripID] = []domain.Address{}
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetRecordShouldPayList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID][]domain.ExtendAddress, error) {
	var shouldPayAddresses []RecordShouldPayAddressListModel
	// Assuming RecordShouldPayAddressListModel has RecordID and Address
	if err := p.db.WithContext(ctx).Where("record_id IN ?", recordIds).Find(&shouldPayAddresses).Error; err != nil {
		return nil, err
	}

	addressIDs := make([]uuid.UUID, 0, len(shouldPayAddresses))
	for _, sp := range shouldPayAddresses {
		addressIDs = append(addressIDs, sp.AddressID)
	}
	addressByID, err := p.addressesByID(addressIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID][]domain.ExtendAddress)
	for _, sp := range shouldPayAddresses {
		result[sp.RecordID] = append(result[sp.RecordID], domain.ExtendAddress{
			Address:   addressByID[sp.AddressID],
			ExtendMsg: sp.ExtendedMsg,
		})
	}
	// Ensure all requested recordIds have an entry in the map, even if empty
	for _, recordID := range recordIds {
		if _, ok := result[recordID]; !ok {
			result[recordID] = []domain.ExtendAddress{}
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	var trips []TripInfoModel
	if err := p.db.WithContext(ctx).Where("id IN ?", tripIds).Find(&trips).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID]*domain.TripInfo)
	for _, t := range trips {
		result[t.ID] = &domain.TripInfo{
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

func recordModelsAddressIDs(records []RecordModel) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.PrePayAddressID)
	}
	return ids
}

func (p *pgDBWrapper) addressesByID(ids []uuid.UUID) (map[uuid.UUID]domain.Address, error) {
	result := make(map[uuid.UUID]domain.Address, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var models []AddressModel
	if err := p.db.Where("id IN ?", ids).Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		result[model.ID] = domain.Address{ID: model.ID, Name: model.Name}
	}
	return result, nil
}
