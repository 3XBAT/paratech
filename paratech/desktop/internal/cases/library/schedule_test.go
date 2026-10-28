package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/services/schedulesubjects"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// scheduleRows: two spellings of one subject, a new one, a spelling for the existing «История» and that subject's own
// alias «История России» (a string of the schedule as well, so it is the trailing alias a retry must keep).
func scheduleRows() []schedulesubjects.Row {
	return []schedulesubjects.Row{
		{Name: "Матанализ (лек.)", Lessons: schedulesubjects.Kinds{Unknown: 4}},
		{Name: "матанализ, сем.", Lessons: schedulesubjects.Kinds{Seminar: 2}},
		{Name: "Физика", Lessons: schedulesubjects.Kinds{Lecture: 3}, Recordings: 1},
		{Name: "История (лек.)", Lessons: schedulesubjects.Kinds{Lecture: 2}},
		{Name: "История России", Lessons: schedulesubjects.Kinds{Seminar: 1}},
	}
}

// scheduleEnv is an env with «История» (alias «История России») and the rows above.
func scheduleEnv(t *testing.T) (*env, *subject.Subject) {
	t.Helper()
	e := newEnv(t)
	hist := createSubject(t, e, "История", "История России")
	e.strings.rows = scheduleRows()
	return e, hist
}

func propose(t *testing.T, e *env) ScheduleProposal {
	t.Helper()
	p, err := e.svc.ScheduleSubjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func groupNamed(t *testing.T, p ScheduleProposal, name string) schedulesubjects.Group {
	t.Helper()
	i := slices.IndexFunc(p.Groups, func(g schedulesubjects.Group) bool { return g.Name == name })
	if i < 0 {
		t.Fatalf("no group %q in %+v", name, p.Groups)
	}
	return p.Groups[i]
}

// failSave makes the n-th next Save of the subjects fail, and every call after it until err is cleared.
func failSave(f *fakeSubjects, n int, err error) {
	count := 0
	var hook func()
	hook = func() {
		if count++; count == n {
			f.err = err
			return
		}
		f.beforeSave = hook
	}
	f.beforeSave = hook
}

func storedSubject(t *testing.T, id shared.SubjectID, name string, aliases ...string) subject.State {
	t.Helper()
	s, err := subject.New(subject.Spec{ID: id, Name: name, Aliases: aliases, Semester: values.SemesterOf(t0)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	st := s.State()
	st.Version = 1
	return st
}

func TestScheduleSubjectsToken(t *testing.T) {
	ctx := context.Background()
	e, _ := scheduleEnv(t)
	p := propose(t, e)
	if !validScheduleToken(p.Token) || len(p.Groups) != 3 || p.Omitted != 0 {
		t.Fatalf("proposal = %+v", p)
	}
	if again := propose(t, e); again.Token != p.Token {
		t.Fatal("the same state gave another token")
	}
	if g := groupNamed(t, p, "История"); g.Subject == "" || !reflect.DeepEqual(g.Aliases, []string{"История (лек.)"}) {
		t.Fatalf("История group = %+v", g)
	}

	e.strings.rows[2].Recordings++ // the counts are part of the proposal
	if propose(t, e).Token == p.Token {
		t.Fatal("other counts kept the token")
	}
	e.strings.rows = scheduleRows()
	createSubject(t, e, "Химия")
	if propose(t, e).Token == p.Token {
		t.Fatal("another subject kept the token")
	}

	t.Run("empty", func(t *testing.T) {
		e := newEnv(t)
		p := propose(t, e)
		if len(p.Groups) != 0 || !validScheduleToken(p.Token) {
			t.Fatalf("empty proposal = %+v", p)
		}
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
		if err != nil || !reflect.DeepEqual(res, ScheduleResult{}) {
			t.Fatalf("CreateFromSchedule(empty) = %+v, %v", res, err)
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		e, _ := scheduleEnv(t)
		e.strings.err = errors.New("disk")
		if _, err := e.svc.ScheduleSubjects(ctx); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("ScheduleSubjects = %v, want ErrUnavailable", err)
		}
	})
}

func TestCreateFromSchedule(t *testing.T) {
	e, hist := scheduleEnv(t)
	ctx := context.Background()
	p := propose(t, e)
	matan, phys := groupNamed(t, p, "Матанализ"), groupNamed(t, p, "Физика")

	res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := ScheduleResult{
		Created: []ScheduleCreated{
			{ID: schedulesubjects.DeriveSubjectID(p.Token, matan.Key), Key: matan.Key, Name: "Матанализ"},
			{ID: schedulesubjects.DeriveSubjectID(p.Token, phys.Key), Key: phys.Key, Name: "Физика"},
		},
		Aliased: []ScheduleAliased{{Subject: hist.ID(), Aliases: []string{"История (лек.)"}}},
	}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("CreateFromSchedule = %+v\nwant %+v", res, want)
	}

	m, err := e.subjects.Get(ctx, want.Created[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Folders()) != len(subject.Systems()) || len(m.Aliases()) != 2 || m.Semester() != values.SemesterOf(t0) {
		t.Fatalf("created subject: %d folders, aliases %v, %s", len(m.Folders()), names(m.Aliases()), m.Semester().Code())
	}
	h, _ := e.subjects.Get(ctx, hist.ID())
	if got := names(h.Aliases()); !reflect.DeepEqual(got, []string{"История России", "История (лек.)"}) {
		t.Fatalf("История aliases = %v", got)
	}

	// The same «Да» again (a retry after a lost answer) creates nothing more and answers the same.
	again, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("retry = %+v, %v\nwant %+v", again, err, want)
	}
	if len(e.subjects.rows) != 3 {
		t.Fatalf("%d subjects after the retry, want 3", len(e.subjects.rows))
	}
	h, _ = e.subjects.Get(ctx, hist.ID())
	if len(h.Aliases()) != 2 {
		t.Fatalf("История aliases after the retry = %v", names(h.Aliases()))
	}

	// After it is done, the schedule proposes nothing more.
	if left := propose(t, e); len(left.Groups) != 0 {
		t.Fatalf("left after the package: %+v", left.Groups)
	}
}

func names(ns []subject.Name) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.String())
	}
	return out
}

