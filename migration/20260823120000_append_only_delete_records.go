package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upAppendOnlyDeleteRecords, downAppendOnlyDeleteRecords)
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

func downAppendOnlyDeleteRecords(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE append_only_active_record_tails (
			trip_id UUID NOT NULL,
			id UUID NOT NULL,
			PRIMARY KEY (trip_id, id)
		) ON COMMIT DROP;

		INSERT INTO append_only_active_record_tails (trip_id, id)
		SELECT trip_id, id
		FROM records
		WHERE child_record_id IS NULL AND is_deleted = FALSE;

		DELETE FROM record_should_pay_address_lists rsp
		WHERE NOT EXISTS (
			SELECT 1
			FROM append_only_active_record_tails tail
			WHERE tail.trip_id = rsp.trip_id AND tail.id = rsp.record_id
		);

		UPDATE records
		SET parent_record_id = NULL, child_record_id = NULL;

		DELETE FROM records r
		WHERE NOT EXISTS (
			SELECT 1
			FROM append_only_active_record_tails tail
			WHERE tail.trip_id = r.trip_id AND tail.id = r.id
		);

		DROP INDEX uq_records_parent_record_id;
		DROP INDEX uq_records_child_record_id;
		ALTER TABLE records
			DROP CONSTRAINT fk_records_parent,
			DROP CONSTRAINT fk_records_child,
			DROP CONSTRAINT chk_records_not_self_linked,
			DROP CONSTRAINT chk_records_child_not_self_linked,
			DROP COLUMN parent_record_id,
			DROP COLUMN child_record_id,
			DROP COLUMN is_deleted;
	`)
	return err
}
