package sqlite

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Versions around the agent chats migration (TASK-204).
const (
	beforeChats = 39
	chatsSchema = 40
)

// longSubject is 100 characters, ending in a space at character 80.
var longSubject = strings.Repeat("Матанализ ", 10)

// agentDataAt39 fills a database of version 39: threads of subject s1, of «Без предмета», of a subject removed since
// and of a subject with a long name, one updated before it was created, with requests, history and journal lines (two of the same instant).
func agentDataAt39(t *testing.T, db *DB) {
	t.Helper()
	for _, s := range []string{
		`INSERT INTO subjects (id, name, name_key, semester, created_at, updated_at) VALUES ('s1', 'Физика', 'физика', '2026-autumn', 1, 1)`,
		`INSERT INTO subjects (id, name, name_key, semester, created_at, updated_at) VALUES ('s2', '` + strings.TrimSpace(longSubject) + `', 'матанализ', '2026-autumn', 1, 1)`,
		`INSERT INTO agent_threads (id, subject_key, turns, history_from, seen, version, created_at, updated_at) VALUES
			('th-s1', 's1', 2, 1, '[{"kind":"material","id":"m1"}]', 5, 1000, 9000),
			('th-none', 'NONE', 1, 0, '[]', 2, 2000, 3000),
			('th-gone', 's-gone', 0, 0, '[]', 1, 4000, 4000),
			('th-long', 's2', 0, 0, '[]', 1, 5000, 6000),
			('th-skew', 's-skew', 0, 0, '[]', 1, 3500, 100)`,
		`INSERT INTO agent_turns (thread_id, number, status, scope, steps, planner_input, started_at, updated_at) VALUES
			('th-s1', 1, 'DONE', '{"subject":"s1"}', 1, 300, 1000, 2000),
			('th-s1', 2, 'DONE', '{"subject":"s1","screen":"subjects"}', 2, 700, 8000, 9000),
			('th-none', 1, 'DONE', '{}', 1, 100, 2000, 3000)`,
		`INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at) VALUES
			('th-s1', 1, 1, 'student', '{"text":"создай папку"}', 1000),
			('th-s1', 2, 2, 'student', '{"text":"ещё"}', 8000),
			('th-none', 1, 1, 'student', '{"text":"привет"}', 2000)`,
		`INSERT INTO agent_actions (id, thread_id, turn, call_id, tool, status, compensation, target_version, version, created_at, updated_at) VALUES
			('zz', 'th-s1', 1, 'c1', 'create_folder', 'DONE', '{"kind":"remove_folder","subject":"s1","folder":"f1"}', 1, 2, 1500, 1500),
			('aa', 'th-s1', 1, 'c2', 'create_folder', 'DONE', '{"kind":"remove_folder","subject":"s1","folder":"f2"}', 1, 2, 1500, 1500),
			('mm', 'th-none', 1, 'c1', 'create_subject', 'FAILED', NULL, 0, 2, 2500, 2500)`,
	} {
		mustExec(t, db, s)
	}
}

// migrationSteps moves a database between versions.
type migrationSteps struct{ p *goose.Provider }

func (s migrationSteps) UpTo(ctx context.Context, v int64) error {
	_, err := s.p.UpTo(ctx, v)
	return err
}

func (s migrationSteps) DownTo(ctx context.Context, v int64) error {
	_, err := s.p.DownTo(ctx, v)
	return err
}

func migratingAgentDB(t *testing.T) (*DB, migrationSteps) {
	t.Helper()
	db := openTest(t, func(o *Options) { o.Migrate = false })
	p, err := db.provider()
	if err != nil {
		t.Fatal(err)
	}
	return db, migrationSteps{p: p}
}