func TestCreateFromScheduleChoices(t *testing.T) {
	ctx := context.Background()

	t.Run("rename", func(t *testing.T) {
		e, _ := scheduleEnv(t)
		p := propose(t, e)
		matan := groupNamed(t, p, "Матанализ")
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, []schedulesubjects.Choice{
			{Group: matan.Key, Name: "Математический анализ"},
			{Group: groupNamed(t, p, "Физика").Key, Action: schedulesubjects.GroupSkip},
			{Group: groupNamed(t, p, "История").Key, Action: schedulesubjects.GroupSkip},
		})
		want := ScheduleResult{Created: []ScheduleCreated{
			{ID: schedulesubjects.DeriveSubjectID(p.Token, matan.Key), Key: matan.Key, Name: "Математический анализ"},
		}}
		if err != nil || !reflect.DeepEqual(res, want) {
			t.Fatalf("CreateFromSchedule(rename) = %+v, %v\nwant %+v", res, err, want)
		}
		got, _ := e.subjects.Get(ctx, want.Created[0].ID)
		if got.Name().String() != "Математический анализ" || len(got.Aliases()) != 2 {
			t.Fatalf("renamed subject %v with aliases %v", got, names(got.Aliases()))
		}
	})

	for _, tc := range []struct {
		name    string
		token   func(p ScheduleProposal) string
		choices func(p ScheduleProposal) []schedulesubjects.Choice
		want    error
	}{
		{"malformed token", func(ScheduleProposal) string { return "abc" }, nil, ErrInvalidRequest},
		{"upper-case token", func(p ScheduleProposal) string { return strings.ToUpper(p.Token) }, nil, ErrInvalidRequest},
		{"token of another state", func(ScheduleProposal) string { return strings.Repeat("0", 64) }, nil, ErrStale},
		{"unknown group", nil, func(ScheduleProposal) []schedulesubjects.Choice {
			return []schedulesubjects.Choice{{Group: "нет такой"}}
		}, ErrInvalidRequest},
		{"repeated choice", nil, func(p ScheduleProposal) []schedulesubjects.Choice {
			return []schedulesubjects.Choice{{Group: p.Groups[0].Key}, {Group: p.Groups[0].Key}}
		}, ErrInvalidRequest},
		{"rename to a taken name", nil, func(p ScheduleProposal) []schedulesubjects.Choice {
			return []schedulesubjects.Choice{{Group: p.Groups[0].Key, Name: "история"}}
		}, ErrNameTaken},
		{"rename too long", nil, func(p ScheduleProposal) []schedulesubjects.Choice {
			return []schedulesubjects.Choice{{Group: p.Groups[0].Key, Name: strings.Repeat("я", 500)}}
		}, ErrInvalidName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := scheduleEnv(t)
			p := propose(t, e)
			token, choices := p.Token, []schedulesubjects.Choice(nil)
			if tc.token != nil {
				token = tc.token(p)
			}
			if tc.choices != nil {
				choices = tc.choices(p)
			}
			res, err := e.svc.CreateFromSchedule(ctx, token, choices)
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(res, ScheduleResult{}) {
				t.Fatalf("CreateFromSchedule = %+v, %v; want %v", res, err, tc.want)
			}
			if len(e.subjects.rows) != 1 {
				t.Fatalf("%d subjects after a refusal, want 1", len(e.subjects.rows))
			}
		})
	}
}

