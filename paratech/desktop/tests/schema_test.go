package tests

// Contract test for docs/contracts/schema.sql (TASK-003). It keeps three things in sync without a SQL engine
// (no dependencies):
//   - the CHECK lists of the status columns equal the Go status constants of the aggregates;
//   - those lists equal the states named in PRD sections 6.2 and 7.2 (read from docs/prd, if present);
//   - the schema has the required tables and no column that could hold a secret (hard rule 4, AC-19).
//
// The files live in the repository root next to paratech/; when paratech/ is extracted into its own
// repository the tests skip. TASK-005 moves the schema into goose migrations, after which this test should
// read the migrations instead.

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

var (
	sqlComment     = regexp.MustCompile(`--[^\n]*`)
	createTable    = regexp.MustCompile(`(?s)CREATE TABLE (\w+) \((.*?)\n\);`)
	statusCheck    = regexp.MustCompile(`(?s)\bstatus\s+TEXT\s+NOT NULL\s+CHECK\s*\(\s*status\s+IN\s*\(([^)]*)\)`)
	quotedValue    = regexp.MustCompile(`'([^']*)'`)
	columnName     = regexp.MustCompile(`^\s*([a-z_][a-z0-9_]*)\s`)
	constraintWord = map[string]bool{"check": true, "unique": true, "primary": true, "foreign": true, "constraint": true}
	// Column names that must never appear: secrets live in Windows Credential Manager (ARCH-34).
	secretLike = regexp.MustCompile(`(?i)token|secret|passw|credential|api_?key|private_?key|refresh`)
)

func readSchema(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join(codeRoot(t), "..", "docs", "contracts", "schema.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("%s not found (paratech/ extracted from the repository?)", path)
		}
		t.Fatal(err)
	}
	return parseTables(string(data))
}

// parseTables returns table name -> body, comments removed.
func parseTables(sql string) map[string]string {
	sql = sqlComment.ReplaceAllString(sql, "")
	tables := map[string]string{}
	for _, m := range createTable.FindAllStringSubmatch(sql, -1) {
		tables[m[1]] = m[2]
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
	prdPath := filepath.Join(codeRoot(t), "..", "docs", "prd", "learning-assistant-prd-v0.4.md")
	data, err := os.ReadFile(prdPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("%s not found", prdPath)
		}
		t.Fatal(err)
	}
	prd := string(data)
	nonStates := map[string]bool{"MVP": true, "LLM": true} // upper-case words of those sections that are not states

	cases := []struct{ table, from, to string }{
		{"recordings", "### 6.2 ", "### 6.3 "},
		{"jobs", "### 7.2 ", "### 7.3 "},
	}
	word := regexp.MustCompile(`\b[A-Z]{3,}(?:_[A-Z]+)*\b`)
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			start := strings.Index(prd, tc.from)
			if start < 0 {
				t.Fatalf("PRD heading %q not found", tc.from)
			}
			end := strings.Index(prd[start:], tc.to)
			if end < 0 {
				t.Fatalf("PRD heading %q not found", tc.to)
			}
			seen := map[string]bool{}
			for _, w := range word.FindAllString(prd[start:start+end], -1) {
				if !nonStates[w] {
					seen[w] = true
				}
			}
			var want []string
			for w := range seen {
				want = append(want, w)
			}
			sort.Strings(want)
			if got := statusSet(tables[tc.table]); !equalStrings(got, want) {
				t.Errorf("status CHECK of %s = %v, PRD %s = %v", tc.table, got, strings.TrimSpace(tc.from), want)
			}
		})
	}
}

func TestSchemaHasNoSecretColumns(t *testing.T) {
	tables := readSchema(t)
	for table, body := range tables {
		for _, line := range strings.Split(body, "\n") {
			m := columnName.FindStringSubmatch(line)
			if m == nil || constraintWord[m[1]] {
				continue
			}
			if secretLike.MatchString(m[1]) {
				t.Errorf("table %s: column %q looks like it holds a secret; secrets live in Windows Credential Manager (hard rule 4)", table, m[1])
			}
		}
	}
}

// TestSchemaCheckersDetectProblems feeds the helpers a synthetic schema.
func TestSchemaCheckersDetectProblems(t *testing.T) {
	sql := `
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('A', 'B')), -- comment 'Z'
    refresh_token TEXT,
    CHECK (id <> '')
);
`
	tables := parseTables(sql)
	if got := statusSet(tables["jobs"]); !equalStrings(got, []string{"A", "B"}) {
		t.Errorf("statusSet = %v", got)
	}
	found := false
	for _, line := range strings.Split(tables["jobs"], "\n") {
		if m := columnName.FindStringSubmatch(line); m != nil && secretLike.MatchString(m[1]) {
			found = true
		}
	}
	if !found {
		t.Error("secret-like column refresh_token was not detected")
	}
}
