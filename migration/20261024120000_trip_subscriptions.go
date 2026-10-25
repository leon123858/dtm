package migrations

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
)

func init() { goose.AddMigrationContext(upTripSubscriptions, downTripSubscriptions) }
func upTripSubscriptions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE users (
 id UUID PRIMARY KEY,
 gcid TEXT NOT NULL UNIQUE CHECK (btrim(gcid) <> '')
 );
 CREATE TABLE trip_subscriptions (
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
 PRIMARY KEY (user_id,trip_id));
 CREATE INDEX trip_subscriptions_trip_id ON trip_subscriptions(trip_id);`)
	return err
}
func downTripSubscriptions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE trip_subscriptions; DROP TABLE users;`)
	return err
}
