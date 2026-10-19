package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	msqlite "modernc.org/sqlite"

	"paratech/desktop/internal/domain/shared"
)

var contractTables = []string{
	"assignments", "deliveries", "homework_draft_revisions", "homework_drafts", "jobs", "proposals", "recordings",
	"schedule_entries", "settings", "stt_models", "stt_speeds", "summaries", "transcript_parts", "transcripts",
}

// unversionedTables hold rows that are written once and never saved again, so they have no Save port and no version
// column (ADR-0004(d)). Draft versions are append-only rows below homework_drafts, which carries the version.
// Live transcript parts are replaced by their number, never saved through an aggregate (TASK-079); so are the
// measured recognition speeds (TASK-084).
var unversionedTables = map[string]bool{"homework_draft_revisions": true, "transcript_parts": true, "stt_speeds": true}

func tableNames(t *testing.T, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) []string {
	t.Helper()
	rows, err := q.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name NOT LIKE 'goose\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

func TestMigrateCreatesAllTables(t *testing.T) {
	db := openTest(t, nil)
	if got := tableNames(t, db.sql); !reflect.DeepEqual(got, contractTables) {
		t.Fatalf("tables = %v, want %v", got, contractTables)
	}
	// Every table with a Save port carries the aggregate version of ADR-0004(d).
	for _, table := range contractTables {
		if unversionedTables[table] {
			continue
		}
		var found int64
		err := db.sql.QueryRowContext(context.Background(),
			"SELECT count(*) FROM pragma_table_info(?) WHERE name = 'version' AND type = 'INTEGER' AND \"notnull\" = 1 AND dflt_value = '0'",
			table).Scan(&found)
		if err != nil || found != 1 {
			t.Errorf("table %s: version INTEGER NOT NULL DEFAULT 0 column found %d times, err %v", table, found, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	first, err := db.SchemaVersion(ctx)
	if err != nil || first != 14 {
		t.Fatalf("SchemaVersion after Open = %d, %v; want 14", first, err)
	}
	mustExec(t, db, "INSERT INTO settings (id, updated_at) VALUES (1, 5)")
	appliedBefore := scanInt(t, db.sql, "SELECT count(*) FROM goose_db_version WHERE version_id > 0")

	for i := 0; i < 2; i++ {
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("Migrate run %d: %v", i+2, err)
		}
	}
	if v, err := db.SchemaVersion(ctx); err != nil || v != first {
		t.Fatalf("SchemaVersion after re-running = %d, %v; want %d", v, err, first)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM goose_db_version WHERE version_id > 0"); n != appliedBefore {
		t.Errorf("%d applied migrations after re-running, want %d", n, appliedBefore)
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM settings"); n != 1 {
		t.Errorf("settings rows = %d after re-running, want 1 (data kept)", n)
	}
	if got := tableNames(t, db.sql); !reflect.DeepEqual(got, contractTables) {
		t.Errorf("tables after re-running = %v", got)
	}
}

func TestMigrateRefusesNewerSchema(t *testing.T) {
	db := openTest(t, nil)
	mustExec(t, db, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99, 1)")
	err := db.Migrate(context.Background())
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Migrate on a database from a newer build: %v, want ErrSchemaTooNew", err)
	}
}

// Open without Migrate still refuses a database from a newer build.
func TestOpenRefusesNewerSchemaWithAndWithoutMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	first, err := Open(context.Background(), Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, first, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99, 1)")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	for _, migrate := range []bool{false, true} {
		db, err := Open(context.Background(), Options{Path: path, Migrate: migrate})
		if err == nil {
			_ = db.Close()
		}
		if !errors.Is(err, ErrSchemaTooNew) {
			t.Errorf("Open(Migrate=%v) on a newer database: %v, want ErrSchemaTooNew", migrate, err)
		}
	}
}

// The version check of Open(Migrate=false) and SchemaVersion only read: they must not create goose_db_version.
func TestSchemaChecksHaveNoSideEffects(t *testing.T) {
	db := openTest(t, func(o *Options) { o.Migrate = false })
	const query = "SELECT count(*) FROM sqlite_master WHERE name = 'goose_db_version'"
	if n := scanInt(t, db.sql, query); n != 0 {
		t.Fatalf("goose_db_version exists after Open without Migrate")
	}
	if v, err := db.SchemaVersion(context.Background()); err != nil || v != 0 {
		t.Fatalf("SchemaVersion = %d, %v; want 0", v, err)
	}
	if n := scanInt(t, db.sql, query); n != 0 {
		t.Fatalf("goose_db_version exists after SchemaVersion")
	}
}

