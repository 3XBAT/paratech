-- paratech desktop: local SQLite schema DRAFT (TASK-003).
--
-- Consumed by TASK-005: goose migrations in desktop/pkg/migrations (ARCH-04, ADR-0006) and sqlc queries are
-- written from this file. This is a draft of the contract, not a migration: it has no goose annotations.
-- Architecture: docs/architecture.md (aggregates map to tables, ARCH-13 references are plain id columns).
-- Source of requirements: docs/prd/learning-assistant-prd-v0.4.md.
-- job_notices (0004) and settings.notify_on_processing_complete were dropped by migration 0008 (TASK-065).
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
--     recording.Statuses, delivery.Statuses, proposal.Statuses). desktop/tests/schema_test.go keeps
--     both in sync.
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
-- settings: the one row of typed user settings (PRD sections 6.1, 7.4, 8; AC-02, AC-07, AC-22).
-- Aggregate settings/settings. DEFAULTs mirror the PRD defaults; the domain owns the defaults (TASK-007).
-- Adding a setting is a migration with a new typed column. Never store secrets here (hard rule 4).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE settings (
    id                            INTEGER PRIMARY KEY CHECK (id = 1),
    recording_prompts_enabled     INTEGER NOT NULL DEFAULT 1 CHECK (recording_prompts_enabled IN (0, 1)),     -- AC-02
    process_only_on_ac_power      INTEGER NOT NULL DEFAULT 1 CHECK (process_only_on_ac_power IN (0, 1)),      -- AC-21, AC-22
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
-- homework_drafts: the draft history of one assignment (PRD section 10; AC-13). Aggregate coursework/homeworkdraft;
-- this row is the root and carries its version, the versions of the draft are homework_draft_revisions.
-- Deletion: an assignment with a draft cannot be deleted (RESTRICT, the repository reports assignment.ErrInUse), so
-- reprocessing never drops what the student wrote; deleting a draft row removes its versions (CASCADE).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE homework_drafts (
    assignment_id TEXT    PRIMARY KEY REFERENCES assignments (id) ON DELETE RESTRICT,
    version       INTEGER NOT NULL DEFAULT 0 -- optimistic concurrency (ADR-0004)
);

-- homework_draft_revisions: one version of a draft. Versions are append-only: the repository inserts only those it has
-- not stored, in the compare-and-swap transaction of the draft, and never updates one. Limits are those of
-- homeworkdraft (title 200, body 16000 characters, 100 versions). job_id is the homework_draft job of a generated
-- version, unique so that one job adds at most one version; no foreign key, so purging finished jobs keeps the history.
CREATE TABLE homework_draft_revisions (
    assignment_id  TEXT    NOT NULL REFERENCES homework_drafts (assignment_id) ON DELETE CASCADE,
    number         INTEGER NOT NULL CHECK (number BETWEEN 1 AND 100),
    origin         TEXT    NOT NULL CHECK (origin IN ('generated', 'edited')),
    title          TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    body           TEXT    NOT NULL CHECK (length(body) BETWEEN 1 AND 16000),
    instruction    TEXT    NOT NULL DEFAULT '' CHECK (length(instruction) <= 4096), -- the student's instructions; not a secret
    prompt_version INTEGER CHECK (prompt_version IS NULL OR prompt_version >= 1),
    job_id         TEXT    UNIQUE,
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (assignment_id, number),
    CHECK ((origin = 'generated' AND job_id IS NOT NULL AND prompt_version IS NOT NULL) OR
           (origin = 'edited' AND job_id IS NULL AND prompt_version IS NULL AND instruction = ''))
);

-- settings: the non-secret mail and Hub setup (PRD sections 10 and 12). Empty means not set. The SMTP password and the
-- Hub API key are in Windows Credential Manager, never here (hard rule 4).
ALTER TABLE settings ADD COLUMN homework_recipient TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN smtp_host TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN smtp_port INTEGER NOT NULL DEFAULT 0 CHECK (smtp_port BETWEEN 0 AND 65535); -- 0: the default of the mode
ALTER TABLE settings ADD COLUMN smtp_security TEXT NOT NULL DEFAULT 'starttls' CHECK (smtp_security IN ('starttls', 'tls'));
ALTER TABLE settings ADD COLUMN smtp_username TEXT NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN hub_url TEXT NOT NULL DEFAULT '';

-- settings: the executable name of the app the user last recorded ("chrome.exe"), preselected next time; empty: the
-- whole computer. An app name, never a process ID: it is remembered across restarts of the app.
ALTER TABLE settings ADD COLUMN recording_source_app TEXT NOT NULL DEFAULT '' CHECK (length(recording_source_app) <= 255);

-- ---------------------------------------------------------------------------------------------------------------
-- proposals: the actions the LLM proposed, with their validation and confirmation state (PRD section 13.3; AC-14,
-- AC-18). Aggregate coursework/proposal; the columns are proposal.State. Status values = proposal.Statuses, kinds =
-- proposal.Kinds. title, body and due_at are model text, stored as data only (hard rule 3); recipient, the attachment
-- digest and assignment_title are frozen by the core at validation, never taken from the model.
-- assignment_id has no foreign key on purpose: a proposal naming an assignment that does not exist is stored REJECTED
-- (unknown_assignment), which a foreign key would refuse; an assignment deleted later makes a confirmed proposal STALE
-- (Recheck) and keeps an EXECUTED one as the record of what was done, while a RESTRICT would block the deletion by
-- reprocessing for every declined or rejected proposal.
-- Rebuilt by migration 0033 (TASK-180): kinds = proposal.AllKinds, with send_work_email, the core's own send of the
-- file of a turnkey work (work_id; never a model proposal); attachment_format ('txt', 'docx' or 'pptx', proposal.Format)
-- is the file a send attaches: the text or Word export of a homework send ('' for sends stored before TASK-094: the
-- text export), the .docx or .pptx of a work; '' for the other kinds.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE proposals (
    id                TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    kind              TEXT    NOT NULL CHECK (kind IN ('send_homework_email', 'export_homework', 'create_deadline_event', 'send_work_email')),
    status            TEXT    NOT NULL CHECK (status IN (
                          'PROPOSED', 'VALIDATED', 'REJECTED', 'CONFIRMED', 'DECLINED', 'EXECUTED', 'STALE')),
    assignment_id     TEXT    NOT NULL CHECK (length(assignment_id) > 0),
    title             TEXT    NOT NULL DEFAULT '',           -- model text: the event title (create_deadline_event)
    body              TEXT    NOT NULL DEFAULT '',           -- model text: the homework (export_homework)
    due_at            INTEGER,                               -- model value: the deadline (create_deadline_event)
    reason            TEXT    NOT NULL DEFAULT '',           -- why it was rejected or went stale (proposal.RejectReason)
    recipient         TEXT    NOT NULL DEFAULT '',           -- the trusted address, an email address, not a secret
    attachment_sha256 TEXT    NOT NULL DEFAULT '' CHECK (attachment_sha256 = '' OR length(attachment_sha256) = 64),
    attachment_size   INTEGER NOT NULL DEFAULT 0 CHECK (attachment_size >= 0),
    assignment_title  TEXT    NOT NULL DEFAULT '',           -- the trusted heading of the export file (export_homework)
    confirmed_at      INTEGER,                               -- the explicit user confirmation (AC-14)
    executed_at       INTEGER,
    version           INTEGER NOT NULL DEFAULT 0,            -- optimistic concurrency (ADR-0004)
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    attachment_format TEXT    NOT NULL DEFAULT '' CHECK (attachment_format IN ('', 'txt', 'docx', 'pptx')),
    work_id           TEXT    NOT NULL DEFAULT '' CHECK (length(work_id) <= 128), -- the work whose file is sent (send_work_email)
    CHECK ((due_at IS NOT NULL) = (kind = 'create_deadline_event')),
    CHECK ((reason <> '') = (status IN ('REJECTED', 'STALE'))),
    CHECK ((confirmed_at IS NOT NULL) = (status IN ('CONFIRMED', 'EXECUTED', 'STALE'))),
    CHECK ((executed_at IS NOT NULL) = (status = 'EXECUTED')),
    CHECK ((work_id <> '') = (kind = 'send_work_email'))
);

-- ---------------------------------------------------------------------------------------------------------------
-- transcript_parts: the segments of a recording recognised so far, stored in batches while the STT job runs, so the
-- transcript viewer shows them during recognition and after a restart (TASK-079). Values are transcript.Part. seq is
-- the number in the engine run; a rerun numbers from 0 again and replaces rows by (recording_id, seq). The rows are a
-- preview: the STT job deletes them once the transcript is stored, and they go with the recording (CASCADE). No
-- version column: rows are replaced, never edited through an aggregate.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE transcript_parts (
    recording_id TEXT    NOT NULL REFERENCES recordings (id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL CHECK (seq >= 0),
    start_ms     INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms       INTEGER NOT NULL CHECK (end_ms >= start_ms),
    text         TEXT    NOT NULL,
    PRIMARY KEY (recording_id, seq)
);

-- jobs: job.Cause of the last unsuccessful attempt, for example a Hub that is not set up while the job parks in
-- WAITING_FOR_NETWORK; empty in every other case. Values = job.Causes.
ALTER TABLE jobs ADD COLUMN cause TEXT NOT NULL DEFAULT '' CHECK (cause IN ('', 'service_not_configured', 'service_unavailable'));

-- settings: how hard speech recognition may load the computer: 'gentle', 'normal' or 'max'. The aggregate reads any
-- other value as the default, so the column is only bounded, not restricted to the list.
ALTER TABLE settings ADD COLUMN recognition_load TEXT NOT NULL DEFAULT 'gentle' CHECK (length(recognition_load) <= 16);

-- ---------------------------------------------------------------------------------------------------------------
-- stt_speeds: the measured speed of an installed model in a load mode: the moving average of wall time over audio
-- duration of its runs (values.MeasuredSpeed) and how many runs it is based on. It refines the time estimates of the
-- pages. The rows go with the model (CASCADE). No version column: a row is replaced, never saved through an aggregate.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE stt_speeds (
    stt_model_id     TEXT    NOT NULL REFERENCES stt_models (id) ON DELETE CASCADE,
    recognition_load TEXT    NOT NULL CHECK (recognition_load IN ('gentle', 'normal', 'max')),
    factor           REAL    NOT NULL CHECK (factor > 0),
    samples          INTEGER NOT NULL CHECK (samples >= 1),
    updated_at       INTEGER NOT NULL,
    PRIMARY KEY (stt_model_id, recognition_load)
);

-- recordings: 1 when the final track is silent (recording.Recording.Silent): the chosen source played nothing, so the
-- recording is not transcribed and the UI says so. Only a COMPLETED recording is silent; existing rows are not.
ALTER TABLE recordings ADD COLUMN silent INTEGER NOT NULL DEFAULT 0 CHECK (silent IN (0, 1));

-- recordings: the snapshot of the lesson at the start (values.LessonSnapshot): imported text, plain text only; the times
-- are Unix milliseconds of the scheduled start and end. NULL: unknown, an ad-hoc recording or one stored before.
ALTER TABLE recordings ADD COLUMN lesson_title TEXT;
ALTER TABLE recordings ADD COLUMN lesson_subject TEXT;
ALTER TABLE recordings ADD COLUMN lesson_kind TEXT;
ALTER TABLE recordings ADD COLUMN lesson_teacher TEXT;
ALTER TABLE recordings ADD COLUMN lesson_start INTEGER;
ALTER TABLE recordings ADD COLUMN lesson_end INTEGER;

-- schedule_entries: the course of the lesson ('' unknown), the type of the class (values = lesson.Kinds) and the
-- teacher ('' none), at most lesson.MaxSubjectRunes and MaxTeacherRunes characters; part of the content the calendar
-- shows. Lessons stored before stay unknown until the next import of their file derives them.
ALTER TABLE schedule_entries ADD COLUMN subject TEXT NOT NULL DEFAULT '' CHECK (length(subject) <= 200);
ALTER TABLE schedule_entries ADD COLUMN kind TEXT NOT NULL DEFAULT 'unknown' CHECK (kind IN ('unknown', 'lecture', 'seminar', 'practice', 'lab'));
ALTER TABLE schedule_entries ADD COLUMN teacher TEXT NOT NULL DEFAULT '' CHECK (length(teacher) <= 200);

-- assignments: the subject and teacher the homework search found in the transcript (hint_*, model output, shown
-- marked «по записи») and the ones the user entered (user_*); '' none. One plain line of at most values.MaxNameRunes
-- characters each. The schedule's subject and teacher are read from the recording's lesson snapshot, never copied here.
ALTER TABLE assignments ADD COLUMN hint_subject TEXT NOT NULL DEFAULT '' CHECK (length(hint_subject) <= 200);
ALTER TABLE assignments ADD COLUMN hint_teacher TEXT NOT NULL DEFAULT '' CHECK (length(hint_teacher) <= 200);
ALTER TABLE assignments ADD COLUMN user_subject TEXT NOT NULL DEFAULT '' CHECK (length(user_subject) <= 200);
ALTER TABLE assignments ADD COLUMN user_teacher TEXT NOT NULL DEFAULT '' CHECK (length(user_teacher) <= 200);

-- ---------------------------------------------------------------------------------------------------------------
-- lecture_texts (migration 0016): the lecture text of one transcript. Aggregate coursework/lecturetext; id = the
-- transcript id. blocks is the text shown (empty until a run completed); run, run_parts and parts are the run in
-- progress and its finished parts, so a run that stops continues from the next part. The text is model output, stored as data only (hard rule
-- 3); it stays when its transcript is deleted (transcript_id SET NULL), like the summary.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE lecture_texts (
    id              TEXT    PRIMARY KEY,
    transcript_id   TEXT    REFERENCES transcripts (id) ON DELETE SET NULL,
    lesson_id       TEXT    REFERENCES schedule_entries (id) ON DELETE SET NULL,
    blocks          TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(blocks)), -- JSON array of {heading, start_ms, paragraphs}
    prompt_version  INTEGER NOT NULL DEFAULT 0 CHECK (prompt_version >= 0),  -- prompts.LectureTextVersion; 0 without blocks
    run             TEXT    NOT NULL DEFAULT '' CHECK (length(run) <= 128),  -- digest of the requests of the run; '' none
    run_parts       INTEGER NOT NULL DEFAULT 0 CHECK (run_parts BETWEEN 0 AND 64),
    parts           TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(parts)),  -- JSON array of the finished parts (block arrays)
    version         INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX idx_lecture_texts_lesson ON lecture_texts (lesson_id);

-- Migration 0017.
-- settings: the student profile of the title page of a homework document (settings.Profile): full name, group,
-- university, faculty or department and city; '' not set. One plain line of at most settings.MaxProfileRunes
-- characters each. Local data, not a secret.
ALTER TABLE settings ADD COLUMN student_full_name TEXT NOT NULL DEFAULT '' CHECK (length(student_full_name) <= 200);
ALTER TABLE settings ADD COLUMN student_group TEXT NOT NULL DEFAULT '' CHECK (length(student_group) <= 200);
ALTER TABLE settings ADD COLUMN student_university TEXT NOT NULL DEFAULT '' CHECK (length(student_university) <= 200);
ALTER TABLE settings ADD COLUMN student_faculty TEXT NOT NULL DEFAULT '' CHECK (length(student_faculty) <= 200);
ALTER TABLE settings ADD COLUMN student_city TEXT NOT NULL DEFAULT '' CHECK (length(student_city) <= 200);

-- proposals: attachment_format, added by 0017, is part of the table as 0033 rebuilt it (above).

-- recordings (migration 0018): the deletion mark (recording.Recording.MarkDeleting), set before any file or row is
-- removed, and the found assignments to delete with the recording (a JSON array of ids). A marked recording is hidden
-- from the lists and its deletion is resumed at the next start. NULL: not being deleted.
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

-- recordings (migration 0019): the subject and the kind of the lesson the user set (values.LessonCorrection), one plain
-- line and a kind code. NULL: not corrected, the lesson snapshot or the schedule names it.
ALTER TABLE recordings ADD COLUMN user_subject TEXT CHECK (user_subject IS NULL OR length(user_subject) BETWEEN 1 AND 256);
ALTER TABLE recordings ADD COLUMN user_kind TEXT CHECK (user_kind IS NULL OR user_kind IN ('lecture', 'seminar', 'practice', 'lab'));

-- summaries (migration 0020): the sections as a JSON array of {"heading","text","points"} objects (summary.Section).
-- NULL: a summary made before, one plain text.
ALTER TABLE summaries ADD COLUMN sections TEXT CHECK (sections IS NULL OR (json_valid(sections) AND json_type(sections) = 'array'));

-- ---------------------------------------------------------------------------------------------------------------
-- subjects (migration 0021): the subjects the student created. Aggregate library/subject; name_key is values.Key of
-- the name (case-insensitive, «ё» = «е»), unique. semester is values.Semester.Code. Never created automatically.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE subjects (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    name        TEXT    NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    name_key    TEXT    NOT NULL UNIQUE CHECK (length(name_key) BETWEEN 1 AND 400),
    semester    TEXT    NOT NULL CHECK (semester GLOB '[0-9][0-9][0-9][0-9]-autumn' OR semester GLOB '[0-9][0-9][0-9][0-9]-spring'),
    version     INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- subject_aliases: other spellings of a subject, part of the aggregate (no version of their own). An alias key is
-- unique across all subjects; that it differs from every other subject's name key is checked by the use case.
CREATE TABLE subject_aliases (
    alias_key   TEXT    NOT NULL PRIMARY KEY CHECK (length(alias_key) BETWEEN 1 AND 400),
    subject_id  TEXT    NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    alias       TEXT    NOT NULL CHECK (length(alias) BETWEEN 1 AND 200),
    position    INTEGER NOT NULL CHECK (position >= 0)
);
CREATE INDEX idx_subject_aliases_subject ON subject_aliases (subject_id);

-- subject_folders: the folders of a subject, part of the aggregate: the six system folders (system = their code) and
-- the student's own ('' system). Names are unique per subject by key. (subject_id, id) is the target of the
-- materials' foreign key, so a file can only be filed in a folder of its own subject.
CREATE TABLE subject_folders (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_id  TEXT    NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    name_key    TEXT    NOT NULL CHECK (length(name_key) BETWEEN 1 AND 128),
    system      TEXT    NOT NULL DEFAULT '' CHECK (system IN ('', 'lectures', 'seminars', 'labs', 'materials', 'homework', 'tickets')),
    position    INTEGER NOT NULL CHECK (position >= 0),
    UNIQUE (subject_id, name_key),
    UNIQUE (subject_id, id)
);

-- ---------------------------------------------------------------------------------------------------------------
-- materials: the files of the library. Aggregate library/material. One row and one blob per sha256 (the blob is
-- materials/<sha256> in the artifact store): uploading the same bytes again returns this row. original_name is shown
-- only. NULL subject and folder: «Входящие». A subject or folder that holds a file (active or removed) cannot be
-- deleted (RESTRICT): a removed file still needs them to be restored. Photos ('jpeg', 'png', rebuilt by migration
-- 0041, TASK-165) are pages of a note without text: text_status 'no_text', and only they have it.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE materials (
    id            TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    sha256        TEXT    NOT NULL UNIQUE CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size_bytes    INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 104857600),
    type          TEXT    NOT NULL CHECK (type IN ('docx', 'pptx', 'txt', 'pdf', 'jpeg', 'png')),
    original_name TEXT    NOT NULL DEFAULT '' CHECK (length(original_name) <= 255),
    subject_id    TEXT    REFERENCES subjects (id) ON DELETE RESTRICT,
    folder_id     TEXT,
    status        TEXT    NOT NULL CHECK (status IN ('active', 'removed')),
    text_status   TEXT    NOT NULL CHECK (text_status IN ('pending', 'extracted', 'failed', 'pdf_not_extracted', 'no_text')),
    version       INTEGER NOT NULL DEFAULT 0,                             -- optimistic concurrency (ADR-0004)
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    CHECK ((subject_id IS NULL) = (folder_id IS NULL)),
    CHECK ((type IN ('jpeg', 'png')) = (text_status = 'no_text')),
    FOREIGN KEY (subject_id, folder_id) REFERENCES subject_folders (subject_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_materials_place ON materials (subject_id, folder_id);

-- material_lessons: the lessons a material is linked to, part of the material aggregate. A purged lesson takes its
-- links with it.
CREATE TABLE material_lessons (
    material_id TEXT    NOT NULL REFERENCES materials (id) ON DELETE CASCADE,
    lesson_id   TEXT    NOT NULL REFERENCES schedule_entries (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (material_id, lesson_id)
);
CREATE INDEX idx_material_lessons_lesson ON material_lessons (lesson_id);

-- agent_threads (migration 0022, rebuilt by 0040): one row per agent chat (ADR-0028), not tied to a subject: the
-- subject belongs to the scope of each request. Aggregate agent/thread; its current request is the agent_turns row
-- number = turns. title is thread.Title (1..80 characters, «Новый чат» until the first text); archived_at is set while
-- the chat is in «Архив» (never deleted: journal lines reference it); last_active_at is when its last request started
-- (its creation before the first): the current chat is the unarchived one with the newest. seen is the JSON array of
-- the {"kind","id"} the provenance check knows from tool results; history_from is the first request whose history is
-- sent (a request restarted after its model turn expired on the Hub sends only its own).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE agent_threads (
    id             TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    title          TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    turns          INTEGER NOT NULL DEFAULT 0 CHECK (turns >= 0),
    history_from   INTEGER NOT NULL DEFAULT 0 CHECK (history_from BETWEEN 0 AND turns),
    seen           TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(seen) AND json_type(seen) = 'array'),
    archived_at    INTEGER CHECK (archived_at IS NULL OR archived_at >= created_at),
    last_active_at INTEGER NOT NULL CHECK (last_active_at >= created_at),
    version        INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX idx_agent_threads_active ON agent_threads (archived_at, last_active_at);

-- ---------------------------------------------------------------------------------------------------------------
-- agent_turns: the requests of a thread, written with the thread (part of its aggregate, no version of its own).
-- Budget counters, the plan (plan_steps JSON), the calls of the last step that wait for results and the ids created
-- by approved create steps. planner_input counts the planner input of the request. mentions (0040) is the JSON array
-- of the {"kind","id","label"} the student mentioned (values.Mention, at most 8; the label is display only, never sent).
-- dropped_by (0040) is the number of the later request that restarted the chat from an earlier one (0: not dropped);
-- a dropped request stays stored but is no longer read into the planner's history.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE agent_turns (
    thread_id       TEXT    NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    number          INTEGER NOT NULL CHECK (number >= 1),
    status          TEXT    NOT NULL CHECK (status IN ('PLANNING', 'AWAITING_PLAN', 'AWAITING_ANSWERS', 'EXECUTING', 'DONE', 'STOPPED', 'FAILED', 'CANCELLED')),
    resume_status   TEXT    NOT NULL DEFAULT '' CHECK (resume_status IN ('', 'PLANNING', 'EXECUTING')),
    stop_reason     TEXT    NOT NULL DEFAULT '' CHECK (stop_reason IN ('', 'budget', 'offline', 'user', 'quota', 'failed', 'expired')),
    scope           TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(scope) AND json_type(scope) = 'object'),
    mentions        TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(mentions) AND json_type(mentions) = 'array'),
    steps           INTEGER NOT NULL DEFAULT 0 CHECK (steps >= 0),
    writes          INTEGER NOT NULL DEFAULT 0 CHECK (writes >= 0),
    paid_unplanned  INTEGER NOT NULL DEFAULT 0 CHECK (paid_unplanned >= 0),
    planner_input   INTEGER NOT NULL DEFAULT 0 CHECK (planner_input >= 0),
    question_rounds INTEGER NOT NULL DEFAULT 0 CHECK (question_rounds >= 0),
    step_open       INTEGER NOT NULL DEFAULT 0 CHECK (step_open IN (0, 1)),
    retry           INTEGER NOT NULL DEFAULT 0 CHECK (retry >= 0),
    proposed_plan   TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(proposed_plan) AND json_type(proposed_plan) = 'array'),
    approved_plan   TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(approved_plan) AND json_type(approved_plan) = 'array'),
    used            TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(used) AND json_type(used) = 'array'),
    open_calls      TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(open_calls) AND json_type(open_calls) = 'array'),
    created_ids     TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(created_ids) AND json_type(created_ids) = 'array'),
    dropped_by      INTEGER NOT NULL DEFAULT 0 CHECK (dropped_by = 0 OR dropped_by > number),
    started_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (thread_id, number)
);

