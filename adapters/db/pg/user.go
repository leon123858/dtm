package pg

import (
	"context"
	"errors"
	"fmt"

	"dtm/adapters/db/db"
	"dtm/adapters/db/utils"
	"dtm/domain"
	"dtm/libs/textvalidate"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ db.AuthDBWrapper = (*pgAuthDBWrapper)(nil)

func (p *pgAuthDBWrapper) UpsertUser(ctx context.Context, gcid string, email ...string) (domain.User, error) {
	if textvalidate.ValidateNonBlank(gcid) != nil {
		return domain.User{}, db.ErrInvalidGCID
	}
	u := UserModel{ID: uuid.New(), GCID: gcid}
	updates := map[string]interface{}{"gcid": gcid}
	if len(email) > 0 && email[0] != "" {
		u.Email = &email[0]
		updates["email"] = email[0]
	}
	err := p.db.WithContext(ctx).Clauses(
		clause.OnConflict{Columns: []clause.Column{{Name: "gcid"}}, DoUpdates: clause.Assignments(updates)},
		clause.Returning{},
	).Create(&u).Error
	return userDomain(u), err
}
func (p *pgAuthDBWrapper) UserGCID2UserId(ctx context.Context, gcid string) (uuid.UUID, error) {
	var u UserModel
	result := p.db.WithContext(ctx).Model(&UserModel{}).Select("id").Where("gcid = ?", gcid).Take(&u)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return uuid.Nil, domain.ErrUserNotFound
	}
	return u.ID, result.Error
}
func (p *pgAuthDBWrapper) SubscribedTrips(ctx context.Context, uid uuid.UUID) ([]domain.SubscribedTrip, error) {
	var count int64
	if err := p.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", uid).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, domain.ErrUserNotFound
	}
	out := []domain.SubscribedTrip{}
	err := p.db.WithContext(ctx).Model(&TripInfoModel{}).
		Select("trips.id, trips.name, trip_subscriptions.last_viewed_at").
		Joins("JOIN trip_subscriptions ON trip_subscriptions.trip_id = trips.id").Where("trip_subscriptions.user_id = ?", uid).
		Order("trips.id").Scan(&out).Error
	return out, err
}
func (p *pgAuthDBWrapper) SubscribeTrip(ctx context.Context, uid, tid uuid.UUID) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccess(tx, ctx, tid, tripShared); err != nil {
			return err
		}
		return subscriptionError(tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TripSubscriptionModel{UserID: uid, TripID: tid}).Error)
	})
}
func (p *pgAuthDBWrapper) UnsubscribeTrip(ctx context.Context, uid, tid uuid.UUID) error {
	var count int64
	if err := p.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", uid).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrUserNotFound
	}
	return p.db.WithContext(ctx).Where("user_id = ? AND trip_id = ?", uid, tid).Delete(&TripSubscriptionModel{}).Error
}
func (p *pgAuthDBWrapper) CreateSubscribedTrip(ctx context.Context, uid uuid.UUID, info *domain.TripInfo) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store := &pgTripDBWrapper{db: tx}
		if err := store.CreateTrip(info); err != nil {
			return err
		}
		return (&pgAuthDBWrapper{db: tx}).SubscribeTrip(ctx, uid, info.ID)
	})
}

func (p *pgAuthDBWrapper) TouchSubscribedTrip(ctx context.Context, uid, tid uuid.UUID) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccess(tx, ctx, tid, tripShared); err != nil {
			return err
		}
		result := tx.Model(&TripSubscriptionModel{}).
			Where("user_id = ? AND trip_id = ?", uid, tid).
			Update("last_viewed_at", gorm.Expr("GREATEST(last_viewed_at, CURRENT_TIMESTAMP)"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrSubscriptionNotFound
		}
		return nil
	})
}

