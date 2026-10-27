package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() { goose.AddMigrationContext(upAuthFeature, downAuthFeature) }

func upAuthFeature(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE auth_entries (
    kind TEXT NOT NULL CHECK (kind IN ('transaction','login')),
    key_hash TEXT NOT NULL,
    binding_hash TEXT NOT NULL,
    payload BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (kind,key_hash)
);
CREATE INDEX auth_entries_expiry ON auth_entries(expires_at);

CREATE TABLE users (
    id UUID PRIMARY KEY,
    gcid TEXT NOT NULL UNIQUE CHECK (btrim(gcid) <> ''),
    name TEXT,
    email TEXT,
    sub_mail BOOLEAN NOT NULL DEFAULT FALSE
);

ALTER TABLE trips ADD COLUMN private BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE addresses ADD COLUMN use_nickname BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE trip_subscriptions (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    last_viewed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id,trip_id)
);
CREATE INDEX trip_subscriptions_trip_id ON trip_subscriptions(trip_id);

CREATE TABLE trip_bindings (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    address_id UUID,
    PRIMARY KEY (user_id,trip_id),
    FOREIGN KEY (trip_id,address_id) REFERENCES addresses(trip_id,id) ON DELETE NO ACTION
);
CREATE UNIQUE INDEX trip_bindings_address ON trip_bindings(trip_id,address_id) WHERE address_id IS NOT NULL;
CREATE INDEX trip_bindings_trip ON trip_bindings(trip_id);
`)
	return err
}

func downAuthFeature(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
DROP TABLE trip_bindings;
DROP TABLE trip_subscriptions;
ALTER TABLE addresses DROP COLUMN use_nickname;
ALTER TABLE trips DROP COLUMN private;
DROP TABLE users;
DROP TABLE auth_entries;
`)
	return err
}