-- ---------------------------------------------------------------------------------------------------------------
-- agent_steps: the history of a thread as sent to the planner, in order (seq): the student's text, the sealed calls
-- of a step and their results, as JSON. Append only; purged after 90 days by created_at.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE agent_steps (
    thread_id  TEXT    NOT NULL REFERENCES agent_threads (id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL CHECK (seq >= 1),
    turn       INTEGER NOT NULL CHECK (turn >= 1),
    entry      TEXT    NOT NULL CHECK (entry IN ('student', 'calls', 'results')),
    body       TEXT    NOT NULL CHECK (json_valid(body) AND json_type(body) = 'object'),
    created_at INTEGER NOT NULL,
    PRIMARY KEY (thread_id, seq)
);
CREATE INDEX idx_agent_steps_turn ON agent_steps (thread_id, turn);
CREATE INDEX idx_agent_steps_created ON agent_steps (created_at);

-- ---------------------------------------------------------------------------------------------------------------
-- agent_actions: the action journal (aggregate agent/action). A U call is PENDING before its use case runs and gets
-- its outcome after; compensation is the typed Undo data (JSON {"kind", ...ids, old values}) of a DONE entry and
-- target_version the version of the target right after the action. proposal is '<kind>:<id>' of a PROPOSED entry.
-- One entry per call of a request. intent (migration 0023) is the JSON object {"target","before","prior","name"} of
-- action.Intent (NULL: none); undo_refusal is why an UNDO_REFUSED entry was refused. The journal of the panel lists
-- entries by created_at, then by rowid (insert order; 0040 kept the rowids).
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE agent_actions (
    id             TEXT    PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    thread_id      TEXT    NOT NULL REFERENCES agent_threads (id),
    turn           INTEGER NOT NULL CHECK (turn >= 1),
    call_id        TEXT    NOT NULL CHECK (length(call_id) BETWEEN 1 AND 64),
    tool           TEXT    NOT NULL CHECK (length(tool) BETWEEN 1 AND 64),
    status         TEXT    NOT NULL CHECK (status IN ('PENDING', 'DONE', 'FAILED', 'UNKNOWN', 'UNDOING', 'UNDONE', 'UNDO_REFUSED', 'PROPOSED')),
    compensation   TEXT    CHECK (compensation IS NULL OR (json_valid(compensation) AND json_type(compensation) = 'object')),
    target_version INTEGER NOT NULL DEFAULT 0 CHECK (target_version >= 0),
    proposal       TEXT    NOT NULL DEFAULT '' CHECK (length(proposal) <= 160),
    version        INTEGER NOT NULL DEFAULT 0,                                 -- optimistic concurrency (ADR-0004)
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    intent         TEXT    CHECK (intent IS NULL OR (json_valid(intent) AND json_type(intent) = 'object')),
    undo_refusal   TEXT    NOT NULL DEFAULT '' CHECK (undo_refusal IN ('', 'changed', 'result_ready', 'cannot_stop')),
    UNIQUE (thread_id, turn, call_id)
);
CREATE INDEX idx_agent_actions_thread ON agent_actions (thread_id, created_at);
CREATE INDEX idx_agent_actions_open ON agent_actions (status) WHERE status IN ('PENDING', 'UNDOING');
CREATE INDEX idx_agent_actions_created ON agent_actions (created_at);

-- ---------------------------------------------------------------------------------------------------------------
-- today_posts: the post after a lesson (aggregate agent/post). The recording is the identity, so a repeated completion
-- event adds no second post; it goes with its recording. summary_at is when the summary of transcript_id was stored,
-- homework_at when its homework was looked for (NULL: not yet). No text: the screen builds it from these facts.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE today_posts (
    recording_id  TEXT    PRIMARY KEY REFERENCES recordings (id) ON DELETE CASCADE,
    transcript_id TEXT    NOT NULL CHECK (length(transcript_id) BETWEEN 1 AND 128),
    summary_at    INTEGER,
    homework_at   INTEGER,
    version       INTEGER NOT NULL DEFAULT 0,                               -- optimistic concurrency (ADR-0004)
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    CHECK (summary_at IS NOT NULL OR homework_at IS NOT NULL)
);
CREATE INDEX idx_today_posts_updated ON today_posts (updated_at);

-- material_inspections (migration 0025): aggregate library/inspection, one row per material. text_sha256 is the digest
-- of the extracted text the outcome is for; attempt counts the reader calls for it (each refused one is charged).
-- The answer columns are suggestions only (never acted on alone) and are set exactly when status is 'done';
-- subject_id is a candidate the reader named, without a foreign key: a subject removed since is dropped when read.
CREATE TABLE material_inspections (
    material_id     TEXT    NOT NULL PRIMARY KEY REFERENCES materials (id) ON DELETE CASCADE,
    text_sha256     TEXT    NOT NULL CHECK (length(text_sha256) = 64 AND text_sha256 NOT GLOB '*[^0-9a-f]*'),
    status          TEXT    NOT NULL CHECK (status IN ('done', 'refused')),
    attempt         INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 3),
    doc_kind        TEXT    CHECK (doc_kind IS NULL OR doc_kind IN ('lecture', 'seminar', 'lab', 'homework', 'ticket_list', 'syllabus', 'other')),
    subject_id      TEXT    CHECK (subject_id IS NULL OR length(subject_id) BETWEEN 1 AND 128),
    lesson_date     TEXT    CHECK (lesson_date IS NULL OR lesson_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    has_homework    INTEGER NOT NULL DEFAULT 0 CHECK (has_homework IN (0, 1)),
    has_ticket_list INTEGER NOT NULL DEFAULT 0 CHECK (has_ticket_list IN (0, 1)),
    mentions_email  INTEGER NOT NULL DEFAULT 0 CHECK (mentions_email IN (0, 1)),
    version         INTEGER NOT NULL DEFAULT 0,                             -- optimistic concurrency (ADR-0004)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    CHECK ((status = 'done') = (doc_kind IS NOT NULL)),
    CHECK (status = 'done' OR (subject_id IS NULL AND lesson_date IS NULL AND has_homework = 0 AND has_ticket_list = 0 AND mentions_email = 0))
);

-- summaries, assignments (migration 0025): the material a summary or an assignment was made from. Its summary id is
-- "material-<material id>" (summary.IDOfMaterial); an assignment of a material is found again by (material_id,
-- source_index), as one of a transcript. A deleted material leaves the rows (ON DELETE SET NULL).
ALTER TABLE summaries ADD COLUMN material_id TEXT REFERENCES materials (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX idx_summaries_material ON summaries (material_id) WHERE material_id IS NOT NULL;
ALTER TABLE assignments ADD COLUMN material_id TEXT REFERENCES materials (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX idx_assignments_material_source ON assignments (material_id, source_index)
    WHERE material_id IS NOT NULL AND source_index IS NOT NULL;

-- works (migration 0026, rebuilt by 0031): aggregate coursework/work. Exactly one source: an assignment, a library
-- material (an uploaded assignment file) or a topic without an assignment (TASK-187: topic is the clean one-line text,
-- untrusted data; topic_request the key of the request that asked for it; topic_subject_id the subject chosen, without
-- a foreign key, so a work outlives a removed subject). A source (a topic by its request) has at most one work that
-- has not failed. round is the round of the pending or last reader call, attempt the attempt of the pending one.
-- edit_* are the student's edits of the card (NULL: the reader's value is shown). writing (migration 0028) is the
-- writing state in its canonical JSON (work.Writing.Encode), read back with work.ParseWriting and checked by the
-- aggregate; NULL before the writing starts.
CREATE TABLE works (
    id                 TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    assignment_id      TEXT    REFERENCES assignments (id) ON DELETE CASCADE,
    material_id        TEXT    REFERENCES materials (id) ON DELETE CASCADE,
    topic_request      TEXT    CHECK (topic_request IS NULL OR length(topic_request) BETWEEN 1 AND 128),
    topic              TEXT    CHECK (topic IS NULL OR length(topic) BETWEEN 1 AND 500),
    topic_subject_id   TEXT    CHECK (topic_subject_id IS NULL OR length(topic_subject_id) BETWEEN 1 AND 128),
    status             TEXT    NOT NULL CHECK (status IN ('triage', 'questions', 'review', 'confirmed', 'writing', 'done', 'helper', 'failed')),
    round              INTEGER NOT NULL CHECK (round BETWEEN 1 AND 3),
    attempt            INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 3),
    edit_work_type     TEXT    CHECK (edit_work_type IS NULL OR edit_work_type IN ('referat', 'doklad', 'essay', 'answers', 'problems', 'lab_report', 'presentation', 'other')),
    edit_volume        TEXT    CHECK (edit_volume IS NULL OR length(edit_volume) <= 200),
    edit_file_format   TEXT    CHECK (edit_file_format IS NULL OR length(edit_file_format) <= 200),
    edit_formatting    TEXT    CHECK (edit_formatting IS NULL OR length(edit_formatting) <= 200),
    edit_variant       TEXT    CHECK (edit_variant IS NULL OR length(edit_variant) <= 200),
    edit_deadline      TEXT    CHECK (edit_deadline IS NULL OR length(edit_deadline) <= 200),
    edit_sources       TEXT    CHECK (edit_sources IS NULL OR length(edit_sources) <= 200),
    writing            TEXT    CHECK (writing IS NULL OR json_valid(writing)),
    version            INTEGER NOT NULL DEFAULT 0,                          -- optimistic concurrency (ADR-0004)
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL,
    CHECK ((assignment_id IS NOT NULL) + (material_id IS NOT NULL) + (topic IS NOT NULL) = 1),
    CHECK ((topic IS NULL) = (topic_request IS NULL)),
    CHECK (topic IS NOT NULL OR topic_subject_id IS NULL)
);
CREATE UNIQUE INDEX idx_works_assignment ON works (assignment_id) WHERE assignment_id IS NOT NULL AND status <> 'failed';
CREATE UNIQUE INDEX idx_works_material ON works (material_id) WHERE material_id IS NOT NULL AND status <> 'failed';
CREATE UNIQUE INDEX idx_works_topic ON works (topic_request) WHERE topic_request IS NOT NULL AND status <> 'failed';

-- work_rounds (migration 0026): one row per answered reader call of a work, saved with it (the work carries the
-- version). answer is the reader's checked answer in its canonical JSON (work.Answer.Encode), untrusted data read back
-- with work.ParseAnswer; replies the student's resolved answers to its questions (NULL until answered or skipped).
CREATE TABLE work_rounds (
    work_id  TEXT    NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    round    INTEGER NOT NULL CHECK (round BETWEEN 1 AND 3),
    answer   TEXT    NOT NULL CHECK (json_valid(answer)),
    replies  TEXT    CHECK (replies IS NULL OR json_valid(replies)),
    PRIMARY KEY (work_id, round)
);

-- answers (migration 0027): aggregate coursework/answer, inserted once (no version: it never changes). request_key is
-- the idempotency key of the reader request, one answer per key, so the same question about the same fragments is
-- answered from here. subject_id is the subject asked about (NULL: the whole base), without a foreign key. claims,
-- missing and sources are the checked answer as JSON: untrusted model text and quotes of lessons, read back through
-- answer.Restore; a source is the place a citation opens (recording, section, index, start).
-- Migration 0030 rebuilt the table to accept the keys v2 (with the reuse scope of the request, TASK-142) next to v1.
CREATE TABLE answers (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    request_key TEXT    NOT NULL UNIQUE CHECK (length(request_key) = 71 AND request_key GLOB 'ans:v[12]:*'),
    subject_id  TEXT    CHECK (subject_id IS NULL OR length(subject_id) BETWEEN 1 AND 128),
    question    TEXT    NOT NULL CHECK (length(question) BETWEEN 1 AND 500),
    fragments   INTEGER NOT NULL CHECK (fragments BETWEEN 1 AND 40),
    claims      TEXT    NOT NULL CHECK (json_valid(claims)),
    missing     TEXT    NOT NULL CHECK (json_valid(missing)),
    sources     TEXT    NOT NULL CHECK (json_valid(sources)),
    created_at  INTEGER NOT NULL
);

-- ticket_sets (migration 0029): aggregate coursework/ticketset, saved with the optimistic-concurrency protocol of
-- ADR-0004(d). subject_id is the subject whose lessons answer the tickets, material_id the material the list was read
-- from (NULL: pasted); neither has a foreign key, so a set outlives a removed subject or file. content is the tickets
-- with their results (answer id and coverage only: the quotes and recordings stay in answers), the acceptance of a
-- cost above the cap and the file, as canonical JSON (ticketset.Content.Encode) read back with ticketset.ParseContent.
CREATE TABLE ticket_sets (
    id          TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_id  TEXT    NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    material_id TEXT    CHECK (material_id IS NULL OR length(material_id) BETWEEN 1 AND 128),
    status      TEXT    NOT NULL CHECK (status IN ('proposed', 'confirmed', 'answering', 'done', 'failed', 'cancelled')),
    content     TEXT    NOT NULL CHECK (json_valid(content)),
    version     INTEGER NOT NULL DEFAULT 0,                          -- optimistic concurrency (ADR-0004)
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_ticket_sets_subject ON ticket_sets (subject_id);

-- Migration 0032.
-- settings: 1 when the agent shows «Такого задания нет — могу сделать как свободную работу» before a work on a topic
-- (settings.FreeWorkNotice, on by default), 0 after «Больше не показывать».
ALTER TABLE settings ADD COLUMN free_work_notice INTEGER NOT NULL DEFAULT 1 CHECK (free_work_notice IN (0, 1));

-- Migration 0034.
-- proposals: the name of the copy a send_work_email attaches (proposal.CopyName, a safe file name the core made from
-- the library name, never model text); '' for the other kinds, before validation and for sends stored before 0034.
ALTER TABLE proposals ADD COLUMN attachment_name TEXT NOT NULL DEFAULT '' CHECK (length(attachment_name) <= 128);

-- Migration 0035.
-- works: the problem of the last reader call (work.Problem, a code, never model text); NULL after an answer. The time
-- the delivery of a send of the work was last SENT (ms); NULL if never.
ALTER TABLE works ADD COLUMN last_problem TEXT CHECK (last_problem IS NULL OR last_problem IN ('offline', 'quota', 'refused', 'no_text', 'source_gone'));
ALTER TABLE works ADD COLUMN sent_at INTEGER;

-- ---------------------------------------------------------------------------------------------------------------
-- notes (migration 0036): aggregate library/note. A subject that holds a note cannot be deleted (RESTRICT). The anchor
-- is one of recording (recording_id), lesson (lesson_id), day or undated; recording_id and lesson_id carry NO foreign
-- key, so the purge of a recording (ADR-0018) and a re-import of the schedule never touch a note: a gone recording or
-- lesson falls back to the day of the snapshot when read. The snapshot is anchor_date (local YYYY-MM-DD, NULL only for
-- undated), lesson_kind (a short code, '' if unknown) and lesson_title.
-- ---------------------------------------------------------------------------------------------------------------
CREATE TABLE notes (
    id            TEXT    NOT NULL PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    subject_id    TEXT    NOT NULL REFERENCES subjects (id) ON DELETE RESTRICT,
    title         TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    anchor_kind   TEXT    NOT NULL CHECK (anchor_kind IN ('recording', 'lesson', 'day', 'undated')),
    recording_id  TEXT    CHECK (recording_id IS NULL OR length(recording_id) BETWEEN 1 AND 128),
    lesson_id     TEXT    CHECK (lesson_id IS NULL OR length(lesson_id) BETWEEN 1 AND 128),
    anchor_date   TEXT    CHECK (anchor_date IS NULL OR anchor_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    lesson_kind   TEXT    NOT NULL DEFAULT '' CHECK (length(lesson_kind) <= 32),
    lesson_title  TEXT    NOT NULL DEFAULT '' CHECK (length(lesson_title) <= 200),
    status        TEXT    NOT NULL CHECK (status IN ('active', 'removed')),
    version       INTEGER NOT NULL DEFAULT 0,                             -- optimistic concurrency (ADR-0004)
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    CHECK ((anchor_kind = 'recording') = (recording_id IS NOT NULL)),
    CHECK ((anchor_kind = 'lesson') = (lesson_id IS NOT NULL)),
    CHECK ((anchor_kind = 'undated') = (anchor_date IS NULL))
);
CREATE INDEX idx_notes_subject ON notes (subject_id);
CREATE INDEX idx_notes_recording ON notes (recording_id) WHERE recording_id IS NOT NULL;
CREATE INDEX idx_notes_lesson ON notes (lesson_id) WHERE lesson_id IS NOT NULL;

-- note_parts: the materials of a note in the student's order, part of the note aggregate (no version of their own).
-- A material is soft-deleted, so one that is a part of a note cannot be deleted (RESTRICT).
CREATE TABLE note_parts (
    note_id     TEXT    NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    material_id TEXT    NOT NULL REFERENCES materials (id) ON DELETE RESTRICT,
    position    INTEGER NOT NULL CHECK (position BETWEEN 0 AND 199),
    PRIMARY KEY (note_id, material_id),
    UNIQUE (note_id, position)
);
CREATE INDEX idx_note_parts_material ON note_parts (material_id);

-- Migration 0038.
-- subjects: the teacher (subject.Teacher); both '' while none is set, never one without the other. Personal data
-- typed by the student, not a secret; never sent to the agent or the Hub (ADR-0022).
ALTER TABLE subjects ADD COLUMN teacher_name TEXT NOT NULL DEFAULT '' CHECK (length(teacher_name) <= 200);
ALTER TABLE subjects ADD COLUMN teacher_email TEXT NOT NULL DEFAULT '' CHECK (length(teacher_email) <= 254 AND (teacher_email = '') = (teacher_name = ''));

-- Migration 0039.
-- assignments: when the student deleted the assignment (ms); NULL while it is not deleted.
ALTER TABLE assignments ADD COLUMN deleted_at INTEGER;

-- Migration 0042.
-- recordings (migration 0042): origin 'captured' (every earlier row) or 'imported'; source_sha256 is the lowercase hex
-- digest of an imported file, NULL for a captured recording.
ALTER TABLE recordings ADD COLUMN origin TEXT NOT NULL DEFAULT 'captured' CHECK (origin IN ('captured', 'imported'));
ALTER TABLE recordings ADD COLUMN source_sha256 TEXT CHECK (source_sha256 IS NULL OR (length(source_sha256) = 64 AND source_sha256 NOT GLOB '*[^0-9a-f]*'));
CREATE UNIQUE INDEX idx_recordings_source_sha256 ON recordings (source_sha256) WHERE source_sha256 IS NOT NULL;