func TestMigrateDownAndUpAgain(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, nil)
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := tableNames(t, db.sql); len(got) != 0 {
		t.Fatalf("tables after down = %v, want none", got)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if got := tableNames(t, db.sql); !reflect.DeepEqual(got, contractTables) {
		t.Fatalf("tables after up again = %v", got)
	}
}

// Version 8 drops the notices of finished processing (TASK-065) from a database of version 7 that holds them, keeps
// the other settings, and goes back down to 7.
func TestMigrationToVersion8DropsCompletionNotices(t *testing.T) {
	ctx := context.Background()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 7); err != nil {
		t.Fatalf("up to 7: %v", err)
	}
	mustExec(t, db, `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at, finished_at)
		VALUES ('j1', 'stt', 'COMPLETED', 'key-1', 3, 0, 0, 0, 0)`)
	mustExec(t, db, `INSERT INTO job_notices (job_id, status, job_type, notified_at) VALUES ('j1', 'COMPLETED', 'stt', 5)`)
	mustExec(t, db, `INSERT INTO settings (id, recording_prompts_enabled, process_only_on_ac_power,
		notify_on_processing_complete, hub_url, recording_source_app, version, updated_at)
		VALUES (1, 0, 1, 0, 'https://hub.example', 'zoom.exe', 4, 9)`)

	if _, err := p.UpTo(ctx, 8); err != nil {
		t.Fatalf("migrate to 8: %v", err)
	}
	if v, err := db.SchemaVersion(ctx); err != nil || v != 8 {
		t.Fatalf("SchemaVersion = %d, %v; want 8", v, err)
	}
	const (
		noticesTable = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'job_notices'"
		notifyColumn = "SELECT count(*) FROM pragma_table_info('settings') WHERE name = 'notify_on_processing_complete'"
	)
	if n := scanInt(t, db.sql, noticesTable); n != 0 {
		t.Fatal("job_notices still exists at version 8")
	}
	if n := scanInt(t, db.sql, notifyColumn); n != 0 {
		t.Fatal("settings.notify_on_processing_complete still exists at version 8")
	}
	// The repository reads the current schema: the row is checked at the latest version.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate to the latest: %v", err)
	}
	got, err := NewSettingsRepository(db).Load(ctx)
	if err != nil {
		t.Fatalf("Load of a version 7 row: %v", err)
	}
	if got.RecordingPromptsEnabled() || !got.ProcessOnlyOnACPower() || got.HubURL() != "https://hub.example" ||
		got.RecordingSource() != "zoom.exe" || got.Version() != 4 {
		t.Fatalf("settings after the migration: prompts %v, AC only %v, hub %q, source %q, version %d",
			got.RecordingPromptsEnabled(), got.ProcessOnlyOnACPower(), got.HubURL(), got.RecordingSource(), got.Version())
	}
	if n := scanInt(t, db.sql, "SELECT count(*) FROM jobs WHERE id = 'j1'"); n != 1 {
		t.Fatalf("jobs rows = %d, want the job kept", n)
	}

	if _, err := p.DownTo(ctx, 7); err != nil {
		t.Fatalf("down to 7: %v", err)
	}
	if scanInt(t, db.sql, noticesTable) != 1 || scanInt(t, db.sql, notifyColumn) != 1 {
		t.Fatal("down to 7 did not restore job_notices and the setting")
	}
	if n := scanInt(t, db.sql, "SELECT notify_on_processing_complete FROM settings WHERE id = 1"); n != 1 {
		t.Fatalf("restored setting = %d, want the default 1", n)
	}
	mustExec(t, db, `INSERT INTO job_notices (job_id, status, job_type, notified_at) VALUES ('j1', 'FAILED', 'stt', 6)`)
	if _, err := db.sql.ExecContext(ctx, "UPDATE settings SET notify_on_processing_complete = 2"); !errors.Is(MapError(ctx, err), ErrConstraint) {
		t.Fatalf("restored CHECK: err = %v, want ErrConstraint", err)
	}
}

