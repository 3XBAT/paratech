package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/services/schedulesubjects"
	"paratech/desktop/internal/domain/shared"
)

var (
	tokA = strings.Repeat("a", 64)
	tokB = strings.Repeat("b", 64)
)

// fakeScheduleSubjects is subjects from the schedule; block, when set, holds CreateFromSchedule until it is closed.
type fakeScheduleSubjects struct {
	mu       sync.Mutex
	proposal library.ScheduleProposal
	propErr  error
	res      library.ScheduleResult
	err      error
	undo     library.ScheduleUndo
	undoErr  error

	block   chan struct{}
	started chan struct{}

	creates    int
	gotToken   string
	gotChoices []schedulesubjects.Choice
	deadline   bool
	undos      []scheduleUndoCall
}

type scheduleUndoCall struct {
	token string
	res   library.ScheduleResult
}

func (f *fakeScheduleSubjects) ScheduleSubjects(context.Context) (library.ScheduleProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.proposal, f.propErr
}

func (f *fakeScheduleSubjects) CreateFromSchedule(ctx context.Context, token string, choices []schedulesubjects.Choice) (library.ScheduleResult, error) {
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	f.gotToken, f.gotChoices = token, choices
	_, f.deadline = ctx.Deadline()
	return f.res, f.err
}

func (f *fakeScheduleSubjects) UndoFromSchedule(_ context.Context, token string, res library.ScheduleResult) (library.ScheduleUndo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.undos = append(f.undos, scheduleUndoCall{token, res})
	return f.undo, f.undoErr
}

func (f *fakeScheduleSubjects) set(res library.ScheduleResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.res, f.err = res, err
}

// scheduleRecordLog keeps every record, so a test can look at the raw values (a text handler would escape a newline).
type scheduleRecordLog struct {
	mu      *sync.Mutex
	records *[]slog.Record
	attrs   []slog.Attr
}

func newScheduleRecordLog() (*slog.Logger, *scheduleRecordLog) {
	h := &scheduleRecordLog{mu: &sync.Mutex{}, records: &[]slog.Record{}}
	return slog.New(h), h
}

func (h *scheduleRecordLog) Enabled(context.Context, slog.Level) bool { return true }

func (h *scheduleRecordLog) Handle(_ context.Context, r slog.Record) error {
	r = r.Clone()
	r.AddAttrs(h.attrs...)
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}

func (h *scheduleRecordLog) WithAttrs(as []slog.Attr) slog.Handler {
	return &scheduleRecordLog{mu: h.mu, records: h.records, attrs: append(append([]slog.Attr{}, h.attrs...), as...)}
}

func (h *scheduleRecordLog) WithGroup(string) slog.Handler { return h }

// texts is every message and attribute value logged.
func (h *scheduleRecordLog) texts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range *h.records {
		out = append(out, r.Message)
		r.Attrs(func(a slog.Attr) bool {
			out = append(out, a.Key, a.Value.Resolve().String())
			return true
		})
	}
	return out
}

func newScheduleBinding(uc *fakeScheduleSubjects) *LibraryService {
	return NewLibraryService(&fakeLibrary{}, nil, nil, discard()).withSchedule(uc)
}