func userDomain(u UserModel) domain.User {
	return domain.User{ID: u.ID, GCID: u.GCID, Name: u.Name, Email: u.Email, SubMail: u.SubMail}
}
func (p *pgAuthDBWrapper) GetUser(ctx context.Context, id uuid.UUID) (domain.User, error) {
	var u UserModel
	err := p.db.WithContext(ctx).Where("id = ?", id).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = domain.ErrUserNotFound
	}
	return userDomain(u), err
}
func (p *pgAuthDBWrapper) UpdateUser(ctx context.Context, id uuid.UUID, name *string, sub *bool) (out domain.User, err error) {
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u UserModel
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&u).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return domain.ErrUserNotFound
			}
			return e
		}
		if e := utils.ValidateProfile(name, sub, u.Email); e != nil {
			return e
		}
		updates := map[string]interface{}{}
		if name != nil {
			u.Name = name
			updates["name"] = *name
		}
		if sub != nil {
			u.SubMail = *sub
			updates["sub_mail"] = *sub
		}
		if len(updates) > 0 {
			if e := tx.Model(&u).Updates(updates).Error; e != nil {
				return e
			}
		}
		out = userDomain(u)
		return nil
	})
	return
}

// Lock the trip before checking access or locking addresses/record chains.
// Select the final mode up front: upgrading SHARE can deadlock with other writers.
type tripLock string

const (
	tripShared    tripLock = "SHARE"
	tripExclusive tripLock = "UPDATE"
)

func lockTrip(tx *gorm.DB, id uuid.UUID, mode tripLock) (*TripInfoModel, error) {
	var info TripInfoModel
	err := tx.Clauses(clause.Locking{Strength: string(mode)}).Where("id = ?", id).Take(&info).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = domain.ErrTripNotFound
	}
	return &info, err
}
func checkAccess(tx *gorm.DB, ctx context.Context, info *TripInfoModel) error {
	if !info.Private {
		return nil
	}
	uid, ok := domain.UserIDFromContext(ctx)
	if !ok {
		return domain.ErrUnauthenticated
	}
	var count int64
	if err := tx.Model(&TripBindingModel{}).Where("trip_id = ? AND user_id = ?", info.ID, uid).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrForbidden
	}
	return nil
}
func lockAccess(tx *gorm.DB, ctx context.Context, id uuid.UUID, mode tripLock) error {
	info, err := lockTrip(tx, id, mode)
	if err != nil {
		return err
	}
	return checkAccess(tx, ctx, info)
}
func bindingUser(ctx context.Context) (uuid.UUID, error) {
	uid, ok := domain.UserIDFromContext(ctx)
	if !ok {
		return uuid.Nil, domain.ErrUnauthenticated
	}
	return uid, ctx.Err()
}
func (p *pgTripDBWrapper) CheckTripAccess(ctx context.Context, id uuid.UUID) error {
	var info TripInfoModel
	tx := p.db.WithContext(ctx)
	if err := tx.Where("id = ?", id).Take(&info).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrTripNotFound
		}
		return err
	}
	return checkAccess(tx, ctx, &info)
}
func (p *pgTripDBWrapper) Binding(ctx context.Context, id uuid.UUID) (*domain.TripBinding, error) {
	uid, err := bindingUser(ctx)
	if err != nil {
		return nil, err
	}
	var row TripBindingModel
	tx := p.db.WithContext(ctx)
	err = tx.Where("trip_id = ? AND user_id = ?", id, uid).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &domain.TripBinding{TripID: id}
	if row.AddressID != nil {
		var a AddressModel
		if err := tx.Where("trip_id = ? AND id = ?", id, *row.AddressID).Take(&a).Error; err != nil {
			return nil, err
		}
		out.Address, err = displayAddress(tx, id, a.ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (p *pgTripDBWrapper) Bind(ctx context.Context, id uuid.UUID, aid *uuid.UUID) error {
	uid, err := bindingUser(ctx)
	if err != nil {
		return err
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccess(tx, ctx, id, tripShared); err != nil {
			return err
		}
		if aid != nil {
			var address AddressModel
			if err := tx.Clauses(clause.Locking{Strength: "KEY SHARE"}).Where("trip_id = ? AND id = ?", id, *aid).Take(&address).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domain.ErrBadUserInput
				}
				return err
			}
			var count int64
			if err := tx.Model(&TripBindingModel{}).Where("trip_id = ? AND address_id = ? AND user_id <> ?", id, *aid, uid).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return domain.ErrConflict
			}
		}
		return bindingError(tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "trip_id"}}, DoUpdates: clause.AssignmentColumns([]string{"address_id"})}).Create(&TripBindingModel{UserID: uid, TripID: id, AddressID: aid}).Error)
	})
}
func (p *pgTripDBWrapper) Unbind(ctx context.Context, id uuid.UUID) error {
	uid, err := bindingUser(ctx)
	if err != nil {
		return err
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		info, err := lockTrip(tx, id, tripExclusive)
		if errors.Is(err, domain.ErrTripNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var own int64
		if err := tx.Model(&TripBindingModel{}).Where("trip_id = ? AND user_id = ?", id, uid).Count(&own).Error; err != nil {
			return err
		}
		if own == 0 {
			return nil
		}
		if info.Private {
			var count int64
			if err := tx.Model(&TripBindingModel{}).Where("trip_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			if count == 1 {
				return domain.ErrConflict
			}
		}
		return tx.Where("trip_id = ? AND user_id = ?", id, uid).Delete(&TripBindingModel{}).Error
	})
}
func (p *pgTripDBWrapper) SetTripPrivate(ctx context.Context, id uuid.UUID, private bool) (*domain.TripInfo, error) {
	return p.PatchTripInfo(ctx, id, nil, &private)
}

