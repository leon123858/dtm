package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upInitTables, downInitTables)
}

func upInitTables(ctx context.Context, tx *sql.Tx) error {
	// Create trips table
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE trips (
			id UUID PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}

	// Create trip_address_lists table
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE trip_address_lists (
			trip_id UUID NOT NULL,
			address VARCHAR(255) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (trip_id, address),
			CONSTRAINT fk_trip_address_lists_trip
				FOREIGN KEY(trip_id)
				REFERENCES trips(id)
				ON UPDATE CASCADE
				ON DELETE CASCADE
		);
	`)
	if err != nil {
		return err
	}

	// Create records table
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE records (
			id UUID PRIMARY KEY,
			trip_id UUID NOT NULL,
			name VARCHAR(255) NOT NULL,
			amount NUMERIC(10,2) NOT NULL,
			pre_pay_address VARCHAR(255) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT fk_records_trip
				FOREIGN KEY(trip_id)
				REFERENCES trips(id)
				ON UPDATE CASCADE
				ON DELETE CASCADE,
			CONSTRAINT fk_records_trip_address
				FOREIGN KEY(trip_id, pre_pay_address)
				REFERENCES trip_address_lists(trip_id, address)
				ON UPDATE CASCADE
				ON DELETE CASCADE
		);
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `CREATE INDEX idx_records_trip_id ON records(trip_id);`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `CREATE INDEX idx_records_trip_id_pre_pay_address ON records(trip_id, pre_pay_address);`)
	if err != nil {
		return err
	}

	// Create record_should_pay_address_lists table
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE record_should_pay_address_lists (
			record_id UUID NOT NULL,
			trip_id UUID NOT NULL,
			address VARCHAR(255) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (record_id, trip_id, address),
			CONSTRAINT fk_rspl_record
				FOREIGN KEY(record_id)
				REFERENCES records(id)
				ON UPDATE CASCADE
				ON DELETE CASCADE,
			CONSTRAINT fk_rspl_trip_address
				FOREIGN KEY(trip_id, address)
				REFERENCES trip_address_lists(trip_id, address)
				ON UPDATE CASCADE
				ON DELETE CASCADE
		);
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `CREATE INDEX idx_rspl_record_id ON record_should_pay_address_lists(record_id);`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `CREATE INDEX idx_rspl_trip_id_address ON record_should_pay_address_lists(trip_id, address);`)
	if err != nil {
		return err
	}

	return nil
}

func downInitTables(ctx context.Context, tx *sql.Tx) error {
	// Drop record_should_pay_address_lists table (and its indexes)
	// Indexes are typically dropped automatically when the table is dropped,
	// but explicit dropping is safer if they were created separately or if a FK constraint might depend on them.
	// However, for simple named indexes on the table being dropped, explicit DROP INDEX is not strictly necessary before DROP TABLE.
	// Foreign key constraints ensure correct drop order or will fail.
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS record_should_pay_address_lists;`)
	if err != nil {
		return err
	}

	// Drop records table (and its indexes)
	_, err = tx.ExecContext(ctx, `DROP TABLE IF EXISTS records;`)
	if err != nil {
		return err
	}

	// Drop trip_address_lists table
	_, err = tx.ExecContext(ctx, `DROP TABLE IF EXISTS trip_address_lists;`)
	if err != nil {
		return err
	}

	// Drop trips table
	_, err = tx.ExecContext(ctx, `DROP TABLE IF EXISTS trips;`)
	if err != nil {
		return err
	}

	return nil
}