func TestCreateFromScheduleStale(t *testing.T) {
	ctx := context.Background()
	for name, change := range map[string]func(t *testing.T, e *env, hist *subject.Subject){
		"a subject was created": func(t *testing.T, e *env, _ *subject.Subject) { createSubject(t, e, "Химия") },
		"an alias was added": func(t *testing.T, e *env, hist *subject.Subject) {
			if _, err := e.svc.AddAlias(ctx, hist.ID(), "Отечественная история"); err != nil {
				t.Fatal(err)
			}
		},
		"the schedule changed": func(_ *testing.T, e *env, _ *subject.Subject) { e.strings.rows = e.strings.rows[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			e, hist := scheduleEnv(t)
			p := propose(t, e)
			change(t, e, hist)
			before := len(e.subjects.rows)
			if _, err := e.svc.CreateFromSchedule(ctx, p.Token, nil); !errors.Is(err, ErrStale) {
				t.Fatalf("CreateFromSchedule = %v, want ErrStale", err)
			}
			if len(e.subjects.rows) != before {
				t.Fatal("a stale package created subjects")
			}
		})
	}
}

// A failure at any save keeps what was done; the retry with the same token does the rest and answers as one clean run.
func TestCreateFromScheduleRetryAfterFailure(t *testing.T) {
	ctx := context.Background()
	clean, _ := scheduleEnv(t)
	want, err := clean.svc.CreateFromSchedule(ctx, propose(t, clean).Token, nil)
	if err != nil {
		t.Fatal(err)
	}

	for failAt := 1; failAt <= 3; failAt++ { // two creates, then the alias of «История»
		t.Run(fmt.Sprintf("save %d fails", failAt), func(t *testing.T) {
			e, hist := scheduleEnv(t)
			p := propose(t, e)
			failSave(e.subjects, failAt, errors.New("disk full"))
			part, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("CreateFromSchedule = %v, want ErrUnavailable", err)
			}
			if len(part.Created)+len(part.Aliased) != failAt-1 || len(e.subjects.rows) != min(failAt, 3) {
				t.Fatalf("done before the failure: %+v, %d subjects", part, len(e.subjects.rows))
			}

			e.subjects.err = nil
			e.clock.now = t0.Add(time.Hour)
			res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
			if err != nil || !reflect.DeepEqual(res, want) {
				t.Fatalf("retry = %+v, %v\nwant %+v", res, err, want)
			}
			h, _ := e.subjects.Get(ctx, hist.ID())
			if len(e.subjects.rows) != 3 || len(h.Aliases()) != 2 {
				t.Fatalf("after the retry: %d subjects, История aliases %v", len(e.subjects.rows), names(h.Aliases()))
			}
		})
	}
}

