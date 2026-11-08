-- «События в календаре при смене срока» and the deadline events to update in Google (TASK-276, ADR-0029 addendum 2
-- «Deadline change and the Google event»).
--
-- The statements between the goose Up and Down markers are mirrored VERBATIM at the end of docs/contracts/schema.sql
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- settings: what a deadline change does to the Google event (settings.CalendarOnDueChange): 'ask' (the default, also
-- of the existing row), 'update' or 'keep'.
ALTER TABLE settings ADD COLUMN deadline_event_on_due_change TEXT NOT NULL DEFAULT 'ask' CHECK (deadline_event_on_due_change IN ('ask', 'update', 'keep'));
-- deadline_event_updates: the assignments whose Google deadline event is to follow a changed deadline. Written in the
-- transaction of the change; the calendar sync reads the assignment again, upserts the event and then removes the row.
-- No foreign key and no version column: a queue as deadline_event_removals, rows are only inserted and removed.
CREATE TABLE deadline_event_updates (
    assignment_id TEXT    PRIMARY KEY CHECK (length(assignment_id) > 0),
    created_at    INTEGER NOT NULL
);

-- +goose Down
DROP TABLE deadline_event_updates;
ALTER TABLE settings DROP COLUMN deadline_event_on_due_change;