func TestScheduleSubjectsMapping(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: library.ScheduleProposal{Token: tokA, Omitted: 3, Groups: []schedulesubjects.Group{
		{
			Key: "матанализ", Name: "Матанализ", Aliases: []string{"Матан"}, Kinds: schedulesubjects.Kinds{Lecture: 2, Lab: 1},
			Lessons: 3, Recordings: 1, Doubtful: true, Members: []schedulesubjects.Member{
				{Key: "матанализ", Name: "Матанализ", Spellings: []string{"Матанализ (лек.)"}, Kinds: schedulesubjects.Kinds{Lecture: 2}, Lessons: 2},
				{
					Key: "матан", Name: "Матан", Spellings: []string{"Матан"}, Kinds: schedulesubjects.Kinds{Lab: 1}, Lessons: 1,
					Recordings: 1, Link: schedulesubjects.LinkAbbreviation, Doubtful: true,
				},
			},
		},
		{
			Key: "физика", Name: "Физика", Subject: "s-phys", Aliases: []string{"физика, сем."},
			Kinds: schedulesubjects.Kinds{Seminar: 4, Unknown: 1}, Lessons: 5, Members: []schedulesubjects.Member{
				{Key: "физика", Name: "физика", Spellings: []string{"физика, сем."}, Kinds: schedulesubjects.Kinds{Seminar: 4, Unknown: 1}, Lessons: 5},
			},
		},
	}}}

	got := newScheduleBinding(uc).ScheduleSubjects(context.Background())
	want := ScheduleSubjectsView{Token: tokA, Omitted: 3, Groups: []ScheduleGroup{
		{
			Key: "матанализ", Name: "Матанализ", Aliases: []string{"Матан"},
			Kinds:   []ScheduleKindCount{{"lecture", 2}, {"lab", 1}},
			Lessons: 3, Recordings: 1, Doubtful: true, Action: "create", Members: []ScheduleMember{
				{Key: "матанализ", Name: "Матанализ", Spellings: []string{"Матанализ (лек.)"}, Kinds: []ScheduleKindCount{{"lecture", 2}}, Lessons: 2, Link: "same"},
				{
					Key: "матан", Name: "Матан", Spellings: []string{"Матан"}, Kinds: []ScheduleKindCount{{"lab", 1}}, Lessons: 1,
					Recordings: 1, Doubtful: true, Link: "abbreviation",
				},
			},
		},
		{
			Key: "физика", Name: "Физика", Aliases: []string{"физика, сем."}, Kinds: []ScheduleKindCount{{"seminar", 4}, {"unknown", 1}},
			Lessons: 5, Action: "alias_of", SubjectID: "s-phys", SubjectName: "Физика", Members: []ScheduleMember{
				{Key: "физика", Name: "физика", Spellings: []string{"физика, сем."}, Kinds: []ScheduleKindCount{{"seminar", 4}, {"unknown", 1}}, Lessons: 5, Link: "same"},
			},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ScheduleSubjects =\n%+v\nwant\n%+v", got, want)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"token"`, `"groups"`, `"omitted":3`, `"problem":""`, `"key"`, `"aliases"`, `"kinds":[{"kind":"lecture","lessons":2}`,
		`"recordings"`, `"doubtful"`, `"members"`, `"spellings"`, `"link":"abbreviation"`, `"action":"alias_of"`,
		`"subjectId":"s-phys"`, `"subjectName":"Физика"`,
	} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("JSON has no %s: %s", field, raw)
		}
	}
}

