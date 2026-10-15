-- Learning Assistant desktop: local SQLite schema DRAFT (TASK-003).
--
-- Consumed by TASK-005: goose migrations in desktop/pkg/migrations (ARCH-04, ADR-0006) and sqlc queries are
-- written from this file. This is a draft of the contract, not a migration: it has no goose annotations.
-- Architecture: docs/architecture.md (aggregates map to tables, ARCH-13 references are plain id columns).
-- Source of requirements: docs/prd/learning-assistant-prd-v0.4.md.
--
-- Conventions
--   * SQLite dialect, pure Go driver modernc.org/sqlite. The adapter enables PRAGMA foreign_keys = ON,
--     journal_mode = WAL and a busy timeout on every connection (TASK-005).
--   * ids are opaque TEXT generated through the IDGenerator ports, never by the database.
--   * Timestamps are INTEGER Unix epoch milliseconds, UTC. Lesson times keep their source IANA zone in a
--     separate column (PRD section 11, TASK-018).
--   * Booleans are INTEGER 0/1 with a CHECK. JSON is TEXT guarded by json_valid().
--   * *_ref columns hold a shared.ArtifactRef in its stored form "<kind>/<path>" (recordings/, transcripts/,
--     models/, exports/ below the private data root). Files never live in SQLite (PRD section 5.2).
--   * Optimistic concurrency (ADR-0004(d)): every table whose aggregate has a Save port carries
--     `version INTEGER NOT NULL DEFAULT 0`. A never-stored aggregate has Version() 0 and is INSERTed by the adapter with
--     version 1 (an existing row with that id: shared.ErrVersionConflict). A stored aggregate has Version() >= 1 and Save
--     is a compare-and-swap: UPDATE ... SET version = version + 1 WHERE id = ? AND version = <loaded version>; zero rows
--     changed means the row changed or was purged meanwhile and the adapter returns shared.ErrVersionConflict, it never
--     re-inserts. After a successful write the adapter calls Persisted(newVersion) on the aggregate, so the same instance
--     can be saved again. The version is state of the aggregate (Restore takes it, Version() returns it); the database
--     never changes it except through Save. The DEFAULT 0 is only a safety net: no row is ever stored with version 0.
--   * Foreign keys never change an aggregate behind its version, except the documented clean-up of references that are
--     copies of identity (lesson_id, transcript_id, model_id of transcripts: ON DELETE SET NULL). A reference that a
--     versioned aggregate OWNS as state (settings.default_stt_model_id) is ON DELETE RESTRICT: the use case clears it
--     through the aggregate first. Adapters map a foreign key violation to an error wrapping shared.ErrConflict.
--   * Status columns carry a CHECK that lists exactly the Go constants of the aggregate (job.Statuses,
--     recording.Statuses, delivery.Statuses, notice.Statuses). desktop/tests/schema_test.go keeps both in sync.
--   * NO SECRETS, TOKENS OR PASSWORDS IN THIS SCHEMA (hard rule 4, PRD section 12, AC-19, ARCH-34). Refresh
--     tokens, the Hub API key and mail credentials live in Windows Credential Manager. Do not add columns for them.
--   * Table creation order respects foreign keys.

-- ---------------------------------------------------------------------------------------------------------------
-- stt_models: installed speech-to-text models (PRD section 8, section 8.1; AC-07, AC-08, AC-23).
-- Aggregate transcription/sttmodel. Only INSTALLED models are rows: the catalog is a JSON manifest, partial
-- downloads are .part files below models/. The default model is chosen in settings, not here (ARCH-23).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE stt_models (
    id            TEXT    PRIMARY KEY,
    name          TEXT    NOT NULL CHECK (length(name) > 0),
    model_version TEXT    NOT NULL CHECK (length(model_version) > 0), -- the model's own version; the column `version` is the aggregate version below
    catalog_id    TEXT,                                    -- NULL: imported outside the catalog after explicit confirmation (AC-23)
    file_ref      TEXT    NOT NULL,                        -- models/<file>
    size_bytes    INTEGER NOT NULL CHECK (size_bytes > 0),
    sha256        TEXT    NOT NULL CHECK (length(sha256) = 64), -- lowercase hex, verified before install (section 14 case 13)
    quantization  TEXT,                                    -- e.g. q5_1; informational (section 8.1)
    capabilities  TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(capabilities)), -- e.g. languages (section 8)
    installed_at  INTEGER NOT NULL,
    version       INTEGER NOT NULL DEFAULT 0,              -- optimistic concurrency (ADR-0004)
    UNIQUE (name, model_version)
);

