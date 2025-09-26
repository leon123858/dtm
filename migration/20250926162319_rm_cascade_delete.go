package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upChangeDeleteCascadeToRestrict, downChangeDeleteCascadeToRestrict)
}

func upChangeDeleteCascadeToRestrict(ctx context.Context, tx *sql.Tx) error {
	// Table: trip_address_lists
	// Remove old constraint and add new one without ON DELETE CASCADE
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE trip_address_lists
		DROP CONSTRAINT fk_trip_address_lists_trip,
		ADD CONSTRAINT fk_trip_address_lists_trip
			FOREIGN KEY(trip_id)
			REFERENCES trips(id)
			ON UPDATE CASCADE;
	`)
	if err != nil {
		return err
	}

	// Table: records
	// Remove old constraints and add new ones without ON DELETE CASCADE
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE records
		DROP CONSTRAINT fk_records_trip,
		ADD CONSTRAINT fk_records_trip
			FOREIGN KEY(trip_id)
			REFERENCES trips(id)
			ON UPDATE CASCADE;
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		ALTER TABLE records
		DROP CONSTRAINT fk_records_trip_address,
		ADD CONSTRAINT fk_records_trip_address
			FOREIGN KEY(trip_id, pre_pay_address)
			REFERENCES trip_address_lists(trip_id, address)
			ON UPDATE CASCADE;
	`)
	if err != nil {
		return err
	}

	// Table: record_should_pay_address_lists
	// Remove old constraints and add new ones without ON DELETE CASCADE
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE record_should_pay_address_lists
		DROP CONSTRAINT fk_rspl_record,
		ADD CONSTRAINT fk_rspl_record
			FOREIGN KEY(record_id)
			REFERENCES records(id)
			ON UPDATE CASCADE;
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		ALTER TABLE record_should_pay_address_lists
		DROP CONSTRAINT fk_rspl_trip_address,
		ADD CONSTRAINT fk_rspl_trip_address
			FOREIGN KEY(trip_id, address)
			REFERENCES trip_address_lists(trip_id, address)
			ON UPDATE CASCADE;
	`)
	if err != nil {
		return err
	}

	return nil
}

func downChangeDeleteCascadeToRestrict(ctx context.Context, tx *sql.Tx) error {
	// Revert changes for: trip_address_lists
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE trip_address_lists
		DROP CONSTRAINT fk_trip_address_lists_trip,
		ADD CONSTRAINT fk_trip_address_lists_trip
			FOREIGN KEY(trip_id)
			REFERENCES trips(id)
			ON UPDATE CASCADE
			ON DELETE CASCADE;
	`)
	if err != nil {
		return err
	}

	// Revert changes for: records
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE records
		DROP CONSTRAINT fk_records_trip,
		ADD CONSTRAINT fk_records_trip
			FOREIGN KEY(trip_id)
			REFERENCES trips(id)
			ON UPDATE CASCADE
			ON DELETE CASCADE;
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		ALTER TABLE records
		DROP CONSTRAINT fk_records_trip_address,
		ADD CONSTRAINT fk_records_trip_address
			FOREIGN KEY(trip_id, pre_pay_address)
			REFERENCES trip_address_lists(trip_id, address)
			ON UPDATE CASCADE
			ON DELETE CASCADE;
	`)
	if err != nil {
		return err
	}

	// Revert changes for: record_should_pay_address_lists
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE record_should_pay_address_lists
		DROP CONSTRAINT fk_rspl_record,
		ADD CONSTRAINT fk_rspl_record
			FOREIGN KEY(record_id)
			REFERENCES records(id)
			ON UPDATE CASCADE
			ON DELETE CASCADE;
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		ALTER TABLE record_should_pay_address_lists
		DROP CONSTRAINT fk_rspl_trip_address,
		ADD CONSTRAINT fk_rspl_trip_address
			FOREIGN KEY(trip_id, address)
			REFERENCES trip_address_lists(trip_id, address)
			ON UPDATE CASCADE
			ON DELETE CASCADE;
	`)
	if err != nil {
		return err
	}

	return nil
}