// An empty proposal is a token and empty lists, never null and never a problem.
func TestScheduleSubjectsEmpty(t *testing.T) {
	got := newScheduleBinding(&fakeScheduleSubjects{proposal: library.ScheduleProposal{Token: tokA}}).ScheduleSubjects(context.Background())
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"token":"` + tokA + `","groups":[],"omitted":0,"problem":""}`; string(raw) != want {
		t.Fatalf("got %s, want %s", raw, want)
	}
}

func TestScheduleSubjectsUnavailable(t *testing.T) {
	got := newScheduleBinding(&fakeScheduleSubjects{propErr: library.ErrUnavailable}).ScheduleSubjects(context.Background())
	if got.Problem != "unavailable" || got.Groups == nil || got.Token != "" {
		t.Fatalf("ScheduleSubjects = %+v", got)
	}
}

// Without the use case every call is unavailable, never a panic.
func TestScheduleNotWired(t *testing.T) {
	s := NewLibraryService(&fakeLibrary{}, nil, nil, discard())
	ctx := context.Background()
	if got := s.ScheduleSubjects(ctx); got.Problem != "unavailable" {
		t.Errorf("ScheduleSubjects = %+v", got)
	}
	if got := s.CreateFromSchedule(ctx, tokA, nil); got.Problem != "unavailable" {
		t.Errorf("CreateFromSchedule = %+v", got)
	}
	if got := s.UndoFromSchedule(ctx, tokA); got.Problem != "unavailable" {
		t.Errorf("UndoFromSchedule = %+v", got)
	}
}

// Every action of the page reaches the use case as its schedulesubjects value, with the group, name and members.
func TestCreateFromSchedulePassesChoices(t *testing.T) {
	uc := &fakeScheduleSubjects{}
	got := newScheduleBinding(uc).CreateFromSchedule(context.Background(), tokA, []ScheduleChoice{
		{Group: "g1", Action: "create", Name: "Математический анализ", Members: []ScheduleMemberChoice{
			{Key: "m1", Action: "keep"}, {Key: "m2", Action: "separate", Name: "Матан"}, {Key: "m3", Action: "skip"},
		}},
		{Group: "g2", Action: "alias_of"},
		{Group: "g3", Action: "skip"},
	})
	if got.Problem != "" {
		t.Fatalf("CreateFromSchedule = %+v", got)
	}
	want := []schedulesubjects.Choice{
		{Group: "g1", Action: schedulesubjects.GroupCreate, Name: "Математический анализ", Members: []schedulesubjects.MemberChoice{
			{Member: "m1", Action: schedulesubjects.MemberMerge},
			{Member: "m2", Action: schedulesubjects.MemberSeparately, Name: "Матан"},
			{Member: "m3", Action: schedulesubjects.MemberSkip},
		}},
		{Group: "g2", Action: schedulesubjects.GroupAddToSubject},
		{Group: "g3", Action: schedulesubjects.GroupSkip},
	}
	if uc.gotToken != tokA || !reflect.DeepEqual(uc.gotChoices, want) {
		t.Fatalf("use case got %q %+v, want %+v", uc.gotToken, uc.gotChoices, want)
	}
	if !uc.deadline {
		t.Error("the call ctx has no deadline")
	}
}

// An unknown action or too many choices is refused here; the use case is not called.
func TestCreateFromScheduleRefusesBadChoices(t *testing.T) {
	many := make([]ScheduleChoice, maxScheduleChoices+1)
	for i := range many {
		many[i] = ScheduleChoice{Group: fmt.Sprint("g", i), Action: "skip"}
	}
	manyMembers := make([]ScheduleMemberChoice, maxScheduleChoices+1)
	for i := range manyMembers {
		manyMembers[i] = ScheduleMemberChoice{Key: fmt.Sprint("m", i), Action: "keep"}
	}
	tests := []struct {
		name    string
		choices []ScheduleChoice
	}{
		{"empty group action", []ScheduleChoice{{Group: "g1"}}},
		{"unknown group action", []ScheduleChoice{{Group: "g1", Action: "delete"}}},
		{"group action in another case", []ScheduleChoice{{Group: "g1", Action: "Create"}}},
		{"empty member action", []ScheduleChoice{{Group: "g1", Action: "create", Members: []ScheduleMemberChoice{{Key: "m1"}}}}},
		{"unknown member action", []ScheduleChoice{{Group: "g1", Action: "create", Members: []ScheduleMemberChoice{{Key: "m1", Action: "merge"}}}}},
		{"too many choices", many},
		{"too many members", []ScheduleChoice{{Group: "g1", Action: "create", Members: manyMembers}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeScheduleSubjects{}
			got := newScheduleBinding(uc).CreateFromSchedule(context.Background(), tokA, tt.choices)
			if got.Problem != "invalid" || uc.creates != 0 {
				t.Fatalf("got %+v, %d calls", got, uc.creates)
			}
		})
	}
}

func TestCreateFromScheduleProblems(t *testing.T) {
	cause := errors.New("disk")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"stale", library.ErrStale, "stale"},
		{"bad token", library.ErrInvalidRequest, "invalid"},
		{"unknown group", fmt.Errorf("%w: %w", library.ErrInvalidRequest, schedulesubjects.ErrUnknownGroup), "invalid"},
		{"unknown member", fmt.Errorf("%w: %w", library.ErrInvalidRequest, schedulesubjects.ErrUnknownMember), "invalid"},
		{"bad choice", fmt.Errorf("%w: %w", library.ErrInvalidRequest, schedulesubjects.ErrInvalidChoice), "invalid"},
		{"bare domain choice", schedulesubjects.ErrInvalidChoice, "invalid"},
		{"bad name", fmt.Errorf("%w: bad", library.ErrInvalidName), "invalid"},
		{"rename taken", fmt.Errorf("%w: %w", library.ErrNameTaken, schedulesubjects.ErrNameTaken), "name_taken"},
		{"busy", library.ErrBusy, "busy"},
		{"storage", fmt.Errorf("%w: %w", library.ErrUnavailable, cause), "unavailable"},
		{"timeout", fmt.Errorf("%w: %w", library.ErrUnavailable, context.DeadlineExceeded), "unavailable"},
		{"unknown", cause, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newScheduleBinding(&fakeScheduleSubjects{err: tt.err}).CreateFromSchedule(context.Background(), tokA, nil)
			if got.Problem != tt.want {
				t.Fatalf("problem = %q, want %q", got.Problem, tt.want)
			}
			if got.Created == nil || got.Aliased == nil || got.Skipped == nil {
				t.Fatalf("null lists: %+v", got)
			}
		})
	}
}

func partialResult() library.ScheduleResult {
	return library.ScheduleResult{
		Created: []library.ScheduleCreated{{ID: "sched-1", Key: "матанализ", Name: "Матанализ"}},
		Aliased: []library.ScheduleAliased{{Subject: "s-phys", Aliases: []string{"физика, сем."}}},
		Skipped: []library.ScheduleSkipped{
			{Key: "химия", Reason: library.ReasonNameTaken}, {Subject: "s-bio", Reason: library.ReasonTooManyAliases},
			{Subject: "s-gone", Reason: library.ReasonSubjectGone},
		},
	}
}

// A failure midway returns the done part with the problem; the undo then takes back exactly what the core stored.
func TestCreateFromScheduleDonePartThenUndo(t *testing.T) {
	uc := &fakeScheduleSubjects{
		res: partialResult(), err: fmt.Errorf("%w: disk", library.ErrUnavailable),
		undo: library.ScheduleUndo{
			Removed:     []shared.SubjectID{"sched-1"},
			Kept:        []library.ScheduleKept{{ID: "sched-2", Name: "Химия", Reason: library.ReasonHasNotes}},
			Unaliased:   []library.ScheduleAliased{{Subject: "s-phys", Aliases: []string{"физика, сем."}}},
			KeptAliases: []library.ScheduleKeptAliases{{Subject: "s-bio", Aliases: []string{"био"}, Reason: library.ReasonStale}},
		},
	}
	s := newScheduleBinding(uc)
	ctx := context.Background()

	got := s.CreateFromSchedule(ctx, tokA, nil)
	want := ScheduleCreateResult{
		Created: []ScheduleCreatedSubject{{ID: "sched-1", Key: "матанализ", Name: "Матанализ"}},
		Aliased: []ScheduleAliases{{SubjectID: "s-phys", Aliases: []string{"физика, сем."}}},
		Skipped: []ScheduleSkippedRow{
			{Key: "химия", Reason: "name_taken"}, {SubjectID: "s-bio", Reason: "too_many_aliases"},
			{SubjectID: "s-gone", Reason: "subject_gone"},
		},
		Problem: "unavailable",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateFromSchedule =\n%+v\nwant\n%+v", got, want)
	}

	undo := s.UndoFromSchedule(ctx, tokA)
	if len(uc.undos) != 1 || uc.undos[0].token != tokA || !reflect.DeepEqual(uc.undos[0].res, partialResult()) {
		t.Fatalf("use case undo got %+v", uc.undos)
	}
	wantUndo := ScheduleUndoResult{
		Removed:     []string{"sched-1"},
		Kept:        []ScheduleKeptSubject{{ID: "sched-2", Name: "Химия", Reason: "has_notes"}},
		Unaliased:   []ScheduleAliases{{SubjectID: "s-phys", Aliases: []string{"физика, сем."}}},
		KeptAliases: []ScheduleKeptAliases{{SubjectID: "s-bio", Aliases: []string{"био"}, Reason: "stale"}},
	}
	if !reflect.DeepEqual(undo, wantUndo) {
		t.Fatalf("UndoFromSchedule =\n%+v\nwant\n%+v", undo, wantUndo)
	}
	raw, err := json.Marshal(undo)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"removed"`, `"kept":[{"id"`, `"unaliased"`, `"keptAliases":[{"subjectId"`, `"reason":"stale"`, `"problem":""`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("JSON has no %s: %s", field, raw)
		}
	}

	// A repeated undo passes the same stored result again.
	s.UndoFromSchedule(ctx, tokA)
	if len(uc.undos) != 2 || !reflect.DeepEqual(uc.undos[1].res, partialResult()) {
		t.Fatalf("repeated undo got %+v", uc.undos)
	}
}