-- ---------------------------------------------------------------------------------------------------------------
-- schedule_entries: the local class schedule, system of record (PRD section 11, section 5.3; AC-16, AC-20).
-- Aggregate schedule/lesson. A recurring ICS rule is stored as expanded occurrences (assumption, TASK-047).
--
-- Soft delete (ADR-0004): removing a lesson from the schedule sets deleted_at; the row stays so that the calendar
-- synchronization can still delete the Google event of a lesson the user no longer has (section 11.3, AC-16).
-- After the event deletion is confirmed, the use case purges the row (LessonRepository.PurgeDeleted). Reads for the
-- UI and the scheduler ignore rows with deleted_at set.
--
-- Calendar projection: the Google event id is DERIVED from the lesson id by the calendar adapter (stable, idempotent
-- upserts, section 11.3 and section 14 case 8), so no event id is stored here. Whether a lesson is out of date in the
-- calendar is decided with two revision counters instead of timestamps (no ties, no clock dependence):
-- content_revision grows on every change of what the calendar shows (including deletion); calendar_synced_revision
-- is the content_revision last written to the calendar. Pending = deleted_at IS NOT NULL (until purged) OR
-- calendar_synced_revision IS NULL OR calendar_synced_revision < content_revision. The lesson aggregate exposes
-- the same predicate (TASK-018); the adapter must agree with it.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE schedule_entries (
    id                        TEXT    PRIMARY KEY,
    title                     TEXT    NOT NULL CHECK (length(title) > 0),
    starts_at                 INTEGER NOT NULL,
    ends_at                   INTEGER NOT NULL,
    timezone                  TEXT    NOT NULL CHECK (length(timezone) > 0), -- IANA name of the source record (TASK-018)
    -- Identity of the occurrence in the imported file, for diffing imports (section 11.2). A recurring rule is
    -- stored as expanded occurrences, so the uid includes the occurrence: "<ICS UID>#<RECURRENCE-ID or DTSTART>".
    -- NULL for manual entries. Unique among live rows only (see the index below).
    source_uid                TEXT,
    start_notified_at         INTEGER,                     -- the "lesson is starting" prompt was raised; prevents duplicates after a restart (section 6.1, TASK-018)
    content_revision          INTEGER NOT NULL DEFAULT 1 CHECK (content_revision >= 1),
    calendar_synced_revision  INTEGER CHECK (calendar_synced_revision IS NULL OR calendar_synced_revision >= 1), -- NULL: never written to the calendar
    calendar_synced_at        INTEGER,                     -- when the last write happened; informational (UI), not used to decide what is pending
    deleted_at                INTEGER,                     -- soft delete, see above; NULL: the lesson is live
    version                   INTEGER NOT NULL DEFAULT 0,  -- optimistic concurrency (ADR-0004)
    created_at                INTEGER NOT NULL,
    updated_at                INTEGER NOT NULL,
    CHECK (ends_at > starts_at),
    CHECK (calendar_synced_revision IS NULL OR calendar_synced_revision <= content_revision)
);
-- A deleted occurrence does not block importing the same occurrence again: the new row gets a new id (and so a new
-- event id) while the old row waits for its event deletion.
CREATE UNIQUE INDEX idx_schedule_entries_source_uid ON schedule_entries (source_uid)
    WHERE source_uid IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_schedule_entries_starts_at ON schedule_entries (starts_at) WHERE deleted_at IS NULL;
