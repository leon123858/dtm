package migrations

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
)

func init() { goose.AddMigrationContext(upAuthEntries, downAuthEntries) }
func upAuthEntries(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE auth_entries (
 kind TEXT NOT NULL CHECK (kind IN ('transaction','login')),
 key_hash TEXT NOT NULL,
 binding_hash TEXT NOT NULL,
 payload BYTEA NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (kind,key_hash));
 CREATE INDEX auth_entries_expiry ON auth_entries(expires_at);`)
	return err
}
func downAuthEntries(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE auth_entries`)
	return err
}