// A retry with the same token replaces the stored outcome by the newer one, unless the newer did nothing.
func TestCreateFromScheduleRetryReplaces(t *testing.T) {
	uc := &fakeScheduleSubjects{}
	s := newScheduleBinding(uc)
	ctx := context.Background()

	first := library.ScheduleResult{Created: []library.ScheduleCreated{{ID: "sched-1", Key: "a", Name: "А"}}}
	uc.set(first, library.ErrBusy)
	s.CreateFromSchedule(ctx, tokA, nil)

	second := library.ScheduleResult{Created: []library.ScheduleCreated{{ID: "sched-1", Key: "a", Name: "А"}, {ID: "sched-2", Key: "b", Name: "Б"}}}
	uc.set(second, nil)
	s.CreateFromSchedule(ctx, tokA, nil)

	uc.set(library.ScheduleResult{}, library.ErrStale)
	if got := s.CreateFromSchedule(ctx, tokA, nil); got.Problem != "stale" {
		t.Fatalf("third create = %+v", got)
	}

	s.UndoFromSchedule(ctx, tokA)
	if len(uc.undos) != 1 || !reflect.DeepEqual(uc.undos[0].res, second) {
		t.Fatalf("undo got %+v, want %+v", uc.undos, second)
	}
}

// An undo of a token this run did not create anything for is invalid, and the use case is not called.
func TestUndoFromScheduleUnknownToken(t *testing.T) {
	uc := &fakeScheduleSubjects{}
	s := newScheduleBinding(uc)
	ctx := context.Background()

	if got := s.UndoFromSchedule(ctx, tokA); got.Problem != "invalid" || got.Removed == nil || got.KeptAliases == nil {
		t.Fatalf("unknown token = %+v", got)
	}

	// A create that did nothing (stale, refused) records nothing: a page cannot fill the journal with made-up tokens.
	uc.set(library.ScheduleResult{}, library.ErrStale)
	s.CreateFromSchedule(ctx, tokB, nil)
	if got := s.UndoFromSchedule(ctx, tokB); got.Problem != "invalid" {
		t.Fatalf("token of a stale create = %+v", got)
	}
	if len(uc.undos) != 0 {
		t.Fatalf("use case called: %+v", uc.undos)
	}
}

