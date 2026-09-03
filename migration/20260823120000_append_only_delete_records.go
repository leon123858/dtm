package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAppendOnlyDeleteRecords, nil)
}

func upAppendOnlyDeleteRecords(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		ALTER TABLE records
			ADD COLUMN parent_record_id UUID,
			ADD COLUMN child_record_id UUID,
			ADD COLUMN is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
			ADD CONSTRAINT fk_records_parent
				FOREIGN KEY (trip_id, parent_record_id)
				REFERENCES records(trip_id, id) ON UPDATE CASCADE DEFERRABLE INITIALLY DEFERRED,
			ADD CONSTRAINT fk_records_child
				FOREIGN KEY (trip_id, child_record_id)
				REFERENCES records(trip_id, id) ON UPDATE CASCADE DEFERRABLE INITIALLY DEFERRED,
			ADD CONSTRAINT chk_records_not_self_linked
				CHECK (parent_record_id IS NULL OR parent_record_id <> id),
			ADD CONSTRAINT chk_records_child_not_self_linked
				CHECK (child_record_id IS NULL OR child_record_id <> id);
		CREATE UNIQUE INDEX uq_records_parent_record_id ON records(trip_id, parent_record_id) WHERE parent_record_id IS NOT NULL;
		CREATE UNIQUE INDEX uq_records_child_record_id ON records(trip_id, child_record_id) WHERE child_record_id IS NOT NULL;
	`)
	return err
}
