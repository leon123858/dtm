package pg

import (
	"context"
	"dtm/adapters/db/db"
	"dtm/domain"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ db.AuthDBWrapper = (*pgAuthDBWrapper)(nil)

func (p *pgAuthDBWrapper) UpsertUser(ctx context.Context, gcid string) (domain.User, error) {
	if strings.TrimSpace(gcid) == "" {
		return domain.User{}, db.ErrInvalidGCID
	}
	u := UserModel{ID: uuid.New(), GCID: gcid}
	// when gcid conflict, do not thing and use old uuid as return
	err := p.db.WithContext(ctx).Clauses(
		clause.OnConflict{Columns: []clause.Column{{Name: "gcid"}}, DoUpdates: clause.AssignmentColumns([]string{"gcid"})},
		clause.Returning{},
	).Create(&u).Error
	return domain.User{ID: u.ID, GCID: u.GCID}, err
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
func subscriptionError(err error) error {
	var pqe *pq.Error
	if errors.As(err, &pqe) && pqe.Code == "23503" {
		if strings.Contains(pqe.Constraint, "user_id") {
			return domain.ErrUserNotFound
		}
		return domain.ErrTripNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23503" {
		if strings.Contains(pe.ConstraintName, "user_id") {
			return domain.ErrUserNotFound
		}
		return domain.ErrTripNotFound
	}
	return err
}
func (p *pgAuthDBWrapper) SubscribeTrip(ctx context.Context, uid, tid uuid.UUID) error {
	return subscriptionError(p.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&TripSubscriptionModel{UserID: uid, TripID: tid}).Error)
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
	result := p.db.WithContext(ctx).Model(&TripSubscriptionModel{}).
		Where("user_id = ? AND trip_id = ?", uid, tid).
		Update("last_viewed_at", gorm.Expr("GREATEST(last_viewed_at, CURRENT_TIMESTAMP)"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrSubscriptionNotFound
	}
	return nil
}