// The page cannot hand a result to the undo: the token is its only argument.
func TestUndoFromScheduleTakesOnlyToken(t *testing.T) {
	m, ok := reflect.TypeFor[*LibraryService]().MethodByName("UndoFromSchedule")
	if !ok {
		t.Fatal("no UndoFromSchedule")
	}
	if m.Type.NumIn() != 3 || m.Type.In(2) != reflect.TypeFor[string]() {
		t.Fatalf("UndoFromSchedule is %v, want (ctx, token string)", m.Type)
	}
}

func TestUndoFromScheduleProblems(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"invalid", library.ErrInvalidRequest, "invalid"},
		{"busy", library.ErrBusy, "busy"},
		{"storage", fmt.Errorf("%w: disk", library.ErrUnavailable), "unavailable"},
		{"unknown", errors.New("boom"), "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeScheduleSubjects{res: partialResult(), undoErr: tt.err, undo: library.ScheduleUndo{Removed: []shared.SubjectID{"sched-1"}}}
			s := newScheduleBinding(uc)
			s.CreateFromSchedule(context.Background(), tokA, nil)
			got := s.UndoFromSchedule(context.Background(), tokA)
			if got.Problem != tt.want || !reflect.DeepEqual(got.Removed, []string{"sched-1"}) {
				t.Fatalf("got %+v, want problem %q with the done part", got, tt.want)
			}
		})
	}
}

// The journal keeps the last scheduleJournalSize tokens; recording a token again makes it the newest.
func TestScheduleJournalBound(t *testing.T) {
	uc := &fakeScheduleSubjects{res: partialResult()}
	s := newScheduleBinding(uc)
	ctx := context.Background()
	tok := func(i int) string { return fmt.Sprintf("%064d", i) }

	for i := range scheduleJournalSize {
		s.CreateFromSchedule(ctx, tok(i), nil)
	}
	s.CreateFromSchedule(ctx, tok(0), nil) // 0 is the newest now, 1 the oldest
	s.CreateFromSchedule(ctx, tok(scheduleJournalSize), nil)

	if got := s.UndoFromSchedule(ctx, tok(1)); got.Problem != "invalid" {
		t.Errorf("evicted token = %+v", got)
	}
	for _, i := range []int{0, 2, scheduleJournalSize} {
		if got := s.UndoFromSchedule(ctx, tok(i)); got.Problem != "" {
			t.Errorf("token %d = %+v", i, got)
		}
	}
	s.schedule.journal.mu.Lock()
	n := len(s.schedule.journal.byTok)
	s.schedule.journal.mu.Unlock()
	if n != scheduleJournalSize {
		t.Errorf("journal holds %d tokens", n)
	}
}

