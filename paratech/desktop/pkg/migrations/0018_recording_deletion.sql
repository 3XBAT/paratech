-- Deleting a recording (TASK-095, ADR-0018): the deletion mark and the deadline events left to remove from Google.
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- recordings: the deletion mark (recording.Recording.MarkDeleting), set before any file or row is removed, and the
-- found assignments to delete with the recording (a JSON array of ids). A marked recording is hidden from the lists
-- and its deletion is resumed at the next start. NULL: not being deleted.
ALTER TABLE recordings ADD COLUMN deleting_at INTEGER;
ALTER TABLE recordings ADD COLUMN deleting_assignments TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(deleting_assignments));

-- ---------------------------------------------------------------------------------------------------------------
-- deadline_event_removals: the assignments deleted with a recording whose Google deadline event (if any) is still to
-- be deleted. Written in the transaction that deletes the assignment; the calendar sync deletes the event (gone is
-- success) and then the row. No foreign key: the assignment is gone. No version column: rows are only inserted and
-- removed.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE deadline_event_removals (
    assignment_id TEXT    PRIMARY KEY CHECK (length(assignment_id) > 0),
    created_at    INTEGER NOT NULL
);

-- +goose Down
DROP TABLE deadline_event_removals;
ALTER TABLE recordings DROP COLUMN deleting_assignments;
ALTER TABLE recordings DROP COLUMN deleting_at;
