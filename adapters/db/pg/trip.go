package pg

import (
	"context"
	"errors"
	"fmt"

	"dtm/adapters/db/db"
	"dtm/domain"
	"dtm/libs/chainlist"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func uuidPtr(v uuid.UUID) *uuid.UUID { return &v }

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

func (p *pgDBWrapper) AppendNew(ctx context.Context, tripID uuid.UUID, record domain.Record, policy db.RecordMaterializer) (materialized domain.Record, err error) {
	if policy == nil {
		return domain.Record{}, db.ErrMaterializerRequired
	}
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		addresses, loadErr := loadTripAddresses(tx, tripID)
		if loadErr != nil {
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", db.ErrTripNotFound, tripID)
			}
			return loadErr
		}
		materialized, loadErr = policy.PrepareNew(record, addresses)
		if loadErr != nil {
			return loadErr
		}
		return insertRecord(tx, tripID, materialized)
	})
	return
}

func (p *pgDBWrapper) AppendPatch(ctx context.Context, tripID, targetID uuid.UUID, patch domain.RecordPatch, policy db.RecordMaterializer) (resultTripID uuid.UUID, materialized domain.Record, appended bool, err error) {
	if policy == nil {
		return uuid.Nil, domain.Record{}, false, db.ErrMaterializerRequired
	}
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock forward in chain order. After waiting, Read Committed returns
		// the updated child link so concurrent patches inherit the newest tail.
		loader := chainlist.LoaderFunc[uuid.UUID, RecordModel](func(ctx context.Context, id uuid.UUID) (chainlist.Node[uuid.UUID, RecordModel], error) {
			var model RecordModel
			loadErr := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("id = ? AND trip_id = ?", id, tripID).First(&model).Error
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return chainlist.Node[uuid.UUID, RecordModel]{}, fmt.Errorf("%w: %s", chainlist.ErrNodeNotFound, id)
			}
			if loadErr != nil {
				return chainlist.Node[uuid.UUID, RecordModel]{}, loadErr
			}
			return chainlist.Node[uuid.UUID, RecordModel]{ID: model.ID, ParentID: model.ParentRecordID, ChildID: model.ChildRecordID, Value: model}, nil
		})
		source, _ := chainlist.NewLazySource(loader, nil)
		var tailModel RecordModel
		for node, walkErr := range source.WalkCanonical(ctx, targetID, false) {
			if walkErr != nil {
				if tailModel.ID == uuid.Nil && errors.Is(walkErr, chainlist.ErrNodeNotFound) {
					return fmt.Errorf("%w: %s: %w", db.ErrRecordNotFound, targetID, walkErr)
				}
				return fmt.Errorf("%w: %w", db.ErrInvalidChain, walkErr)
			}
			tailModel = node.Value
		}
		if tailModel.ID == uuid.Nil {
			return fmt.Errorf("%w: %s: %w", db.ErrRecordNotFound, targetID, chainlist.ErrNodeNotFound)
		}
		addresses, loadErr := loadTripAddresses(tx, tripID)
		if loadErr != nil {
			return loadErr
		}
		tail, loadErr := loadDomainRecord(tx, tailModel, addresses)
		if loadErr != nil {
			return loadErr
		}
		resultTripID = tripID
		merged, changed, mergeErr := policy.ApplyPatch(tail, patch, addresses)
		if mergeErr != nil {
			return mergeErr
		}
		if !changed {
			materialized, appended = tail, false
			return nil
		}

		parentID := tail.ID
		merged.ID = uuid.New()
		merged.ParentRecordID = &parentID
		merged.ChildRecordID = nil
		result := tx.Model(&RecordModel{}).Where("id = ? AND trip_id = ? AND child_record_id IS NULL", tail.ID, tripID).Update("child_record_id", merged.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("record chain write invariant violated at tail %s", tail.ID)
		}
		if insertErr := insertRecord(tx, tripID, merged); insertErr != nil {
			return insertErr
		}
		materialized, appended = merged, true
		return nil
	})
	return
}

func insertRecord(tx *gorm.DB, tripID uuid.UUID, record domain.Record) error {
	model := RecordModel{ID: record.ID, TripID: tripID, ParentRecordID: record.ParentRecordID, ChildRecordID: record.ChildRecordID,
		Name: record.Name, Amount: record.Amount, Time: record.Time, PrePayAddressID: record.PrePayAddress.ID,
		Category: int(record.Category), IsDeleted: record.IsDeleted}
	if err := tx.Create(&model).Error; err != nil {
		return err
	}
	if len(record.ShouldPayAddress) == 0 {
		return nil
	}
	items := make([]RecordShouldPayAddressListModel, len(record.ShouldPayAddress))
	for i, address := range record.ShouldPayAddress {
		items[i] = RecordShouldPayAddressListModel{RecordID: record.ID, TripID: tripID, AddressID: address.Address.ID, ExtendedMsg: address.ExtendMsg}
	}
	return tx.Create(&items).Error
}

func loadTripAddresses(tx *gorm.DB, tripID uuid.UUID) ([]domain.Address, error) {
	var models []AddressModel
	if err := tx.Where("trip_id = ?", tripID).Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]domain.Address, len(models))
	for i, model := range models {
		result[i] = domain.Address{ID: model.ID, Name: model.Name}
	}
	return result, nil
}

