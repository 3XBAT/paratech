package sqlite

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/summary"
)

// TASK-101: a sectioned summary is stored with its sections and read back as it was; body keeps its plain text.
func TestSummarySectionsRoundTrip(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	c, err := summary.NewSectionedContent([]summary.Section{
		{Heading: "Реки", Text: "Реки текут.\n\nВ море.", Points: []string{"Исток", "Устье"}},
		{Heading: "Озёра", Text: "Озёра стоят."},
	}, []string{"Вода"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := summary.New(summary.Spec{Transcript: "t1", Lesson: "l1", PromptVersion: 5, Content: c}, asgT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.summaries.Save(ctx, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := e.summaries.Get(ctx, s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.State(), s.State()) || !got.Content().Equal(c) {
		t.Fatalf("got %+v, want %+v", got.State(), s.State())
	}

	var body string
	var sections sql.NullString
	if err := e.db.sql.QueryRowContext(ctx, "SELECT body, sections FROM summaries WHERE id = 't1'").Scan(&body, &sections); err != nil {
		t.Fatal(err)
	}
	const wantJSON = `[{"heading":"Реки","text":"Реки текут.\n\nВ море.","points":["Исток","Устье"]},` +
		`{"heading":"Озёра","text":"Озёра стоят.","points":[]}]`
	if body != c.Text() || sections.String != wantJSON {
		t.Fatalf("stored body %q, sections %q", body, sections.String)
	}

	// A plain content (as the summaries made before) clears the sections.
	plain, err := summary.NewContent("Просто текст.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := got.Replace(plain, 5, asgT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := e.summaries.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, err := e.summaries.Get(ctx, s.ID())
	if err != nil || again.Content().Sectioned() || !again.Content().Equal(plain) {
		t.Fatalf("after a plain Replace: %+v, %v", again, err)
	}
}

// A row stored before migration 0020 (no sections) reads as before: one section without heading.
func TestSummaryWithoutSectionsReadsAsBefore(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	mustExec(t, e.db, `INSERT INTO summaries (id, transcript_id, body, key_points, prompt_version, version, created_at, updated_at)
		VALUES ('t1', 't1', 'Про реки.', '["Реки"]', 4, 1, 1, 1)`)
	got, err := e.summaries.Get(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	c := got.Content()
	if c.Sectioned() || c.Text() != "Про реки." || !reflect.DeepEqual(c.Sections(), []summary.Section{{Text: "Про реки."}}) ||
		!reflect.DeepEqual(c.KeyPoints(), []string{"Реки"}) {
		t.Fatalf("legacy summary = %+v", got.State())
	}
}

func TestSummaryCorruptSections(t *testing.T) {
	cases := map[string]string{
		"empty array":     `[]`,
		"unknown field":   `[{"heading":"H","text":"T","points":[],"link":"x"}]`,
		"no heading":      `[{"heading":"","text":"T","points":[]}]`,
		"no text":         `[{"heading":"H","text":"","points":[]}]`,
		"not sections":    `["H"]`,
		"trailing data":   `[{"heading":"H","text":"T","points":[]}] []`,
		"bad point":       `[{"heading":"H","text":"T","points":["a\nb"]}]`,
		"wrong item type": `[{"heading":1,"text":"T","points":[]}]`,
	}
	for name, sections := range cases {
		t.Run(name, func(t *testing.T) {
			e, ctx := newCourseworkEnv(t), context.Background()
			if _, err := e.db.sql.ExecContext(ctx, `INSERT INTO summaries
				(id, transcript_id, body, sections, key_points, prompt_version, version, created_at, updated_at)
				VALUES ('t1', 't1', 'x', ?, '[]', 5, 1, 1, 1)`, sections); err != nil {
				// The column CHECK refuses what is not a JSON array; that is as good as a refused read.
				return
			}
			if _, err := e.summaries.Get(ctx, "t1"); err == nil {
				t.Fatal("Get of a row with corrupt sections succeeded")
			}
		})
	}

	// The column holds JSON arrays only.
	e, ctx := newCourseworkEnv(t), context.Background()
	for _, bad := range []string{`{}`, `null`, `not json`} {
		if _, err := e.db.sql.ExecContext(ctx, `INSERT INTO summaries
			(id, transcript_id, body, sections, key_points, prompt_version, version, created_at, updated_at)
			VALUES ('t1', 't1', 'x', ?, '[]', 5, 1, 1, 1)`, bad); err == nil {
			t.Errorf("sections %q stored", bad)
		}
	}
}