// TestSchemaConstraints checks that the CHECK, UNIQUE and foreign key rules of the contract are really in force on
// the migrated database and that violations are translated by MapError.
func TestSchemaConstraints(t *testing.T) {
	const (
		validJob = `INSERT INTO jobs (id, type, status, idempotency_key, max_attempts, next_run_at, created_at, updated_at)
			VALUES ('j1', 'stt', 'QUEUED', 'key-1', 3, 0, 0, 0)`
		validModel = `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at)
			VALUES ('m1', 'small', '1', 'models/small.bin', 10, '` + "0123456789012345678901234567890123456789012345678901234567890123" + `', 0)`
	)
	tests := []struct {
		name  string
		setup []string
		stmt  string
		want  error // nil: the statement must succeed
	}{
		{name: "valid job", stmt: validJob},
		{name: "unknown job status", stmt: strings.Replace(validJob, "'QUEUED'", "'BOGUS'", 1), want: ErrConstraint},
		{name: "job finished without finished_at", stmt: strings.Replace(validJob, "'QUEUED'", "'COMPLETED'", 1), want: ErrConstraint},
		{name: "job invalid payload json", stmt: `INSERT INTO jobs (id, type, status, idempotency_key, payload, max_attempts, next_run_at, created_at, updated_at)
			VALUES ('j1', 'stt', 'QUEUED', 'k', '{oops', 3, 0, 0, 0)`, want: ErrConstraint},
		{name: "duplicate job idempotency key", setup: []string{validJob}, stmt: strings.Replace(validJob, "'j1'", "'j2'", 1), want: shared.ErrDuplicate},
		{name: "duplicate job id", setup: []string{validJob}, stmt: strings.Replace(validJob, "'key-1'", "'key-2'", 1), want: shared.ErrDuplicate},
		{name: "model sha256 of wrong length", stmt: strings.Replace(validModel, "0123456789012345678901234567890123456789012345678901234567890123", "abc", 1), want: ErrConstraint},
		{name: "duplicate model name and version", setup: []string{validModel}, stmt: strings.Replace(validModel, "'m1'", "'m2'", 1), want: shared.ErrDuplicate},
		{name: "same model name other version", setup: []string{validModel}, stmt: strings.Replace(strings.Replace(validModel, "'m1'", "'m2'", 1), "'1'", "'2'", 1)},
		{name: "assignment of unknown lesson", stmt: `INSERT INTO assignments (id, lesson_id, title, created_at, updated_at) VALUES ('a1', 'nope', 'hw', 0, 0)`, want: shared.ErrConflict},
		{name: "assignment without lesson", stmt: `INSERT INTO assignments (id, title, created_at, updated_at) VALUES ('a1', 'hw', 0, 0)`},
		{name: "delivery of unknown assignment", stmt: `INSERT INTO deliveries (id, assignment_id, recipient, subject, artifact_ref, message_id, idempotency_key, status, confirmed_at, created_at, updated_at)
			VALUES ('d1', 'nope', 'a@b.c', 's', 'exports/x', 'msg-1', 'dk-1', 'PENDING', 0, 0, 0)`, want: shared.ErrConflict},
		{name: "settings default model unknown", stmt: `INSERT INTO settings (id, default_stt_model_id, updated_at) VALUES (1, 'nope', 0)`, want: shared.ErrConflict},
		{name: "settings singleton id", stmt: `INSERT INTO settings (id, updated_at) VALUES (2, 0)`, want: ErrConstraint},
		{name: "remove default model is restricted", setup: []string{validModel, `INSERT INTO settings (id, default_stt_model_id, updated_at) VALUES (1, 'm1', 0)`},
			stmt: `DELETE FROM stt_models WHERE id = 'm1'`, want: shared.ErrConflict},
		{name: "lesson ends before it starts", stmt: `INSERT INTO schedule_entries (id, title, starts_at, ends_at, timezone, created_at, updated_at)
			VALUES ('l1', 'math', 10, 5, 'Europe/Moscow', 0, 0)`, want: ErrConstraint},
		{name: "completed recording needs audio", stmt: `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, created_at, updated_at)
			VALUES ('r1', 'COMPLETED', 0, 1000, 'recordings/r1', 0, 0)`, want: ErrConstraint},
		{name: "recording null status", stmt: `INSERT INTO recordings (id, started_at, max_duration_ms, chunks_ref, created_at, updated_at)
			VALUES ('r1', 0, 1000, 'recordings/r1', 0, 0)`, want: ErrConstraint},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openTest(t, nil)
			for _, s := range tc.setup {
				mustExec(t, db, s)
			}
			_, err := db.sql.ExecContext(context.Background(), tc.stmt)
			got := MapError(context.Background(), err)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("statement failed: %v", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("error = %v, want errors.Is %v", got, tc.want)
			}
		})
	}
}

