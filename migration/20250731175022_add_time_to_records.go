package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAddTimeToRecordsWithExistingCreatedAt, downAddTimeToRecordsWithExistingCreatedAt)
}

func upAddTimeToRecordsWithExistingCreatedAt(ctx context.Context, tx *sql.Tx) error {
	// Step 1: Add 'time' column, initially allowing NULL
	// This will add the column without populating existing rows with a default value
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE records
		ADD COLUMN time TIMESTAMPTZ;
	`)
	if err != nil {
		return err
	}

	// Step 2: Update existing rows to set 'time' equal to 'created_at'
	_, err = tx.ExecContext(ctx, `
		UPDATE records
		SET time = created_at;
	`)
	if err != nil {
		return err
	}

	// Step 3 (Optional but recommended): Add NOT NULL constraint and a default for future inserts
	// This ensures new records will have a 'time' value and makes the column non-nullable
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE records
		ALTER COLUMN time SET NOT NULL,
		ALTER COLUMN time SET DEFAULT CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return err
	}

	return nil
}

func downAddTimeToRecordsWithExistingCreatedAt(ctx context.Context, tx *sql.Tx) error {
	// Remove 'time' column from records table
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE records
		DROP COLUMN IF EXISTS time;
	`)
	if err != nil {
		return err
	}
	return nil
}