func TestCreateFromScheduleSkips(t *testing.T) {
	ctx := context.Background()

	t.Run("name taken meanwhile", func(t *testing.T) {
		e, hist := scheduleEnv(t)
		p := propose(t, e)
		matan := groupNamed(t, p, "Матанализ")
		e.subjects.beforeSave = func() { e.subjects.rows["other"] = storedSubject(t, "other", "Матанализ") }
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(res.Skipped, []ScheduleSkipped{{Key: matan.Key, Reason: ReasonNameTaken}}) ||
			len(res.Created) != 1 || len(res.Aliased) != 1 || res.Aliased[0].Subject != hist.ID() {
			t.Fatalf("CreateFromSchedule = %+v", res)
		}
	})

	t.Run("alias taken meanwhile", func(t *testing.T) {
		e, hist := scheduleEnv(t)
		p := propose(t, e)
		saves := 0
		var hook func()
		hook = func() {
			if saves++; saves == 3 {
				e.subjects.rows["other"] = storedSubject(t, "other", "Древняя история", "История (лек.)")
				return
			}
			e.subjects.beforeSave = hook
		}
		e.subjects.beforeSave = hook
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
		if err != nil || !reflect.DeepEqual(res.Skipped, []ScheduleSkipped{{Subject: hist.ID(), Reason: ReasonNameTaken}}) ||
			len(res.Created) != 2 || len(res.Aliased) != 0 {
			t.Fatalf("CreateFromSchedule = %+v, %v", res, err)
		}
	})

	t.Run("too many aliases", func(t *testing.T) {
		e := newEnv(t)
		var aliases []string
		for i := range subject.MaxAliases {
			aliases = append(aliases, fmt.Sprintf("Ф-%02d", i))
		}
		phys := createSubject(t, e, "Физика", aliases...)
		e.strings.rows = []schedulesubjects.Row{
			{Name: "Физика (лек.)", Lessons: schedulesubjects.Kinds{Lecture: 2}},
			{Name: "Химия", Lessons: schedulesubjects.Kinds{Lecture: 1}},
		}
		p := propose(t, e)
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
		if err != nil || !reflect.DeepEqual(res.Skipped, []ScheduleSkipped{{Subject: phys.ID(), Reason: ReasonTooManyAliases}}) ||
			len(res.Created) != 1 {
			t.Fatalf("CreateFromSchedule = %+v, %v", res, err)
		}
		got, _ := e.subjects.Get(ctx, phys.ID())
		if len(got.Aliases()) != subject.MaxAliases {
			t.Fatalf("Физика has %d aliases", len(got.Aliases()))
		}
	})

	t.Run("subject removed meanwhile", func(t *testing.T) {
		e, hist := scheduleEnv(t)
		p := propose(t, e)
		saves := 0
		var hook func()
		hook = func() {
			if saves++; saves == 2 {
				delete(e.subjects.rows, hist.ID()) // before the second create is stored; the alias comes after
				return
			}
			e.subjects.beforeSave = hook
		}
		e.subjects.beforeSave = hook
		res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
		if err != nil || !reflect.DeepEqual(res.Skipped, []ScheduleSkipped{{Subject: hist.ID(), Reason: ReasonSubjectGone}}) {
			t.Fatalf("CreateFromSchedule = %+v, %v", res, err)
		}
	})
}