// Every existing thread becomes a chat titled with its subject's name, last active when it was last updated; requests,
// history and journal stay, with their ids and the journal's insert order; Get reads every chat.
func TestMigrationToChatsKeepsThreads(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db, steps := migratingAgentDB(t)
	if err := steps.UpTo(ctx, beforeChats); err != nil {
		t.Fatal(err)
	}
	agentDataAt39(t, db)
	if err := steps.UpTo(ctx, chatsSchema); err != nil {
		t.Fatalf("up to %d: %v", chatsSchema, err)
	}

	type chat struct {
		title      string
		lastActive int64
		archived   bool
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT id, title, last_active_at, archived_at IS NOT NULL FROM agent_threads`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]chat{}
	for rows.Next() {
		var id string
		var c chat
		if err := rows.Scan(&id, &c.title, &c.lastActive, &c.archived); err != nil {
			t.Fatal(err)
		}
		got[id] = c
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	want := map[string]chat{
		"th-s1":   {"Физика", 9000, false},
		"th-none": {"Без предмета", 3000, false},
		"th-gone": {"Без предмета", 4000, false},
		"th-long": {strings.TrimSpace(longSubject[:len(strings.Repeat("Матанализ ", 8))]), 6000, false},
		// Updated before it was created (0022 did not check it): last active when created, the migration goes on.
		"th-skew": {"Без предмета", 3500, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chats = %+v\nwant %+v", got, want)
	}
	if n := len([]rune(got["th-long"].title)); n != 79 {
		t.Fatalf("long title has %d characters, want 79 (cut to 80, trailing space trimmed)", n)
	}
	for query, n := range map[string]int64{
		"SELECT count(*) FROM agent_turns":                                                   3,
		"SELECT count(*) FROM agent_turns WHERE mentions = '[]' AND dropped_by = 0":          3,
		"SELECT count(*) FROM agent_steps":                                                   3,
		"SELECT count(*) FROM agent_actions":                                                 3,
		"SELECT count(*) FROM agent_actions WHERE intent IS NULL AND undo_refusal = ''":      3,
		"SELECT count(*) FROM sqlite_master WHERE name LIKE 'agent\\_%\\_v1' ESCAPE '\\'":    0,
		"SELECT count(*) FROM pragma_table_info('agent_threads') WHERE name = 'subject_key'": 0,
		"SELECT count(*) FROM pragma_foreign_key_check":                                      0,
	} {
		if got := scanInt(t, db.sql, query); got != n {
			t.Errorf("%s = %d, want %d", query, got, n)
		}
	}

	repo := NewAgentThreadRepository(db)
	for _, id := range []shared.ThreadID{"th-s1", "th-none", "th-gone", "th-long"} {
		if _, err := repo.Get(ctx, id); err != nil {
			t.Fatalf("Get(%s) after the migration: %v", id, err)
		}
	}
	th, _ := repo.Get(ctx, "th-s1")
	turn, _ := th.Turn()
	if st := th.State(); st.Version != 5 || st.Turns != 2 || st.HistoryFrom != 1 || turn.Scope().Subject().ID() != "s1" ||
		!th.Seen(agentRef(t, values.KindMaterial, "m1")) || turn.Budget().InputTokens != 700 {
		t.Fatalf("th-s1 = %+v, request %+v", st, turn)
	}
	if cur, err := repo.Current(ctx); err != nil || cur.ID() != "th-s1" {
		t.Fatalf("Current = %v, %v; want the thread updated last", cur, err)
	}
	if scope, err := repo.RequestScope(ctx, "th-s1", 1); err != nil || scope.Subject().ID() != "s1" {
		t.Fatalf("scope of the first request = %+v, %v", scope, err)
	}
	entries, err := NewAgentHistory(db, fixedNow{agentT0}).Since(ctx, "th-s1", 0, 10)
	if err != nil || len(entries) != 2 || entries[0].Entry.StudentText != "создай папку" || entries[1].Turn != 2 {
		t.Fatalf("history = %+v, %v", entries, err)
	}
	actions, err := NewAgentActionRepository(db).ListByThread(ctx, "th-s1", 10)
	if err != nil || len(actions) != 2 || actions[0].ID() != "aa" || actions[1].ID() != "zz" {
		t.Fatalf("journal of th-s1 = %v, %v; want the insert order kept (aa inserted last)", actions, err)
	}

	// A migrated chat goes on: a new request, titled chats and a second chat beside it.
	if err := th.Ask(thread.Request{Text: "дальше"}, agentAt(1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, th); err != nil {
		t.Fatalf("Save of a migrated chat: %v", err)
	}
	if th.Title() != "Физика" {
		t.Fatalf("a migrated chat was renamed by its next request: %q", th.Title())
	}
}

// Down restores the old tables: each chat goes back to the subject key of its last request ('NONE' without one), only
// the newest chat of a key survives with its requests, history and journal; Up again titles the threads anew.
func TestMigrationFromChatsDown(t *testing.T) {
	t.Parallel() // uses a database of its own
	ctx := context.Background()
	db, steps := migratingAgentDB(t)
	if err := steps.UpTo(ctx, beforeChats); err != nil {
		t.Fatal(err)
	}
	agentDataAt39(t, db)
	if err := steps.UpTo(ctx, chatsSchema); err != nil {
		t.Fatal(err)
	}
	// A newer chat whose request is in s1 too: it wins the key s1 on Down, th-s1 is lost.
	repo := NewAgentThreadRepository(db)
	newer, err := thread.New("th-new", agentT0)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := values.NewScope(agentRef(t, values.KindSubject, "s1"), "", values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	mention := agentMention(t, values.KindLesson, "l1", "Лекция 1")
	if err := newer.Ask(thread.Request{Scope: s1, Mentions: []values.Mention{mention}, Text: "по физике"}, agentAt(1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, newer); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO agent_steps (thread_id, seq, turn, entry, body, created_at) VALUES ('th-new', 1, 1, 'student', '{"text":"по физике"}', 1)`)

	if err := steps.DownTo(ctx, beforeChats); err != nil {
		t.Fatalf("down to %d: %v", beforeChats, err)
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT id, subject_key FROM agent_threads ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			t.Fatal(err)
		}
		keys[id] = key
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	// th-none, th-gone, th-skew and th-long have no subject in their last request: one 'NONE' survives, the newest (th-long).
	if want := map[string]string{"th-new": "s1", "th-long": "NONE"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("threads after Down = %v, want %v", keys, want)
	}
	for query, n := range map[string]int64{
		"SELECT count(*) FROM agent_turns":   1,
		"SELECT count(*) FROM agent_steps":   1,
		"SELECT count(*) FROM agent_actions": 0,
		"SELECT count(*) FROM pragma_table_info('agent_turns') WHERE name IN ('mentions', 'dropped_by')": 0,
		"SELECT count(*) FROM pragma_foreign_key_check":                                                  0,
	} {
		if got := scanInt(t, db.sql, query); got != n {
			t.Errorf("%s = %d, want %d", query, got, n)
		}
	}
	// The old UNIQUE key is in force again.
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO agent_threads (id, subject_key, created_at, updated_at) VALUES ('x', 's1', 0, 0)`); err == nil {
		t.Fatal("a second thread of key s1 was accepted after Down")
	}

	if err := steps.UpTo(ctx, chatsSchema); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if got := scanString(t, db, `SELECT title FROM agent_threads WHERE id = 'th-new'`); got != "Физика" {
		t.Fatalf("title after up again = %q", got)
	}
	if _, err := repo.Get(ctx, "th-new"); err != nil {
		t.Fatalf("Get after up again: %v", err)
	}
}