func (p *pgTripDBWrapper) PatchTripInfo(ctx context.Context, id uuid.UUID, name *string, private *bool) (out *domain.TripInfo, err error) {
	if name != nil {
		if err := textvalidate.ValidateName(*name); err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrBadUserInput, err)
		}
	}
	err = p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		info, err := lockTrip(tx, id, tripExclusive)
		if err != nil {
			return err
		}
		if err := checkAccess(tx, ctx, info); err != nil {
			return err
		}
		updates := map[string]interface{}{}
		if private != nil {
			uid, err := bindingUser(ctx)
			if err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&TripBindingModel{}).Where("trip_id = ? AND user_id = ?", id, uid).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return domain.ErrForbidden
			}
			updates["private"] = *private
			info.Private = *private
		}
		if name != nil {
			updates["name"] = *name
			info.Name = *name
		}
		if len(updates) > 0 {
			if err := tx.Model(info).Updates(updates).Error; err != nil {
				return err
			}
		}
		out = &domain.TripInfo{ID: id, Name: info.Name, Private: info.Private}
		return nil
	})
	return
}

// Batch authorization keeps loader work proportional to batches, not trip count.
// For read performance, loaders intentionally run this check and their data query
// as separate statements without a shared snapshot or lock. If a public trip
// becomes private after this check, an in-flight read may still return data
// committed after the privacy change, including to anonymous callers. This is an
// accepted consistency tradeoff for record, address, and trip-info loaders;
// privacy changes do not strictly revoke reads already in progress.
func (p *pgTripDBWrapper) batchAccess(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	uid, loggedIn := domain.UserIDFromContext(ctx)
	var denied []uuid.UUID
	err := p.db.WithContext(ctx).Model(&TripInfoModel{}).Where("id IN ? AND private AND NOT EXISTS (SELECT 1 FROM trip_bindings b WHERE b.trip_id = trips.id AND b.user_id = ?)", ids, uid).Pluck("id", &denied).Error
	if err != nil {
		return err
	}
	if len(denied) > 0 {
		if !loggedIn {
			return domain.ErrUnauthenticated
		}
		return domain.ErrForbidden
	}
	return nil
}
