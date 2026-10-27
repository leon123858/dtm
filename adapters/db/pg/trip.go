package pg

import (
	"context"
	"errors"
	"fmt"

	"dtm/adapters/db/db"
	"dtm/adapters/db/utils"
	"dtm/domain"
	"dtm/libs/chainlist"
	"dtm/libs/textvalidate"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func uuidPtr(v uuid.UUID) *uuid.UUID { return &v }

// pgTripDBWrapper is an implementation of TripDBWrapper using GORM.
type pgTripDBWrapper struct {
	db *gorm.DB
}

// NewPgTripDBWrapper creates a new instance of pgTripDBWrapper.
func NewPgTripDBWrapper(db *gorm.DB) db.TripDBWrapper {
	return &pgTripDBWrapper{db: db}
}

func (p *pgTripDBWrapper) CreateTrip(info *domain.TripInfo) error {
	tripModel := TripInfoModel{
		ID:   info.ID,
		Name: info.Name,
	}
	return p.db.Create(&tripModel).Error
}

func (p *pgTripDBWrapper) AppendNew(ctx context.Context, tripID uuid.UUID, record domain.Record, policy db.RecordMaterializer) (materialized domain.Record, err error) {
	if policy == nil {
		return domain.Record{}, db.ErrMaterializerRequired
	}
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccess(tx, ctx, tripID, tripShared); err != nil {
			return err
		}
		addresses, display, loadErr := loadTripAddresses(tx, tripID)
		if loadErr != nil {
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", domain.ErrTripNotFound, tripID)
			}
			return loadErr
		}
		materialized, loadErr = policy.PrepareNew(record, addresses)
		if loadErr != nil {
			return loadErr
		}
		if e := insertRecord(tx, tripID, materialized); e != nil {
			return e
		}
		materialized = projectRecord(materialized, display)
		return nil
	})
	return
}

func (p *pgTripDBWrapper) AppendPatch(ctx context.Context, tripID, targetID uuid.UUID, patch domain.RecordPatch, policy db.RecordMaterializer) (resultTripID uuid.UUID, materialized domain.Record, appended bool, err error) {
	if policy == nil {
		return uuid.Nil, domain.Record{}, false, db.ErrMaterializerRequired
	}
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccess(tx, ctx, tripID, tripShared); err != nil {
			return err
		}
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
					return fmt.Errorf("%w: %s: %w", domain.ErrRecordNotFound, targetID, walkErr)
				}
				return fmt.Errorf("%w: %w", domain.ErrInvalidChain, walkErr)
			}
			tailModel = node.Value
		}
		if tailModel.ID == uuid.Nil {
			return fmt.Errorf("%w: %s: %w", domain.ErrRecordNotFound, targetID, chainlist.ErrNodeNotFound)
		}
		addresses, display, loadErr := loadTripAddresses(tx, tripID)
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
			materialized = projectRecord(materialized, display)
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
		materialized = projectRecord(materialized, display)
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