func TestUndoFromSchedule(t *testing.T) {
	ctx := context.Background()
	e, hist := scheduleEnv(t)
	notes := &fakeNotes{of: map[shared.SubjectID]int{}}
	withNotes(t, e, notes)
	p := propose(t, e)
	res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
	if err != nil || len(res.Created) != 2 {
		t.Fatalf("CreateFromSchedule = %+v, %v", res, err)
	}
	matan, phys := res.Created[0], res.Created[1]

	t.Run("refused", func(t *testing.T) {
		for name, bad := range map[string]struct {
			token string
			res   ScheduleResult
		}{
			"malformed token":        {"zz", res},
			"foreign subject":        {p.Token, ScheduleResult{Created: []ScheduleCreated{{ID: hist.ID(), Key: "история"}}}},
			"key of another subject": {p.Token, ScheduleResult{Created: []ScheduleCreated{{ID: matan.ID, Key: phys.Key}}}},
			"no key":                 {p.Token, ScheduleResult{Created: []ScheduleCreated{{ID: matan.ID}}}},
			"key too long": {p.Token, ScheduleResult{Created: []ScheduleCreated{{
				ID: schedulesubjects.DeriveSubjectID(p.Token, strings.Repeat("я", 500)), Key: strings.Repeat("я", 500),
			}}}},
			"too many subjects": {p.Token, ScheduleResult{Created: slices.Repeat([]ScheduleCreated{matan}, maxScheduleRows+1)}},
			"own alias of the student": {p.Token, ScheduleResult{Aliased: []ScheduleAliased{
				{Subject: hist.ID(), Aliases: []string{"История России"}},
			}}},
			"alias of another subject": {p.Token, ScheduleResult{Aliased: []ScheduleAliased{
				{Subject: matan.ID, Aliases: []string{"История (лек.)"}},
			}}},
			"empty aliases": {p.Token, ScheduleResult{Aliased: []ScheduleAliased{{Subject: hist.ID()}}}},
		} {
			if _, err := e.svc.UndoFromSchedule(ctx, bad.token, bad.res); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("%s: UndoFromSchedule = %v, want ErrInvalidRequest", name, err)
			}
		}
		if len(e.subjects.rows) != 3 {
			t.Fatalf("a refused undo removed subjects: %d left", len(e.subjects.rows))
		}
	})

	// «Физика» got a file, «Матанализ» stays empty.
	m := importText(t, e, "конспект")
	if _, err := e.svc.Place(ctx, m.ID(), phys.ID, ""); err != nil {
		t.Fatal(err)
	}
	undo, err := e.svc.UndoFromSchedule(ctx, p.Token, res)
	want := ScheduleUndo{
		Removed:   []shared.SubjectID{matan.ID},
		Kept:      []ScheduleKept{{ID: phys.ID, Name: "Физика", Reason: ReasonHasFiles}},
		Unaliased: []ScheduleAliased{{Subject: hist.ID(), Aliases: []string{"История (лек.)"}}},
	}
	if err != nil || !reflect.DeepEqual(undo, want) {
		t.Fatalf("UndoFromSchedule = %+v, %v\nwant %+v", undo, err, want)
	}
	h, _ := e.subjects.Get(ctx, hist.ID())
	if got := names(h.Aliases()); !reflect.DeepEqual(got, []string{"История России"}) {
		t.Fatalf("История aliases after the undo = %v", got)
	}
	if _, ok := e.subjects.rows[matan.ID]; ok {
		t.Fatal("the empty subject was not removed")
	}

	// Again: the removed one counts as removed, nothing else changes.
	if again, err := e.svc.UndoFromSchedule(ctx, p.Token, res); err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("undo again = %+v, %v", again, err)
	}

	t.Run("notes", func(t *testing.T) {
		notes.of[phys.ID] = 1
		undo, err := e.svc.UndoFromSchedule(ctx, p.Token, ScheduleResult{Created: []ScheduleCreated{phys}})
		if err != nil || !reflect.DeepEqual(undo.Kept, []ScheduleKept{{ID: phys.ID, Name: "Физика", Reason: ReasonHasNotes}}) {
			t.Fatalf("UndoFromSchedule(notes) = %+v, %v", undo, err)
		}
	})
}

