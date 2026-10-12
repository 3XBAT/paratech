package tests

// Contract test for docs/contracts/schema.sql (TASK-003). It keeps these things in sync without a SQL engine
// (no dependencies):
//   - the CHECK lists of the status columns equal the Go status constants of the aggregates;
//   - the state names of PRD sections 6.2 and 7.2 (read from their stateDiagram blocks) equal those lists;
//   - the schema has the required tables, a version column on every table with a Save port, and no table or column
//     that could hold a secret (hard rule 4, AC-19).
//
// The files live in the repository root next to paratech/. When the root docs/ directory is not found (paratech/
// was extracted into its own repository) the tests skip with a message; set PARATECH_REQUIRE_DOCS=1 (CI of the
// monorepo) to make that a failure instead of a silent skip.
//
// NOT covered here, on purpose: whether the SQL is valid for SQLite. That needs an engine, and this task adds no
// dependency. TASK-005 must execute the schema (as goose migrations) against modernc.org/sqlite in its tests, and
// move this test to read the migrations. The schema was checked by hand with the sqlite3 module of Python.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
)

// requireDocsEnv forces the docs-dependent tests to fail instead of skip when docs/ is missing.
const requireDocsEnv = "PARATECH_REQUIRE_DOCS"

var (
	sqlComment  = regexp.MustCompile(`--[^\n]*`)
	createTable = regexp.MustCompile(`(?is)CREATE TABLE (?:IF NOT EXISTS )?(\w+) \((.*?)\n\);`)
	statusCheck = regexp.MustCompile(`(?is)\bstatus\s+TEXT\s+NOT NULL\s+CHECK\s*\(\s*status\s+IN\s*\(([^)]*)\)`)
	quotedValue = regexp.MustCompile(`'([^']*)'`)
	columnName  = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s`)
	// A SQL column definition of the aggregate version, see the conventions in the schema header.
	versionColumn        = regexp.MustCompile(`(?im)^\s*version\s+INTEGER\s+NOT NULL\s+DEFAULT\s+0\b`)
	defaultModelRestrict = regexp.MustCompile(`(?is)default_stt_model_id\s+TEXT\s+REFERENCES\s+stt_models\s*\(\s*id\s*\)\s+ON\s+DELETE\s+RESTRICT`)
	constraintWord       = map[string]bool{"check": true, "unique": true, "primary": true, "foreign": true, "constraint": true}
	// Names that must never appear in a table or column name: secrets live in Windows Credential Manager (ARCH-34).
	secretLike = regexp.MustCompile(`(?i)api_?key|oauth|token|passw|secret|credential|private_?key|refresh`)
)

// docsStatus is the outcome of looking for the repository-root docs/ directory.
type docsStatus int

const (
	docsFound docsStatus = iota
	docsSkip
	docsFail
)

// locateDocs looks for <codeRoot>/../docs. When it is missing the result depends on the environment variable.
func locateDocs(root string, getenv func(string) string) (dir string, status docsStatus) {
	dir = filepath.Join(root, "..", "docs")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, docsFound
	}
	if getenv(requireDocsEnv) == "1" {
		return dir, docsFail
	}
	return dir, docsSkip
}

// docsFile returns the path of a file below the repository-root docs/ directory, skipping (or failing, see above)
// when docs/ is absent. A missing file inside an existing docs/ is always a failure.
func docsFile(t *testing.T, rel ...string) string {
	t.Helper()
	dir, status := locateDocs(codeRoot(t), os.Getenv)
	switch status {
	case docsSkip:
		t.Skipf("repository-root docs/ not found at %s (paratech/ extracted from the monorepo?); set %s=1 to fail instead of skipping", dir, requireDocsEnv)
	case docsFail:
		t.Fatalf("repository-root docs/ not found at %s and %s=1", dir, requireDocsEnv)
	}
	path := filepath.Join(append([]string{dir}, rel...)...)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("docs/ exists but %s cannot be read: %v", path, err)
	}
	return path
}

func readSchema(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(docsFile(t, "contracts", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return parseTables(string(data))
}

// parseTables returns table name -> body, comments removed.
func parseTables(sql string) map[string]string {
	sql = sqlComment.ReplaceAllString(sql, "")
	tables := map[string]string{}
	for _, m := range createTable.FindAllStringSubmatch(sql, -1) {
		tables[strings.ToLower(m[1])] = m[2]
	}
	return tables
}

// statusSet returns the values of the status CHECK list of a table body.
func statusSet(body string) []string {
	m := statusCheck.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	var out []string
	for _, v := range quotedValue.FindAllStringSubmatch(m[1], -1) {
		out = append(out, v[1])
	}
	sort.Strings(out)
	return out
}

// secretNames returns the names of the tables and columns that look like they could hold a secret.
func secretNames(tables map[string]string) []string {
	var found []string
	for table, body := range tables {
		if secretLike.MatchString(table) {
			found = append(found, "table "+table)
		}
		for _, line := range strings.Split(body, "\n") {
			m := columnName.FindStringSubmatch(line)
			if m == nil || constraintWord[strings.ToLower(m[1])] {
				continue
			}
			if secretLike.MatchString(m[1]) {
				found = append(found, "column "+table+"."+m[1])
			}
		}
	}
	sort.Strings(found)
	return found
}

var (
	mermaidBlock      = regexp.MustCompile("(?s)```mermaid\\s*\n(.*?)```")
	mermaidTransition = regexp.MustCompile(`(?m)^\s*(\[\*\]|\w+)\s*-->\s*(\[\*\]|\w+)`)
)

// diagramStates returns the sorted state names that appear in the stateDiagram blocks of a Markdown section.
// Reading the diagram, not every upper-case word of the prose, means a new acronym in the text of the PRD
// cannot produce a spurious state.
func diagramStates(section string) []string {
	seen := map[string]bool{}
	for _, block := range mermaidBlock.FindAllStringSubmatch(section, -1) {
		if !strings.Contains(block[1], "stateDiagram") {
			continue
		}
		for _, m := range mermaidTransition.FindAllStringSubmatch(block[1], -1) {
			for _, name := range m[1:3] {
				if name != "[*]" {
					seen[name] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// section returns the text from the heading that starts with from up to the heading that starts with to.
func section(md, from, to string) (string, bool) {
	start := strings.Index(md, from)
	if start < 0 {
		return "", false
	}
	end := strings.Index(md[start:], to)
	if end < 0 {
		return "", false
	}
	return md[start : start+end], true
}

func sortedStrings[S ~string](in []S) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSchemaHasRequiredTables(t *testing.T) {
	tables := readSchema(t)
	for _, name := range []string{
		"jobs", "recordings", "transcripts", "assignments", "schedule_entries", "stt_models", "deliveries", "settings",
	} {
		if _, ok := tables[name]; !ok {
			t.Errorf("table %s is missing", name)
		}
	}
}

// Every aggregate with a Save port is saved with a version compare-and-swap (ADR-0004).
func TestSchemaVersionColumns(t *testing.T) {
	tables := readSchema(t)
	for _, name := range []string{
		"jobs", "recordings", "transcripts", "assignments", "schedule_entries", "stt_models", "deliveries", "settings",
	} {
		body, ok := tables[name]
		if !ok {
			t.Errorf("table %s is missing", name)
			continue
		}
		if !versionColumn.MatchString(body) {
			t.Errorf("table %s has no `version INTEGER NOT NULL DEFAULT 0` column (optimistic concurrency, ADR-0004)", name)
		}
	}
}

// A reference that a versioned aggregate owns as state must not be cleared by the database behind its version
// (ADR-0004): the default STT model of the settings is RESTRICT, so the use case clears it through the aggregate.
func TestSchemaSettingsDefaultModelIsRestrict(t *testing.T) {
	body := readSchema(t)["settings"]
	if !defaultModelRestrict.MatchString(body) {
		t.Error("settings.default_stt_model_id must be `REFERENCES stt_models (id) ON DELETE RESTRICT` (ADR-0004)")
	}
	if defaultModelRestrict.MatchString("default_stt_model_id TEXT REFERENCES stt_models (id) ON DELETE SET NULL") {
		t.Error("defaultModelRestrict accepts ON DELETE SET NULL")
	}
}

// Lessons are deleted softly so that the calendar can still delete the event (ADR-0004).
func TestSchemaScheduleEntriesSupportSoftDelete(t *testing.T) {
	body := readSchema(t)["schedule_entries"]
	for _, col := range []string{"deleted_at", "content_revision", "calendar_synced_revision"} {
		if !regexp.MustCompile(`(?m)^\s*` + col + `\s`).MatchString(body) {
			t.Errorf("schedule_entries has no column %s", col)
		}
	}
	if regexp.MustCompile(`(?mi)^\s*calendar_event_id\s`).MatchString(body) {
		t.Error("schedule_entries has calendar_event_id, but the event id is derived from the lesson id (ADR-0004)")
	}
}

func TestSchemaStatusChecksMatchGoConstants(t *testing.T) {
	tables := readSchema(t)
	cases := []struct {
		table string
		want  []string
	}{
		{"jobs", sortedStrings(job.Statuses())},
		{"recordings", sortedStrings(recording.Statuses())},
		{"deliveries", sortedStrings(delivery.Statuses())},
	}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			body, ok := tables[tc.table]
			if !ok {
				t.Fatalf("table %s is missing", tc.table)
			}
			got := statusSet(body)
			if got == nil {
				t.Fatalf("table %s has no status CHECK (status IN (...))", tc.table)
			}
			if !equalStrings(got, tc.want) {
				t.Errorf("status CHECK of %s = %v, Go constants = %v", tc.table, got, tc.want)
			}
		})
	}
}

func TestSchemaStatesMatchPRD(t *testing.T) {
	tables := readSchema(t)
	data, err := os.ReadFile(docsFile(t, "prd", "learning-assistant-prd-v0.4.md"))
	if err != nil {
		t.Fatal(err)
	}
	prd := string(data)

	cases := []struct{ table, from, to string }{
		{"recordings", "### 6.2 ", "### 6.3 "},
		{"jobs", "### 7.2 ", "### 7.3 "},
	}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			sec, ok := section(prd, tc.from, tc.to)
			if !ok {
				t.Fatalf("PRD headings %q .. %q not found", tc.from, tc.to)
			}
			want := diagramStates(sec)
			if len(want) == 0 {
				t.Fatalf("PRD %s has no stateDiagram block", strings.TrimSpace(tc.from))
			}
			if got := statusSet(tables[tc.table]); !equalStrings(got, want) {
				t.Errorf("status CHECK of %s = %v, PRD %s diagram = %v", tc.table, got, strings.TrimSpace(tc.from), want)
			}
		})
	}
}

func TestSchemaHasNoSecretColumns(t *testing.T) {
	if found := secretNames(readSchema(t)); len(found) > 0 {
		t.Errorf("names that look like they hold a secret (secrets live in Windows Credential Manager, hard rule 4): %v", found)
	}
}

// TestSchemaCheckersDetectProblems feeds the helpers a synthetic schema.
func TestSchemaCheckersDetectProblems(t *testing.T) {
	sql := `
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('A', 'B')), -- comment 'Z'
    version INTEGER NOT NULL DEFAULT 0,
    Refresh_Token TEXT,
    API_KEY TEXT,
    "ignored" TEXT,
    CHECK (id <> '')
);
create table OAuth_Grants (
    id TEXT PRIMARY KEY,
    Password_Hash TEXT,
    client_secret TEXT,
    user_credentials TEXT
);
CREATE TABLE clean (
    id TEXT PRIMARY KEY,
    title TEXT
);
`
	tables := parseTables(sql)
	if got := statusSet(tables["jobs"]); !equalStrings(got, []string{"A", "B"}) {
		t.Errorf("statusSet = %v", got)
	}
	if !versionColumn.MatchString(tables["jobs"]) || versionColumn.MatchString(tables["clean"]) {
		t.Error("versionColumn does not tell a table with a version column from one without")
	}
	got := secretNames(tables)
	want := []string{
		"column jobs.API_KEY", "column jobs.Refresh_Token",
		"column oauth_grants.Password_Hash", "column oauth_grants.client_secret", "column oauth_grants.user_credentials",
		"table oauth_grants",
	}
	if !equalStrings(got, want) {
		t.Errorf("secretNames = %v, want %v", got, want)
	}
}

func TestDiagramStatesIgnoresProse(t *testing.T) {
	md := "### 1.1 Example\n\nThe ICS import and the MVP use LLM and HTTP, GET /x. `NOT_A_STATE` is mentioned.\n\n" +
		"```mermaid\nstateDiagram-v2\n    [*] --> NEW\n    NEW --> RUNNING : go\n    RUNNING --> DONE\n    DONE --> [*]\n```\n\n" +
		"```mermaid\nflowchart LR\n    A --> B\n```\n"
	want := []string{"DONE", "NEW", "RUNNING"}
	if got := diagramStates(md); !equalStrings(got, want) {
		t.Errorf("diagramStates = %v, want %v", got, want)
	}
	if got := diagramStates("no diagram here, ACRONYM"); len(got) != 0 {
		t.Errorf("diagramStates of prose = %v, want none", got)
	}
}

func TestLocateDocs(t *testing.T) {
	root := t.TempDir()
	codeDir := filepath.Join(root, "paratech")
	if err := os.Mkdir(codeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	noEnv := func(string) string { return "" }
	force := func(k string) string {
		if k == requireDocsEnv {
			return "1"
		}
		return ""
	}

	if _, st := locateDocs(codeDir, noEnv); st != docsSkip {
		t.Errorf("missing docs/ without the env var: status %v, want skip", st)
	}
	if _, st := locateDocs(codeDir, force); st != docsFail {
		t.Errorf("missing docs/ with %s=1: status %v, want fail", requireDocsEnv, st)
	}
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, getenv := range map[string]func(string) string{"no env": noEnv, "forced": force} {
		if _, st := locateDocs(codeDir, getenv); st != docsFound {
			t.Errorf("existing docs/ (%s): status %v, want found", name, st)
		}
	}
}