func loadTripAddresses(tx *gorm.DB, tripID uuid.UUID) ([]domain.Address, map[uuid.UUID]domain.Address, error) {
	var rows []struct {
		AddressModel
		BoundName *string
	}
	err := tx.Table("addresses AS a").Select("a.*, u.name AS bound_name").Joins("LEFT JOIN trip_bindings b ON b.trip_id = a.trip_id AND b.address_id = a.id").Joins("LEFT JOIN users u ON u.id = b.user_id").Where("a.trip_id = ?", tripID).Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	raw := make([]domain.Address, len(rows))
	display := map[uuid.UUID]domain.Address{}
	for i, row := range rows {
		a := addressDomain(row.AddressModel)
		raw[i] = a
		display[a.ID] = a.Display(row.BoundName)
	}
	return raw, display, nil
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

func (p *pgTripDBWrapper) UpdateTripInfo(ctx context.Context, info *domain.TripInfo) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := lockTrip(tx, info.ID, tripExclusive)
		if err != nil {
			return err
		}
		if err := checkAccess(tx, ctx, current); err != nil {
			return err
		}
		info.Private = current.Private
		return tx.Model(current).Update("name", info.Name).Error
	})
}
func (p *pgTripDBWrapper) CreateAddress(ctx context.Context, tripID uuid.UUID, name string) (out *domain.Address, err error) {
	returnValue, _, err := p.CreateAddressWithOptions(ctx, tripID, &name, nil, false)
	return returnValue, err
}
func (p *pgTripDBWrapper) UpdateAddress(ctx context.Context, tripID, addressID uuid.UUID, name string) (out *domain.Address, err error) {
	returnValue, _, err := p.PatchAddress(ctx, tripID, addressID, &name, nil)
	return returnValue, err
}
func (p *pgTripDBWrapper) DeleteAddress(ctx context.Context, tripID, addressID uuid.UUID) (out *domain.Address, err error) {
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccess(tx, ctx, tripID, tripShared); err != nil {
			return err
		}
		var m AddressModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trip_id = ? AND id = ?", tripID, addressID).Take(&m).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrAddressNotFound
			}
			return err
		}
		var count int64
		if err := tx.Model(&TripBindingModel{}).Where("trip_id = ? AND address_id = ?", tripID, addressID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return domain.ErrConflict
		}
		if err := tx.Delete(&m).Error; err != nil {
			return addressDeleteError(err)
		}
		address := addressDomain(m)
		out = &address
		return nil
	})
	return
}
func (p *pgTripDBWrapper) DeleteTrip(ctx context.Context, id uuid.UUID) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccess(tx, ctx, id, tripExclusive); err != nil {
			return err
		}
		if err := tx.Where("trip_id = ?", id).Delete(&TripBindingModel{}).Error; err != nil {
			return err
		}
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
func (p *pgTripDBWrapper) joinedRecords(ctx context.Context, column clause.Column, ids []uuid.UUID, options db.RecordReadOptions) ([]db.RecordSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []db.RecordSnapshot{}, nil
	}
	var rows []struct {
		RecordModel      `gorm:"embedded"`
		PayerUseNickname bool
		ShareUseNickname bool
		PayerName        string
		ShareAddressID   *uuid.UUID
		ShareName        string
		ExtendedMsg      float64
	}
	values := make([]any, len(ids))
	for i, id := range ids {
		values[i] = id
	}
	query := p.db.WithContext(ctx).Table("records AS r").
		Select("r.*, CASE WHEN NOT payer.use_nickname THEN COALESCE(NULLIF(pu.name, ''),payer.name) ELSE payer.name END AS payer_name, payer.use_nickname AS payer_use_nickname, share.address_id AS share_address_id, CASE WHEN NOT recipient.use_nickname THEN COALESCE(NULLIF(ru.name, ''),recipient.name) ELSE recipient.name END AS share_name, recipient.use_nickname AS share_use_nickname, share.extended_msg").
		Joins("LEFT JOIN addresses AS payer ON payer.id = r.pre_pay_address_id AND payer.trip_id = r.trip_id").
		Joins("LEFT JOIN record_should_pay_address_lists AS share ON share.record_id = r.id AND share.trip_id = r.trip_id").
		Joins("LEFT JOIN addresses AS recipient ON recipient.id = share.address_id AND recipient.trip_id = share.trip_id").
		Joins("LEFT JOIN trip_bindings pb ON pb.trip_id = payer.trip_id AND pb.address_id = payer.id").Joins("LEFT JOIN users pu ON pu.id = pb.user_id").Joins("LEFT JOIN trip_bindings rb ON rb.trip_id = recipient.trip_id AND rb.address_id = recipient.id").Joins("LEFT JOIN users ru ON ru.id = rb.user_id").Where(clause.IN{Column: column, Values: values})
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
			payer := domain.Address{ID: row.PrePayAddressID, Name: row.PayerName, UseNickname: row.PayerUseNickname}
			info := recordInfoFromModel(row.RecordModel, map[uuid.UUID]domain.Address{payer.ID: payer})
			result = append(result, db.RecordSnapshot{TripID: row.TripID, Record: domain.Record{RecordInfo: info, RecordData: domain.RecordData{ShouldPayAddress: []domain.ExtendAddress{}}}})
		}
		if row.ShareAddressID != nil {
			result[index].ShouldPayAddress = append(result[index].ShouldPayAddress, domain.ExtendAddress{Address: domain.Address{ID: *row.ShareAddressID, Name: row.ShareName, UseNickname: row.ShareUseNickname}, ExtendMsg: row.ExtendedMsg})
		}
	}
	return result, nil
}