func TestJobAttemptsAboveMaxIsRejected(t *testing.T) {
	db := openTest(t, nil)
	_, err := db.sql.ExecContext(context.Background(), `INSERT INTO jobs
		(id, type, status, idempotency_key, attempts, max_attempts, next_run_at, created_at, updated_at)
		VALUES ('j1', 'stt', 'QUEUED', 'k', 4, 3, 0, 0, 0)`)
	if got := MapError(context.Background(), err); !errors.Is(got, ErrConstraint) {
		t.Fatalf("error = %v, want ErrConstraint", got)
	}
}

func TestForeignKeySetNullKeepsTranscript(t *testing.T) {
	db := openTest(t, nil)
	ctx := context.Background()
	mustExec(t, db, `INSERT INTO stt_models (id, name, model_version, file_ref, size_bytes, sha256, installed_at)
		VALUES ('m1', 'small', '1', 'models/small.bin', 10, '`+strings.Repeat("a", 64)+`', 0)`)
	mustExec(t, db, `INSERT INTO recordings (id, status, started_at, max_duration_ms, chunks_ref, created_at, updated_at)
		VALUES ('r1', 'FAILED', 0, 1000, 'recordings/r1', 0, 0)`)
	mustExec(t, db, `INSERT INTO transcripts (id, recording_id, model_id, model_name, model_version, text_ref, created_at)
		VALUES ('t1', 'r1', 'm1', 'small', '1', 'transcripts/t1', 0)`)

	mustExec(t, db, "DELETE FROM stt_models WHERE id = 'm1'")
	var modelID sql.NullString
	var name string
	if err := db.sql.QueryRowContext(ctx, "SELECT model_id, model_name FROM transcripts WHERE id = 't1'").Scan(&modelID, &name); err != nil {
		t.Fatal(err)
	}
	if modelID.Valid || name != "small" {
		t.Fatalf("transcript after model removal: model_id %v, model_name %q; want NULL and the pinned name kept", modelID, name)
	}
}

// ---- contract equality ----------------------------------------------------------------------------------------

// contractSchemaPath finds docs/contracts/schema.sql in the repository root (five levels above this package). When
// the docs directory is missing (paratech/ extracted into its own repository) the test skips, unless
// PARATECH_REQUIRE_DOCS=1, as in desktop/tests.
func contractSchemaPath(t *testing.T) string {
	t.Helper()
	docs := filepath.Join("..", "..", "..", "..", "..", "docs")
	if _, err := os.Stat(docs); err != nil {
		if os.Getenv("PARATECH_REQUIRE_DOCS") == "1" {
			t.Fatalf("repository-root docs/ not found at %s and PARATECH_REQUIRE_DOCS=1", docs)
		}
		t.Skipf("repository-root docs/ not found at %s (paratech/ extracted from the monorepo?); set PARATECH_REQUIRE_DOCS=1 to fail instead of skipping", docs)
	}
	path := filepath.Join(docs, "contracts", "schema.sql")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("docs/ exists but %s cannot be read: %v", path, err)
	}
	return path
}

var (
	lineComment = regexp.MustCompile(`--[^\n]*`)
	spaces      = regexp.MustCompile(`\s+`)
	punctSpace  = regexp.MustCompile(`\s*([(),])\s*`)
)

// normalizeDDL removes comments and layout so that two spellings of the same statement compare equal.
func normalizeDDL(ddl string) string {
	ddl = lineComment.ReplaceAllString(ddl, "")
	ddl = spaces.ReplaceAllString(ddl, " ")
	ddl = punctSpace.ReplaceAllString(ddl, "$1")
	return strings.TrimSpace(ddl)
}