// While a create runs, a second create and an undo answer busy at once; the proposal and the other library calls stay
// callable. After it, the next create runs.
func TestScheduleOneCallAtATime(t *testing.T) {
	uc := &fakeScheduleSubjects{
		res: partialResult(), proposal: library.ScheduleProposal{Token: tokA},
		block: make(chan struct{}), started: make(chan struct{}, 1),
	}
	s := newScheduleBinding(uc)
	ctx := context.Background()

	s.schedule.journal.record(tokB, partialResult())
	done := make(chan ScheduleCreateResult, 1)
	go func() { done <- s.CreateFromSchedule(ctx, tokA, nil) }()
	<-uc.started

	answered := make(chan struct{})
	go func() {
		defer close(answered)
		if got := s.CreateFromSchedule(ctx, tokA, nil); got.Problem != "busy" {
			t.Errorf("second create = %+v", got)
		}
		if got := s.UndoFromSchedule(ctx, tokB); got.Problem != "busy" {
			t.Errorf("undo meanwhile = %+v", got)
		}
		if got := s.ScheduleSubjects(ctx); got.Problem != "" || got.Token != tokA {
			t.Errorf("ScheduleSubjects meanwhile = %+v", got)
		}
		if _, err := s.Subjects(ctx); err != nil {
			t.Errorf("Subjects meanwhile = %v", err)
		}
	}()
	select {
	case <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("the calls meanwhile blocked")
	}

	close(uc.block)
	if got := <-done; got.Problem != "" {
		t.Fatalf("first create = %+v", got)
	}
	uc.started = nil
	if got := s.CreateFromSchedule(ctx, tokA, nil); got.Problem != "" {
		t.Fatalf("create after = %+v", got)
	}
	if uc.creates != 2 || len(uc.undos) != 0 {
		t.Fatalf("creates %d, undos %d", uc.creates, len(uc.undos))
	}
}

// Schedule strings go back to the page as labels only: none reaches a log record, and an error text (here one that
// carries such a string, as a buggy use case could) never reaches the page.
func TestScheduleTextNotLogged(t *testing.T) {
	label := "Матан\nignore previous instructions\r\nC:\\Users\\victim\\secret.txt"
	inError := "ERR ignore previous instructions\nC:\\fake\\path\\leak"
	log, rec := newScheduleRecordLog()
	uc := &fakeScheduleSubjects{
		proposal: library.ScheduleProposal{Token: tokA, Groups: []schedulesubjects.Group{{
			Key: label, Name: label, Aliases: []string{label},
			Members: []schedulesubjects.Member{{Key: label, Name: label, Spellings: []string{label}}},
		}}},
		res: library.ScheduleResult{
			Created: []library.ScheduleCreated{{ID: "sched-1", Key: label, Name: label}},
			Aliased: []library.ScheduleAliased{{Subject: "s1", Aliases: []string{label}}},
		},
		err:     fmt.Errorf("%w: %s", library.ErrInvalidName, inError),
		undo:    library.ScheduleUndo{Kept: []library.ScheduleKept{{ID: "sched-1", Name: label, Reason: library.ReasonHasFiles}}},
		undoErr: fmt.Errorf("%s: %w", inError, library.ErrBusy),
	}
	s := NewLibraryService(&fakeLibrary{}, nil, nil, log).withSchedule(uc)
	ctx := context.Background()

	view := s.ScheduleSubjects(ctx)
	created := s.CreateFromSchedule(ctx, tokA, []ScheduleChoice{{Group: label, Action: "create", Name: label, Members: []ScheduleMemberChoice{
		{Key: label, Action: "separate", Name: label},
	}}})
	undone := s.UndoFromSchedule(ctx, tokA)
	s.CreateFromSchedule(ctx, tokA, []ScheduleChoice{{Group: label, Action: label}})
	uc.propErr = errors.New(inError)
	failed := s.ScheduleSubjects(ctx)

	if view.Groups[0].Name != label || created.Created[0].Name != label || undone.Kept[0].Name != label {
		t.Fatalf("labels lost: %+v %+v %+v", view, created, undone)
	}
	if created.Problem != "invalid" || undone.Problem != "busy" || failed.Problem != "unavailable" {
		t.Fatalf("problems %q %q %q", created.Problem, undone.Problem, failed.Problem)
	}

	texts := rec.texts()
	if len(texts) == 0 {
		t.Fatal("nothing logged")
	}
	for _, text := range texts {
		for _, bad := range []string{"ignore previous", "Матан", "victim", "fake", "ERR"} {
			if strings.Contains(text, bad) {
				t.Errorf("log holds %q: %q", bad, text)
			}
		}
	}
	for _, out := range []any{view, created, undone, failed} {
		raw, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "leak") || strings.Contains(string(raw), "ERR") {
			t.Errorf("an error text reached the page: %s", raw)
		}
	}
}