func (p *pgTripDBWrapper) DataLoaderGetTripRecords(ctx context.Context, tripIds []uuid.UUID, options db.RecordReadOptions) (map[uuid.UUID][]db.RecordSnapshot, error) {
	if err := p.batchAccess(ctx, tripIds); err != nil {
		return nil, err
	}
	records, err := p.joinedRecords(ctx, clause.Column{Table: "r", Name: "trip_id"}, tripIds, options)
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

func (p *pgTripDBWrapper) DataLoaderGetRecordList(ctx context.Context, recordIds []uuid.UUID) (map[uuid.UUID]db.RecordSnapshot, error) {
	records, err := p.joinedRecords(ctx, clause.Column{Table: "r", Name: "id"}, recordIds, db.RecordReadOptions{HaveHistory: true})
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]db.RecordSnapshot, len(records))
	tripIDs := make([]uuid.UUID, 0, len(records))
	for _, record := range records {
		tripIDs = append(tripIDs, record.TripID)
	}
	if err := p.batchAccess(ctx, tripIDs); err != nil {
		return nil, err
	}
	for _, record := range records {
		result[record.ID] = record
	}
	errorsByID := make(map[uuid.UUID]error)
	for _, id := range recordIds {
		if _, ok := result[id]; !ok {
			errorsByID[id] = fmt.Errorf("%w: %w: %s", domain.ErrRecordNotFound, chainlist.ErrNodeNotFound, id)
		}
	}
	if len(errorsByID) > 0 {
		return result, dataloadgen.MappedFetchError[uuid.UUID](errorsByID)
	}
	return result, nil
}

func (p *pgTripDBWrapper) DataLoaderGetTripAddressList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID][]domain.Address, error) {
	if err := p.batchAccess(ctx, tripIds); err != nil {
		return nil, err
	}
	var addresses []AddressModel
	if err := addressQuery(p.db.WithContext(ctx)).Where("a.trip_id IN ?", tripIds).Find(&addresses).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID][]domain.Address)
	for _, a := range addresses {
		result[a.TripID] = append(result[a.TripID], addressDomain(a))
	}
	// Ensure all requested tripIds have an entry in the map, even if empty
	for _, tripID := range tripIds {
		if _, ok := result[tripID]; !ok {
			result[tripID] = []domain.Address{}
		}
	}
	return result, nil
}

