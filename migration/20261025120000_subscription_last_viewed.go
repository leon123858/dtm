package migrations

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
)

func init() { goose.AddMigrationContext(upSubscriptionLastViewed, downSubscriptionLastViewed) }
func upSubscriptionLastViewed(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE trip_subscriptions ADD COLUMN last_viewed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP`)
	return err
}
func downSubscriptionLastViewed(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE trip_subscriptions DROP COLUMN last_viewed_at`)
	return err
}