func TestUndoFromScheduleStale(t *testing.T) {
	ctx := context.Background()
	e, hist := scheduleEnv(t)
	p := propose(t, e)
	res, err := e.svc.CreateFromSchedule(ctx, p.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	createSubject(t, e, "Химия")
	if _, err := e.svc.UndoFromSchedule(ctx, p.Token, res); !errors.Is(err, ErrStale) {
		t.Fatalf("UndoFromSchedule = %v, want ErrStale", err)
	}
	h, _ := e.subjects.Get(ctx, hist.ID())
	if len(e.subjects.rows) != 4 || len(h.Aliases()) != 2 {
		t.Fatal("a stale undo changed the library")
	}

	// Without spellings to take back the subjects need no proposal: their IDs prove they are of the package.
	undo, err := e.svc.UndoFromSchedule(ctx, p.Token, ScheduleResult{Created: res.Created})
	if err != nil || len(undo.Removed) != 2 {
		t.Fatalf("UndoFromSchedule(subjects only) = %+v, %v", undo, err)
	}
}

func TestCapScheduleRows(t *testing.T) {
	var rows []schedulesubjects.Row
	for i := range maxScheduleRows + 5 {
		rows = append(rows, schedulesubjects.Row{Name: fmt.Sprintf("r%03d", i), Lessons: schedulesubjects.Kinds{Lecture: i / 2}})
	}
	kept, omitted := capScheduleRows(rows)
	if omitted != 5 || len(kept) != maxScheduleRows {
		t.Fatalf("capScheduleRows: %d kept, %d omitted", len(kept), omitted)
	}
	// r000-r003 have 0 and 1 uses; of r004 and r005 (2 uses each) the first by name stays.
	for _, r := range kept {
		if slices.Contains([]string{"r000", "r001", "r002", "r003", "r005"}, r.Name) {
			t.Fatalf("a least used row %q was kept", r.Name)
		}
	}
	reversed := slices.Clone(rows)
	slices.Reverse(reversed)
	if again, _ := capScheduleRows(reversed); !reflect.DeepEqual(again, kept) {
		t.Fatal("the cap depends on the order of the rows")
	}
	if k, o := capScheduleRows(rows[:3]); len(k) != 3 || o != 0 {
		t.Fatalf("under the cap: %d kept, %d omitted", len(k), o)
	}
}

func TestScheduleSubjectsRowCap(t *testing.T) {
	e := newEnv(t)
	letters := []rune("абвгдежзиклмнопрстуф")
	var rows []schedulesubjects.Row
	for i := range maxScheduleRows + 5 {
		name := fmt.Sprintf("Курс %c%c%c", letters[i%20], letters[(i/20)%20], letters[(i/7)%20])
		rows = append(rows, schedulesubjects.Row{Name: name, Lessons: schedulesubjects.Kinds{Lecture: 1 + i}})
	}
	e.strings.rows = rows
	p := propose(t, e)
	if p.Omitted != 5 {
		t.Fatalf("Omitted = %d, want 5", p.Omitted)
	}
	for _, g := range p.Groups {
		for _, m := range g.Members {
			for _, s := range m.Spellings {
				if i := slices.IndexFunc(rows[:5], func(r schedulesubjects.Row) bool { return r.Name == s }); i >= 0 {
					t.Fatalf("the least used row %d is in the proposal", i)
				}
			}
		}
	}
}

// Schedule strings never reach the log or an error (ADR-0022).
func TestScheduleStringsStayOutOfLogsAndErrors(t *testing.T) {
	ctx := context.Background()
	e, _ := scheduleEnv(t)
	var buf bytes.Buffer
	svc, err := New(Deps{
		Subjects: e.subjects, Materials: e.materials, Blobs: e.blobs, Lessons: e.lessons, Strings: e.strings,
		Clock: e.clock, IDs: &seqIDs{n: 100}, Log: slog.New(slog.NewTextHandler(&buf, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.ScheduleSubjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var errs []error
	_, err = svc.CreateFromSchedule(ctx, p.Token, []schedulesubjects.Choice{{Group: "Матанализ (лек.)"}})
	errs = append(errs, err)
	failSave(e.subjects, 1, errors.New("disk"))
	_, err = svc.CreateFromSchedule(ctx, p.Token, nil)
	errs = append(errs, err)
	e.subjects.err = nil
	res, err := svc.CreateFromSchedule(ctx, p.Token, nil)
	errs = append(errs, err)
	_, err = svc.UndoFromSchedule(ctx, p.Token, res)
	errs = append(errs, err)

	text := buf.String()
	for _, err := range errs {
		if err != nil {
			text += err.Error()
		}
	}
	for _, r := range scheduleRows() {
		for _, part := range strings.Fields(r.Name) {
			if len([]rune(part)) > 3 && strings.Contains(strings.ToLower(text), strings.ToLower(strings.Trim(part, ",()."))) {
				t.Fatalf("%q reached the log or an error:\n%s", part, text)
			}
		}
	}
	if !strings.Contains(text, "created=") {
		t.Fatalf("no counts logged:\n%s", text)
	}
}