func (p *pgTripDBWrapper) DataLoaderGetTripInfoList(ctx context.Context, tripIds []uuid.UUID) (map[uuid.UUID]*domain.TripInfo, error) {
	if err := p.batchAccess(ctx, tripIds); err != nil {
		return nil, err
	}
	var trips []TripInfoModel
	if err := p.db.WithContext(ctx).Where("id IN ?", tripIds).Find(&trips).Error; err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID]*domain.TripInfo)
	for _, t := range trips {
		result[t.ID] = &domain.TripInfo{
			ID:      t.ID,
			Name:    t.Name,
			Private: t.Private,
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

func (p *pgTripDBWrapper) addressesByID(ids []uuid.UUID) (map[uuid.UUID]domain.Address, error) {
	result := make(map[uuid.UUID]domain.Address, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var models []AddressModel
	if err := p.db.Where("id IN ?", ids).Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		result[model.ID] = addressDomain(model)
	}
	return result, nil
}

func addressDomain(a AddressModel) domain.Address {
	return domain.Address{ID: a.ID, Name: a.Name, UseNickname: a.UseNickname}
}

// Projection joins at most one binding per address; display names never affect identity.
func addressQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("addresses AS a").Select("a.*, CASE WHEN NOT a.use_nickname THEN COALESCE(NULLIF(u.name, ''), a.name) ELSE a.name END AS name").Joins("LEFT JOIN trip_bindings b ON b.trip_id = a.trip_id AND b.address_id = a.id").Joins("LEFT JOIN users u ON u.id = b.user_id")
}
func displayAddress(tx *gorm.DB, tid, aid uuid.UUID) (*domain.Address, error) {
	var a AddressModel
	err := addressQuery(tx).Where("a.trip_id = ? AND a.id = ?", tid, aid).Take(&a).Error
	out := addressDomain(a)
	return &out, err
}
func (p *pgTripDBWrapper) CreateAddressWithOptions(ctx context.Context, tid uuid.UUID, name *string, useNickname *bool, bind bool) (out *domain.Address, created bool, err error) {
	var uid uuid.UUID
	if bind {
		uid, err = bindingUser(ctx)
		if err != nil {
			return
		}
	}
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Exclusive trip lock serializes name selection and membership creation, including first membership.
		mode := tripShared
		if bind {
			mode = tripExclusive
		}
		if e := lockAccess(tx, ctx, tid, mode); e != nil {
			return e
		}
		var u UserModel
		if bind {
			if e := tx.Where("id = ?", uid).Take(&u).Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return domain.ErrUserNotFound
				}
				return e
			}
			var b TripBindingModel
			e := tx.Where("trip_id = ? AND user_id = ?", tid, uid).Take(&b).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if b.AddressID != nil {
				out, e = displayAddress(tx, tid, *b.AddressID)
				return e
			}
		}
		var addresses []AddressModel
		if e := tx.Where("trip_id = ?", tid).Find(&addresses).Error; e != nil {
			return e
		}
		exists := func(n string) bool {
			for _, a := range addresses {
				if a.Name == n {
					return true
				}
			}
			return false
		}
		n, e := utils.AddressName(name, u.Name, bind, exists)
		if e != nil {
			return e
		}
		if exists(n) {
			return domain.ErrConflict
		}
		a := AddressModel{ID: uuid.New(), TripID: tid, Name: n}
		if useNickname != nil {
			a.UseNickname = *useNickname
		}
		if e := tx.Create(&a).Error; e != nil {
			return e
		}
		if bind {
			if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "trip_id"}}, DoUpdates: clause.AssignmentColumns([]string{"address_id"})}).Create(&TripBindingModel{UserID: uid, TripID: tid, AddressID: &a.ID}).Error; e != nil {
				return bindingError(e)
			}
		}
		created = true
		out, e = displayAddress(tx, tid, a.ID)
		return e
	})
	if err != nil {
		created = false
	}
	return
}
func (p *pgTripDBWrapper) PatchAddress(ctx context.Context, tid, aid uuid.UUID, name *string, useNickname *bool) (out *domain.Address, changed bool, err error) {
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockAccess(tx, ctx, tid, tripShared); e != nil {
			return e
		}
		var a AddressModel
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trip_id = ? AND id = ?", tid, aid).Take(&a).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return domain.ErrAddressNotFound
			}
			return e
		}
		if name != nil {
			if e := textvalidate.ValidateName(*name); e != nil {
				return fmt.Errorf("%w: %v", domain.ErrBadUserInput, e)
			}
		}
		updates := map[string]interface{}{}
		if name != nil && *name != a.Name {
			updates["name"] = *name
		}
		if useNickname != nil && *useNickname != a.UseNickname {
			updates["use_nickname"] = *useNickname
		}
		changed = len(updates) > 0
		if changed {
			if e := tx.Model(&a).Updates(updates).Error; e != nil {
				return bindingError(e)
			}
		}
		var e error
		out, e = displayAddress(tx, tid, aid)
		return e
	})
	if err != nil {
		changed = false
	}
	return
}

func projectRecord(r domain.Record, byID map[uuid.UUID]domain.Address) domain.Record {
	r.PrePayAddress = byID[r.PrePayAddress.ID]
	for i := range r.ShouldPayAddress {
		r.ShouldPayAddress[i].Address = byID[r.ShouldPayAddress[i].Address.ID]
	}
	return r
}