// schemaObjects lists every schema object of a database (tables, indexes, views, triggers) with its normalized DDL and
// the column layout of the tables, ignoring goose's bookkeeping table.
func schemaObjects(t *testing.T, db *sql.DB) []string {
	t.Helper()
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master
		WHERE name NOT LIKE 'goose\_%' ESCAPE '\' AND tbl_name NOT LIKE 'goose\_%' ESCAPE '\' AND name <> 'sqlite_sequence'
		ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	var tables []string
	for rows.Next() {
		var typ, name, tbl, ddl string
		if err := rows.Scan(&typ, &name, &tbl, &ddl); err != nil {
			t.Fatal(err)
		}
		out = append(out, typ+" "+name+" ON "+tbl+": "+normalizeDDL(ddl))
		if typ == "table" {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		cols, err := db.QueryContext(ctx, `SELECT name, type, "notnull", COALESCE(dflt_value, 'NULL'), pk FROM pragma_table_info(?) ORDER BY cid`, table)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var name, typ, dflt string
			var notNull, pk int
			if err := cols.Scan(&name, &typ, &notNull, &dflt, &pk); err != nil {
				_ = cols.Close()
				t.Fatal(err)
			}
			out = append(out, strings.Join([]string{"column", table, name, typ, strconv.Itoa(notNull), dflt, strconv.Itoa(pk)}, " "))
		}
		if err := cols.Err(); err != nil {
			t.Fatal(err)
		}
		_ = cols.Close()
	}
	sort.Strings(out)
	return out
}

// openRaw opens a plain database without the adapter, to execute the contract file as it is.
func openRaw(t *testing.T) *sql.DB {
	t.Helper()
	connector, err := msqlite.NewConnector(filepath.Join(t.TempDir(), "raw.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw := sql.OpenDB(connector)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func diff(a, b []string) (onlyA, onlyB []string) {
	inB := map[string]bool{}
	for _, s := range b {
		inB[s] = true
	}
	inA := map[string]bool{}
	for _, s := range a {
		inA[s] = true
		if !inB[s] {
			onlyA = append(onlyA, s)
		}
	}
	for _, s := range b {
		if !inA[s] {
			onlyB = append(onlyB, s)
		}
	}
	return onlyA, onlyB
}

func TestMigrationMatchesContractSchema(t *testing.T) {
	schema, err := os.ReadFile(contractSchemaPath(t))
	if err != nil {
		t.Fatal(err)
	}
	raw := openRaw(t)
	if _, err := raw.ExecContext(context.Background(), string(schema)); err != nil {
		t.Fatalf("docs/contracts/schema.sql does not execute on SQLite: %v", err)
	}
	fromContract := schemaObjects(t, raw)
	if len(fromContract) < len(contractTables) {
		t.Fatalf("contract produced only %d objects, expected tables, indexes and columns", len(fromContract))
	}

	migrated := schemaObjects(t, openTest(t, nil).sql)
	onlyContract, onlyMigration := diff(fromContract, migrated)
	if len(onlyContract) > 0 || len(onlyMigration) > 0 {
		t.Fatalf("migrations and docs/contracts/schema.sql define different objects.\nonly in the contract:\n  %s\nonly in the migrations:\n  %s",
			strings.Join(onlyContract, "\n  "), strings.Join(onlyMigration, "\n  "))
	}
}

// TestContractComparisonDetectsDrift proves that the comparison above can fail: a contract with an extra index and a
// changed CHECK differs from the migrations.
func TestContractComparisonDetectsDrift(t *testing.T) {
	schema, err := os.ReadFile(contractSchemaPath(t))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		modify func(string) string
	}{
		{"extra index", func(s string) string { return s + "\nCREATE INDEX idx_extra ON jobs (type);\n" }},
		{"changed check", func(s string) string {
			return strings.Replace(s, "'WAITING_FOR_POWER'", "'WAITING_FOR_POWER', 'PAUSED'", 1)
		}},
		{"changed default", func(s string) string {
			return strings.Replace(s, "attempts         INTEGER NOT NULL DEFAULT 0", "attempts         INTEGER NOT NULL DEFAULT 1", 1)
		}},
	}
	migrated := schemaObjects(t, openTest(t, nil).sql)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modified := tc.modify(string(schema))
			if modified == string(schema) {
				t.Fatal("test bug: the modification did not change the contract text")
			}
			raw := openRaw(t)
			if _, err := raw.ExecContext(context.Background(), modified); err != nil {
				t.Fatal(err)
			}
			a, b := diff(schemaObjects(t, raw), migrated)
			if len(a) == 0 && len(b) == 0 {
				t.Fatal("comparison did not notice the drift")
			}
		})
	}
}

func TestNormalizeDDL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"CREATE TABLE a (\n  x INTEGER, -- note\n  y TEXT\n)", "CREATE TABLE a(x INTEGER,y TEXT)"},
		{"CREATE   TABLE a ( x INTEGER ,y TEXT )", "CREATE TABLE a(x INTEGER,y TEXT)"},
	}
	for _, tc := range tests {
		if got := normalizeDDL(tc.in); got != tc.want {
			t.Errorf("normalizeDDL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
