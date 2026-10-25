package pg

import (
	"context"
	"time"

	"dtm/adapters/db/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type pgAuthDBWrapper struct{ db *gorm.DB }

// NewPgAuthDBWrapper shares the supplied connection pool, but never logs auth queries.
func NewPgAuthDBWrapper(conn *gorm.DB) db.AuthDBWrapper {
	return &pgAuthDBWrapper{db: conn.Session(&gorm.Session{Logger: logger.Discard})}
}

func (p *pgAuthDBWrapper) PutLock(ctx context.Context, kind, key, binding string, data []byte, expires time.Time) error {
	return p.db.WithContext(ctx).Create(&AuthEntryModel{
		Kind: kind, KeyHash: key, BindingHash: binding, Payload: data, ExpiresAt: expires,
	}).Error
}

func (p *pgAuthDBWrapper) TakeLock(ctx context.Context, kind, key, binding string, now time.Time) ([]byte, error) {
	var entry AuthEntryModel
	result := p.db.WithContext(ctx).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "payload"}}}).
		Where("kind = ? AND key_hash = ? AND binding_hash = ? AND expires_at > ?", kind, key, binding, now).
		Delete(&entry)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, db.ErrInvalidAuthEntry
	}
	return entry.Payload, nil
}

func (p *pgAuthDBWrapper) CleanupLock(ctx context.Context, now time.Time) error {
	return p.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&AuthEntryModel{}).Error
}