-- Entries that still need a calendar write or deletion (section 11.3, controlled synchronization).
CREATE INDEX idx_schedule_entries_calendar_pending ON schedule_entries (starts_at)
    WHERE deleted_at IS NOT NULL OR calendar_synced_revision IS NULL OR calendar_synced_revision < content_revision;

-- ---------------------------------------------------------------------------------------------------------------
-- recordings: one capture of system audio (PRD section 6.2, 6.3; AC-03, AC-04, AC-05, AC-24).
-- Aggregate recording/recording. Status values = recording.Statuses (PRD section 6.2).
-- Chunk files are discovered by listing chunks_ref; only their count is kept (the aggregate owns chunk rules).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE recordings (
    id              TEXT    PRIMARY KEY,
    lesson_id       TEXT    REFERENCES schedule_entries (id) ON DELETE SET NULL, -- NULL: ad-hoc recording
    status          TEXT    NOT NULL CHECK (status IN (
                        'STARTING', 'RECORDING', 'STOPPING', 'FINALIZING',
                        'COMPLETED', 'FAILED', 'RECOVERY_REQUIRED')),
    started_at      INTEGER NOT NULL,
    stopped_at      INTEGER,
    heartbeat_at    INTEGER,                               -- watchdog liveness mark; stale after restart => RECOVERY_REQUIRED (section 6.2)
    max_duration_ms INTEGER NOT NULL CHECK (max_duration_ms > 0), -- hard cap applied to this recording, default 3 h (section 6.2, AC-04)
    duration_ms     INTEGER CHECK (duration_ms >= 0),      -- final duration, set when finalized
    chunk_count     INTEGER NOT NULL DEFAULT 0 CHECK (chunk_count >= 0),
    chunks_ref      TEXT    NOT NULL,                      -- recordings/<id>: directory of chunk files (section 6.3)
    audio_ref       TEXT,                                  -- recordings/<id>.<ext>: the single final track
    failure_reason  TEXT,                                  -- short non-sensitive text for the UI
    version         INTEGER NOT NULL DEFAULT 0,            -- optimistic concurrency (ADR-0004)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    CHECK (status <> 'COMPLETED' OR audio_ref IS NOT NULL)
);
CREATE INDEX idx_recordings_status ON recordings (status);
CREATE INDEX idx_recordings_lesson ON recordings (lesson_id);

-- ---------------------------------------------------------------------------------------------------------------
-- transcripts: local STT result of one recording (PRD section 8, section 14 case 11; AC-06, AC-08).
-- Aggregate transcription/transcript. model_name/model_version are copies so the pin stays readable after
-- the model is removed (AC-08). Deleting a recording deletes its transcripts (section 13.1): the use case does it
-- explicitly (RecordingTranscripts.DeleteByRecording, in the same transaction as the recording delete). The
-- ON DELETE CASCADE below is only a safety net behind PRAGMA foreign_keys = ON; no code path relies on it.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE transcripts (
    id             TEXT    PRIMARY KEY,
    recording_id   TEXT    NOT NULL REFERENCES recordings (id) ON DELETE CASCADE,
    model_id       TEXT    REFERENCES stt_models (id) ON DELETE SET NULL,
    model_name     TEXT    NOT NULL CHECK (length(model_name) > 0),
    model_version  TEXT    NOT NULL CHECK (length(model_version) > 0),
    language       TEXT,
    text_ref       TEXT    NOT NULL,                       -- transcripts/<id>: the transcript file
    version        INTEGER NOT NULL DEFAULT 0,             -- optimistic concurrency (ADR-0004)
    created_at     INTEGER NOT NULL
);
CREATE INDEX idx_transcripts_recording ON transcripts (recording_id);

