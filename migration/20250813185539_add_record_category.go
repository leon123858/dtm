package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddRecordCategory, downAddRecordCategory)
}

func upAddRecordCategory(ctx context.Context, tx *sql.Tx) error {
	// Add 'category' column to 'records' table
	// We'll add it, then update existing rows to a default value (e.g., 0),
	// and finally add the NOT NULL constraint.
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE records
		ADD COLUMN category INTEGER;
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE records
		SET category = 0;
	`)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		ALTER TABLE records
		ALTER COLUMN category SET NOT NULL;
	`)
	if err != nil {
		return err
	}

	// Add 'extended_msg' column to 'record_should_pay_address_lists' table
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE record_should_pay_address_lists
		ADD COLUMN extended_msg NUMERIC(10, 2);
	`)
	if err != nil {
		return err
	}

	return nil
}

func downAddRecordCategory(ctx context.Context, tx *sql.Tx) error {
	// Remove 'category' column from 'records' table
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE records
		DROP COLUMN IF EXISTS category;
	`)
	if err != nil {
		return err
	}

	// Remove 'extended_msg' column from 'record_should_pay_address_lists' table
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE record_should_pay_address_lists
		DROP COLUMN IF EXISTS extended_msg;
	`)
	if err != nil {
		return err
	}

	return nil
}
