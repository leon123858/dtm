package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddressEntities, downAddressEntities)
}

func upAddressEntities(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE addresses (
			id UUID PRIMARY KEY,
			trip_id UUID NOT NULL,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT uq_addresses_trip_name UNIQUE (trip_id, name),
			CONSTRAINT uq_addresses_trip_id UNIQUE (trip_id, id),
			CONSTRAINT fk_addresses_trip FOREIGN KEY (trip_id)
				REFERENCES trips(id) ON UPDATE CASCADE
		);
		ALTER TABLE records ADD COLUMN pre_pay_address_id UUID;
		ALTER TABLE record_should_pay_address_lists ADD COLUMN address_id UUID;
	`); err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, `SELECT trip_id, address, created_at, updated_at FROM trip_address_lists`)
	if err != nil {
		return err
	}
	type legacyAddress struct {
		tripID, id uuid.UUID
		name       string
		createdAt  any
		updatedAt  any
	}
	var addresses []legacyAddress
	for rows.Next() {
		var a legacyAddress
		if err := rows.Scan(&a.tripID, &a.name, &a.createdAt, &a.updatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		a.id = uuid.New()
		addresses = append(addresses, a)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, a := range addresses {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO addresses (id, trip_id, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
			a.id, a.tripID, a.name, a.createdAt, a.updatedAt); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE records r SET pre_pay_address_id = a.id
		FROM addresses a
		WHERE a.trip_id = r.trip_id AND a.name = r.pre_pay_address;

		UPDATE record_should_pay_address_lists rsp SET address_id = a.id
		FROM addresses a
		WHERE a.trip_id = rsp.trip_id AND a.name = rsp.address;
	`); err != nil {
		return err
	}

	var missing int
	if err := tx.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM records WHERE pre_pay_address_id IS NULL)
		     + (SELECT count(*) FROM record_should_pay_address_lists WHERE address_id IS NULL)
	`).Scan(&missing); err != nil {
		return err
	}
	if missing != 0 {
		return fmt.Errorf("address UUID backfill left %d unresolved references", missing)
	}

	_, err = tx.ExecContext(ctx, `
		ALTER TABLE records
			DROP CONSTRAINT fk_records_trip_address,
			ALTER COLUMN pre_pay_address_id SET NOT NULL,
			DROP COLUMN pre_pay_address,
			ADD CONSTRAINT fk_records_trip_address
				FOREIGN KEY (trip_id, pre_pay_address_id)
				REFERENCES addresses(trip_id, id) ON UPDATE CASCADE;
		DROP INDEX IF EXISTS idx_records_trip_id_pre_pay_address;
		CREATE INDEX idx_records_trip_id_pre_pay_address_id ON records(trip_id, pre_pay_address_id);

		ALTER TABLE records ADD CONSTRAINT uq_records_trip_id UNIQUE (trip_id, id);
		ALTER TABLE record_should_pay_address_lists
			DROP CONSTRAINT fk_rspl_record,
			DROP CONSTRAINT fk_rspl_trip_address,
			DROP CONSTRAINT record_should_pay_address_lists_pkey,
			ALTER COLUMN address_id SET NOT NULL,
			DROP COLUMN address,
			ADD CONSTRAINT record_should_pay_address_lists_pkey PRIMARY KEY (record_id, address_id),
			ADD CONSTRAINT fk_rspl_record FOREIGN KEY (trip_id, record_id)
				REFERENCES records(trip_id, id) ON UPDATE CASCADE,
			ADD CONSTRAINT fk_rspl_trip_address FOREIGN KEY (trip_id, address_id)
				REFERENCES addresses(trip_id, id) ON UPDATE CASCADE;
		DROP INDEX IF EXISTS idx_rspl_trip_id_address;
		CREATE INDEX idx_rspl_trip_id_address_id ON record_should_pay_address_lists(trip_id, address_id);

		DROP TABLE trip_address_lists;
	`)
	return err
}

func downAddressEntities(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE trip_address_lists (
			trip_id UUID NOT NULL,
			address VARCHAR(255) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (trip_id, address),
			CONSTRAINT fk_trip_address_lists_trip FOREIGN KEY (trip_id)
				REFERENCES trips(id) ON UPDATE CASCADE
		);
		INSERT INTO trip_address_lists (trip_id, address, created_at, updated_at)
		SELECT trip_id, name, created_at, updated_at FROM addresses;

		ALTER TABLE records ADD COLUMN pre_pay_address VARCHAR(255);
		UPDATE records r SET pre_pay_address = a.name
		FROM addresses a WHERE a.id = r.pre_pay_address_id AND a.trip_id = r.trip_id;

		ALTER TABLE record_should_pay_address_lists ADD COLUMN address VARCHAR(255);
		UPDATE record_should_pay_address_lists rsp SET address = a.name
		FROM addresses a WHERE a.id = rsp.address_id AND a.trip_id = rsp.trip_id;

		ALTER TABLE record_should_pay_address_lists
			DROP CONSTRAINT fk_rspl_record,
			DROP CONSTRAINT fk_rspl_trip_address,
			DROP CONSTRAINT record_should_pay_address_lists_pkey,
			ALTER COLUMN address SET NOT NULL,
			DROP COLUMN address_id,
			ADD CONSTRAINT record_should_pay_address_lists_pkey PRIMARY KEY (record_id, trip_id, address),
			ADD CONSTRAINT fk_rspl_record FOREIGN KEY (record_id)
				REFERENCES records(id) ON UPDATE CASCADE,
			ADD CONSTRAINT fk_rspl_trip_address FOREIGN KEY (trip_id, address)
				REFERENCES trip_address_lists(trip_id, address) ON UPDATE CASCADE;
		DROP INDEX IF EXISTS idx_rspl_trip_id_address_id;
		CREATE INDEX idx_rspl_trip_id_address ON record_should_pay_address_lists(trip_id, address);

		ALTER TABLE records
			DROP CONSTRAINT fk_records_trip_address,
			ALTER COLUMN pre_pay_address SET NOT NULL,
			DROP COLUMN pre_pay_address_id,
			ADD CONSTRAINT fk_records_trip_address FOREIGN KEY (trip_id, pre_pay_address)
				REFERENCES trip_address_lists(trip_id, address) ON UPDATE CASCADE,
			DROP CONSTRAINT uq_records_trip_id;
		DROP INDEX IF EXISTS idx_records_trip_id_pre_pay_address_id;
		CREATE INDEX idx_records_trip_id_pre_pay_address ON records(trip_id, pre_pay_address);

		DROP TABLE addresses;
	`)
	return err
}