-- ---------------------------------------------------------------------------------------------------------------
-- assignments: homework items, system of record (PRD section 10, section 5.3; AC-13, AC-20).
-- Aggregate coursework/assignment. Draft versions and deadlines/summaries tables are added by TASK-042/TASK-043.
-- LLM-extracted values are untrusted until validated by the core (hard rule 3).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE assignments (
    id             TEXT    PRIMARY KEY,
    lesson_id      TEXT    REFERENCES schedule_entries (id) ON DELETE SET NULL,
    transcript_id  TEXT    REFERENCES transcripts (id) ON DELETE SET NULL,
    title          TEXT    NOT NULL CHECK (length(title) > 0),
    description    TEXT    NOT NULL DEFAULT '',
    due_at         INTEGER,
    user_edited    INTEGER NOT NULL DEFAULT 0 CHECK (user_edited IN (0, 1)), -- reprocessing must not overwrite user edits (TASK-042)
    version        INTEGER NOT NULL DEFAULT 0,             -- optimistic concurrency (ADR-0004): an edit and a reprocessing job must not overwrite each other
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX idx_assignments_lesson ON assignments (lesson_id);
CREATE INDEX idx_assignments_due ON assignments (due_at);

-- ---------------------------------------------------------------------------------------------------------------
-- jobs: persistent job store (PRD section 7.1, 7.2, 7.3, 7.4; AC-09, AC-10, AC-21, AC-22).
-- Aggregate jobs/job. Status values = job.Statuses = all states of PRD section 7.2, including WAITING_FOR_POWER.
-- Ownership (ADR-0004): from Claim on, a job is written only by the jobrunner use case (version guarded); handlers
-- report progress and checkpoint through the runner and never save the job.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE jobs (
    id               TEXT    PRIMARY KEY,
    type             TEXT    NOT NULL CHECK (length(type) > 0), -- selects the handler, e.g. stt (open set, see job.Type)
    status           TEXT    NOT NULL CHECK (status IN (
                         'QUEUED', 'RUNNING', 'COMPLETED', 'RETRY_WAIT', 'CANCELLED',
                         'FAILED', 'WAITING_FOR_NETWORK', 'WAITING_FOR_QUOTA', 'WAITING_FOR_POWER')),
    -- One key = one piece of work for ever (a second Enqueue returns the existing job). Callers put a
    -- discriminator in it when the same subject may legitimately be processed again (section 7.3, 14 case 5, hard rule 7).
    idempotency_key  TEXT    NOT NULL UNIQUE CHECK (length(idempotency_key) > 0),
    payload          TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(payload)), -- ids and pinned versions only; no secrets, no audio, no prompts
    -- STT job pins the exact model (AC-08). No FK on purpose: finished jobs must not block model removal. Whether a
    -- model is pinned is decided with job.ActiveStatuses() passed by the use case; the pin check and the model delete
    -- run in one write transaction (section 14 case 11).
    pinned_model_id  TEXT,
    attempts         INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts     INTEGER NOT NULL CHECK (max_attempts >= 1),   -- bounded retries (section 7.3, AC-10)
    next_run_at      INTEGER NOT NULL,                     -- QUEUED jobs are eligible when next_run_at <= now; RETRY_WAIT: end of backoff
    -- "Run now" override of the AC-power wait (section 7.4, AC-22). Claim uses it through job.ClaimFilter: a QUEUED
    -- run_now job is claimable even when its heavy type is filtered out because the computer runs on battery.
    run_now          INTEGER NOT NULL DEFAULT 0 CHECK (run_now IN (0, 1)),
    progress_percent INTEGER NOT NULL DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
    checkpoint       TEXT,                                 -- opaque resume info persisted at a safe point, e.g. last finished chunk (section 7.4)
    last_error       TEXT,                                 -- sanitised message for the UI; never secrets or prompt text
    created_at       INTEGER NOT NULL,
    started_at       INTEGER,
    finished_at      INTEGER,
    version          INTEGER NOT NULL DEFAULT 0,           -- optimistic concurrency (ADR-0004); Claim is a guarded UPDATE ... WHERE status = 'QUEUED' AND version = ?
    updated_at       INTEGER NOT NULL,
    CHECK (attempts <= max_attempts),
    CHECK (status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED') OR finished_at IS NOT NULL)
);
-- Claim: next eligible QUEUED job (section 7.2). The adapter restores the aggregate, calls the domain transition and
-- saves it with the guarded update; it never sets the status with a raw UPDATE.
CREATE INDEX idx_jobs_status_next_run ON jobs (status, next_run_at);
CREATE INDEX idx_jobs_pinned_model ON jobs (pinned_model_id) WHERE pinned_model_id IS NOT NULL;

-- ---------------------------------------------------------------------------------------------------------------
-- deliveries: attempts to email an artifact (PRD section 10, section 14 case 7; AC-14, AC-15; hard rule 7).
-- Aggregate delivery/delivery. Status values = delivery.Statuses. The PRD names only UNKNOWN; the rest is a
-- proposal derived from section 10 (see the delivery package and ADR-0004): SENDING is saved before the message is
-- handed to the provider, so a crash becomes UNKNOWN; a transient failure returns SENDING to PENDING.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE deliveries (
    id               TEXT    PRIMARY KEY,
    assignment_id    TEXT    NOT NULL REFERENCES assignments (id) ON DELETE RESTRICT, -- keep the history of what was sent
    recipient        TEXT    NOT NULL CHECK (length(recipient) > 0),  -- an email address, not a secret
    subject          TEXT    NOT NULL,
    artifact_ref     TEXT    NOT NULL,                     -- exports/<file>: the attachment
    message_id       TEXT    NOT NULL UNIQUE,              -- RFC 5322 Message-ID from IDGenerator.NewMessageID, generated BEFORE sending: the handle for reconciliation of UNKNOWN
    idempotency_key  TEXT    NOT NULL UNIQUE CHECK (length(idempotency_key) > 0),
    status           TEXT    NOT NULL CHECK (status IN ('PENDING', 'SENDING', 'SENT', 'FAILED', 'UNKNOWN')),
    attempts         INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error       TEXT,                                 -- sanitised; never credentials
    confirmed_at     INTEGER NOT NULL,                     -- explicit user confirmation; a delivery cannot exist without it (AC-14)
    sent_at          INTEGER,
    version          INTEGER NOT NULL DEFAULT 0,           -- optimistic concurrency (ADR-0004)
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    CHECK (status <> 'SENT' OR sent_at IS NOT NULL)
);
CREATE INDEX idx_deliveries_status ON deliveries (status);
CREATE INDEX idx_deliveries_assignment ON deliveries (assignment_id);

-- ---------------------------------------------------------------------------------------------------------------
-- settings: the one row of typed user settings (PRD sections 6.1, 7.4, 8, 15; AC-02, AC-07, AC-22).
-- Aggregate settings/settings. DEFAULTs mirror the PRD defaults; the domain owns the defaults (TASK-007).
-- Adding a setting is a migration with a new typed column. Never store secrets here (hard rule 4).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE settings (
    id                            INTEGER PRIMARY KEY CHECK (id = 1),
    recording_prompts_enabled     INTEGER NOT NULL DEFAULT 1 CHECK (recording_prompts_enabled IN (0, 1)),     -- AC-02
    process_only_on_ac_power      INTEGER NOT NULL DEFAULT 1 CHECK (process_only_on_ac_power IN (0, 1)),      -- AC-21, AC-22
    notify_on_processing_complete INTEGER NOT NULL DEFAULT 1 CHECK (notify_on_processing_complete IN (0, 1)), -- section 15
    default_stt_model_id          TEXT    REFERENCES stt_models (id) ON DELETE RESTRICT,                      -- AC-07; the use case clears it via the settings aggregate before removing a model (ADR-0004)
    version                       INTEGER NOT NULL DEFAULT 0,                                                  -- optimistic concurrency (ADR-0004)
    updated_at                    INTEGER NOT NULL
);

-- ---------------------------------------------------------------------------------------------------------------
-- summaries: the LLM summary of one transcript (PRD sections 9 and 10; AC-13). Aggregate coursework/summary.
-- id = the transcript id: one summary per transcript, so reprocessing replaces it. The text is model output, stored
-- as data only (hard rule 3). The summary stays when its transcript is deleted (transcript_id SET NULL).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE summaries (
    id              TEXT    PRIMARY KEY,
    transcript_id   TEXT    REFERENCES transcripts (id) ON DELETE SET NULL,
    lesson_id       TEXT    REFERENCES schedule_entries (id) ON DELETE SET NULL,
    body            TEXT    NOT NULL CHECK (length(body) > 0),
    key_points      TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(key_points)), -- JSON array of strings
    prompt_version  INTEGER NOT NULL CHECK (prompt_version >= 1),                 -- prompts.Version that produced it
    version         INTEGER NOT NULL DEFAULT 0,                                    -- optimistic concurrency (ADR-0004)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_summaries_lesson ON summaries (lesson_id);

-- assignments extracted from a transcript: source_index is the position of the item in the extraction answer, so
-- reprocessing finds the assignment it made before (no duplicates); prompt_version is the prompts.Version of the
-- extraction that last set the values. Both are NULL for an assignment that was not extracted. A user-edited row
-- (user_edited = 1) is never overwritten by reprocessing; the rule lives in the assignment aggregate.
ALTER TABLE assignments ADD COLUMN source_index INTEGER CHECK (source_index IS NULL OR source_index >= 0);
ALTER TABLE assignments ADD COLUMN prompt_version INTEGER CHECK (prompt_version IS NULL OR prompt_version >= 1);
CREATE UNIQUE INDEX idx_assignments_source ON assignments (transcript_id, source_index)
    WHERE transcript_id IS NOT NULL AND source_index IS NOT NULL;

-- deliveries: the SHA-256 and size of the attachment the user confirmed (AC-14). The adapter sends the file only if the
-- bytes it read match, so a file changed after the confirmation is never sent. The DEFAULTs exist only because SQLite
-- needs them to add a NOT NULL column; no delivery row exists before this migration, and the aggregate rejects them.
ALTER TABLE deliveries ADD COLUMN attachment_sha256 TEXT NOT NULL DEFAULT '' CHECK (attachment_sha256 = '' OR length(attachment_sha256) = 64);
ALTER TABLE deliveries ADD COLUMN attachment_size INTEGER NOT NULL DEFAULT 0 CHECK (attachment_size >= 0);
-- deliveries: the send round, 0 for the first job of a delivery and one more for every "send again" (UNKNOWN to
-- PENDING). The job idempotency key is per round, so retries, repeats and restarts never add a second job.
ALTER TABLE deliveries ADD COLUMN send_round INTEGER NOT NULL DEFAULT 0 CHECK (send_round >= 0);

-- ---------------------------------------------------------------------------------------------------------------
-- job_notices: the user was notified that a job reached a finished status (PRD section 15). Aggregate jobs/notice.
-- One row per job and status, written BEFORE the notification is shown, so repeated events and restarts never notify
-- twice (at most once: a crash between the two loses that notification). Rows never change, so there is no Save
-- port and no version column (ADR-0004(d)). Status values = notice.Statuses. Nothing but the job type is copied: the
-- texts shown are fixed per type, never user content.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE job_notices (
    job_id      TEXT    NOT NULL REFERENCES jobs (id) ON DELETE CASCADE, -- a notice means nothing without its job
    status      TEXT    NOT NULL CHECK (status IN ('COMPLETED', 'FAILED')),
    job_type    TEXT    NOT NULL CHECK (length(job_type) > 0),
    notified_at INTEGER NOT NULL,
    PRIMARY KEY (job_id, status)
);
