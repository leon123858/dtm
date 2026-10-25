package migrations

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
)

func init() { goose.AddMigrationContext(upAuthFeature, downAuthFeature) }
func upAuthFeature(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE auth_entries (
 kind TEXT NOT NULL CHECK (kind IN ('transaction','login')),
 key_hash TEXT NOT NULL,
 binding_hash TEXT NOT NULL,
 payload BYTEA NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (kind,key_hash));
 CREATE INDEX auth_entries_expiry ON auth_entries(expires_at);
CREATE TABLE users (
 id UUID PRIMARY KEY,
 gcid TEXT NOT NULL UNIQUE CHECK (btrim(gcid) <> '')
 );
 CREATE TABLE trip_subscriptions (
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
 last_viewed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY (user_id,trip_id));
 CREATE INDEX trip_subscriptions_trip_id ON trip_subscriptions(trip_id);`)
	return err
}
func downAuthFeature(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE trip_subscriptions; DROP TABLE users; DROP TABLE auth_entries;`)
	return err
}