func loadDomainRecord(tx *gorm.DB, model RecordModel, addresses []domain.Address) (domain.Record, error) {
	byID := make(map[uuid.UUID]domain.Address, len(addresses))
	for _, address := range addresses {
		byID[address.ID] = address
	}
	var shouldPay []RecordShouldPayAddressListModel
	if err := tx.Where("record_id = ? AND trip_id = ?", model.ID, model.TripID).Find(&shouldPay).Error; err != nil {
		return domain.Record{}, err
	}
	result := domain.Record{RecordInfo: recordInfoFromModel(model, byID), RecordData: domain.RecordData{ShouldPayAddress: make([]domain.ExtendAddress, len(shouldPay))}}
	for i, item := range shouldPay {
		result.ShouldPayAddress[i] = domain.ExtendAddress{Address: byID[item.AddressID], ExtendMsg: item.ExtendedMsg}
	}
	return result, nil
}

func (p *pgDBWrapper) UpdateTripInfo(info *domain.TripInfo) error {
	tripModel := TripInfoModel{
		ID:   info.ID,
		Name: info.Name,
	}
	return p.db.Model(&TripInfoModel{}).Where("id = ?", info.ID).Updates(tripModel).Error
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
	return p.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("trip_id = ?", id).Delete(&RecordShouldPayAddressListModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("trip_id = ?", id).Delete(&RecordModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("trip_id = ?", id).Delete(&AddressModel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&TripInfoModel{}, "id = ?", id).Error
	})
}

// joinedRecords materializes complete records in one SQL statement.
func (p *pgDBWrapper) joinedRecords(ctx context.Context, column string, ids []uuid.UUID, options db.RecordReadOptions) ([]db.RecordSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []db.RecordSnapshot{}, nil
	}
	var rows []struct {
		RecordModel    `gorm:"embedded"`
		PayerName      string
		ShareAddressID *uuid.UUID
		ShareName      string
		ExtendedMsg    float64
	}
	query := p.db.WithContext(ctx).Table("records AS r").
		Select("r.*, payer.name AS payer_name, share.address_id AS share_address_id, recipient.name AS share_name, share.extended_msg").
		Joins("LEFT JOIN addresses AS payer ON payer.id = r.pre_pay_address_id AND payer.trip_id = r.trip_id").
		Joins("LEFT JOIN record_should_pay_address_lists AS share ON share.record_id = r.id AND share.trip_id = r.trip_id").
		Joins("LEFT JOIN addresses AS recipient ON recipient.id = share.address_id AND recipient.trip_id = share.trip_id").
		Where(column+" IN ?", ids)
	if !options.HaveHistory {
		query = query.Where("r.child_record_id IS NULL AND r.is_deleted = false")
	}
	if err := query.Order("r.created_at, r.id, share.address_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]db.RecordSnapshot, 0)
	indexes := make(map[uuid.UUID]int)
	for _, row := range rows {
		index, exists := indexes[row.ID]
		if !exists {
			index = len(result)
			indexes[row.ID] = index
			payer := domain.Address{ID: row.PrePayAddressID, Name: row.PayerName}
			info := recordInfoFromModel(row.RecordModel, map[uuid.UUID]domain.Address{payer.ID: payer})
			result = append(result, db.RecordSnapshot{TripID: row.TripID, Record: domain.Record{RecordInfo: info, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{}}}})
		}
		if row.ShareAddressID != nil {
			result[index].ShouldPayAddress = append(result[index].ShouldPayAddress, domain.ExtendAddress{Address: domain.Address{ID: *row.ShareAddressID, Name: row.ShareName}, ExtendMsg: row.ExtendedMsg})
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetTripRecords(ctx context.Context, tripIds []uuid.UUID, options db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	records, err := p.joinedRecords(ctx, "r.trip_id", tripIds, options)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID][]db.RecordSnapshot, len(tripIds))
	for _, id := range tripIds {
		result[id] = []db.RecordSnapshot{}
	}
	for _, record := range records {
		result[record.TripID] = append(result[record.TripID], record)
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetRecordList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID]db.RecordSnapshot, error) {
	records, err := p.joinedRecords(ctx, "r.id", recordIds, db.RecordReadOptions{HaveHistory: true})
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]db.RecordSnapshot, len(records))
	for _, record := range records {
		result[record.ID] = record
	}
	errorsByID := make(map[uuid.UUID]error)
	for _, id := range recordIds {
		if _, ok := result[id]; !ok {
			errorsByID[id] = fmt.Errorf("%w: %w: %s", db.ErrRecordNotFound, chainlist.ErrNodeNotFound, id)
		}
	}
	if len(errorsByID) > 0 {
		return result, dataloadgen.MappedFetchError[uuid.UUID](errorsByID)
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

func recordInfoFromModel(r RecordModel, addresses map[uuid.UUID]domain.Address) domain.RecordInfo {
	return domain.RecordInfo{
		ID: r.ID, ParentRecordID: r.ParentRecordID, ChildRecordID: r.ChildRecordID, Name: r.Name, Amount: r.Amount,
		Time: r.Time, PrePayAddress: addresses[r.PrePayAddressID], Category: domain.RecordCategory(r.Category), IsDeleted: r.IsDeleted,
	}
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
