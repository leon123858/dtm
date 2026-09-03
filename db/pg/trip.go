package pg

import (
	"context"
	"errors"
	"fmt"

	chainpkg "dtm/chain"
	"dtm/db/db"
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

func (p *pgDBWrapper) AppendNew(ctx context.Context, tripID uuid.UUID, record domain.Record) error {
	if record.ParentRecordID != nil || record.ChildRecordID != nil {
		return fmt.Errorf("root record must not have chain links")
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return insertRecord(tx, tripID, record)
	})
}

func (p *pgDBWrapper) AppendPatch(ctx context.Context, targetID uuid.UUID, patch domain.RecordPatch) (tripID uuid.UUID, materialized domain.Record, appended bool, err error) {
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		root, resolveErr := resolveRootModel(tx, targetID)
		if resolveErr != nil {
			return &chainpkg.TailResolutionError{TargetID: targetID, Err: resolveErr}
		}
		tripID = root.TripID
		if lockErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND trip_id = ?", root.ID, tripID).First(&root).Error; lockErr != nil {
			return lockErr
		}

		loader := chainlist.LoaderFunc[uuid.UUID, domain.RecordInfo](func(ctx context.Context, id uuid.UUID) (chainlist.Node[uuid.UUID, domain.RecordInfo], error) {
			var model RecordModel
			loadErr := tx.WithContext(ctx).Where("id = ? AND trip_id = ?", id, tripID).First(&model).Error
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return chainlist.Node[uuid.UUID, domain.RecordInfo]{}, fmt.Errorf("%w: %s", chainlist.ErrNodeNotFound, id)
			}
			if loadErr != nil {
				return chainlist.Node[uuid.UUID, domain.RecordInfo]{}, loadErr
			}
			info := recordInfoFromModel(model, nil)
			return chainpkg.InfoNode(info), nil
		})
		source, _ := chainlist.NewLazySource(loader, nil)
		var tailInfo domain.RecordInfo
		for node, walkErr := range source.WalkCanonical(ctx, targetID) {
			if walkErr != nil {
				return &chainpkg.TailResolutionError{TargetID: targetID, Err: walkErr}
			}
			tailInfo = node.Value
		}
		if tailInfo.ID == uuid.Nil {
			return &chainpkg.TailResolutionError{TargetID: targetID, Err: fmt.Errorf("%w: %s", chainlist.ErrNodeNotFound, targetID)}
		}
		tail, loadErr := loadDomainRecord(tx, tripID, tailInfo.ID)
		if loadErr != nil {
			return loadErr
		}
		addresses, loadErr := loadTripAddresses(tx, tripID)
		if loadErr != nil {
			return loadErr
		}
		merged, changed, mergeErr := chainpkg.MergeRecordPatch(tail, patch, addresses)
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
	for _, address := range record.ShouldPayAddress {
		item := RecordShouldPayAddressListModel{RecordID: record.ID, TripID: tripID, AddressID: address.Address.ID, ExtendedMsg: address.ExtendMsg}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func resolveRootModel(tx *gorm.DB, targetID uuid.UUID) (RecordModel, error) {
	currentID := targetID
	seen := make(map[uuid.UUID]struct{})
	var tripID uuid.UUID
	for {
		if _, duplicate := seen[currentID]; duplicate {
			return RecordModel{}, fmt.Errorf("%w at node %s", chainlist.ErrCycle, currentID)
		}
		seen[currentID] = struct{}{}
		var current RecordModel
		query := tx.Where("id = ?", currentID)
		if tripID != uuid.Nil {
			query = query.Where("trip_id = ?", tripID)
		}
		err := query.First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if currentID == targetID {
				return RecordModel{}, fmt.Errorf("%w: %s", chainlist.ErrNodeNotFound, currentID)
			}
			return RecordModel{}, fmt.Errorf("%w: parent %s", chainlist.ErrDanglingParent, currentID)
		}
		if err != nil {
			return RecordModel{}, err
		}
		if tripID == uuid.Nil {
			tripID = current.TripID
		}
		if current.ParentRecordID == nil {
			return current, nil
		}
		var parent RecordModel
		err = tx.Where("id = ? AND trip_id = ?", *current.ParentRecordID, tripID).First(&parent).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RecordModel{}, fmt.Errorf("%w: parent %s of node %s", chainlist.ErrDanglingParent, *current.ParentRecordID, current.ID)
		}
		if err != nil {
			return RecordModel{}, err
		}
		if parent.ChildRecordID == nil || *parent.ChildRecordID != current.ID {
			return RecordModel{}, fmt.Errorf("%w: parent %s does not select child %s", chainlist.ErrNonCanonical, parent.ID, current.ID)
		}
		currentID = parent.ID
	}
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

func loadDomainRecord(tx *gorm.DB, tripID, recordID uuid.UUID) (domain.Record, error) {
	var model RecordModel
	if err := tx.Where("id = ? AND trip_id = ?", recordID, tripID).First(&model).Error; err != nil {
		return domain.Record{}, err
	}
	addresses, err := loadTripAddresses(tx, tripID)
	if err != nil {
		return domain.Record{}, err
	}
	byID := make(map[uuid.UUID]domain.Address, len(addresses))
	for _, address := range addresses {
		byID[address.ID] = address
	}
	var shouldPay []RecordShouldPayAddressListModel
	if err := tx.Where("record_id = ? AND trip_id = ?", recordID, tripID).Find(&shouldPay).Error; err != nil {
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
		result[r.TripID] = append(result[r.TripID], recordInfoFromModel(r, addresses))
	}
	// Ensure all requested tripIds have an entry in the map, even if empty
	for _, tripID := range tripIds {
		if _, ok := result[tripID]; !ok {
			result[tripID] = []domain.RecordInfo{}
		}
	}
	return result, nil
}

func (p *pgDBWrapper) DataLoaderGetRecordList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID]chainpkg.RecordNode, error) {
	var records []RecordModel
	if err := p.db.WithContext(ctx).Where("id IN ?", recordIds).Find(&records).Error; err != nil {
		return nil, err
	}
	addresses, err := p.addressesByID(recordModelsAddressIDs(records))
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]chainpkg.RecordNode, len(records))
	for _, record := range records {
		result[record.ID] = chainpkg.RecordNode{TripID: record.TripID, Info: recordInfoFromModel(record, addresses)}
	}
	errorsByID := make(map[uuid.UUID]error)
	for _, id := range recordIds {
		if _, ok := result[id]; !ok {
			errorsByID[id] = fmt.Errorf("%w: record %s", chainlist.ErrNodeNotFound, id)
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
