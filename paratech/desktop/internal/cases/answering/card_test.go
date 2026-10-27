package answering

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

// fakeNotes answers NoteActive from active; a note in fail cannot be checked.
type fakeNotes struct {
	active map[shared.NoteID]bool
	fail   map[shared.NoteID]bool
	checks []shared.NoteID
}

func (f *fakeNotes) NoteActive(_ context.Context, id shared.NoteID) (bool, error) {
	f.checks = append(f.checks, id)
	if f.fail[id] {
		return false, errors.New("disk")
	}
	return f.active[id], nil
}

// cardAnswer is a stored answer citing a recording and the notes n1 (twice), n2 and n3, in that order of sources.
func cardAnswer(t *testing.T) *answer.Answer {
	t.Helper()
	note := func(n shared.NoteID, page int) answer.Source {
		return answer.Source{Section: answer.SectionOwnNote, Note: n, Material: "m1", Locator: answer.Locator{Kind: answer.LocatorPage, N: page}}
	}
	req, err := answer.NewRequest("Что такое ряд?", []answer.Excerpt{
		{Text: "Ряд сходится абсолютно.", Source: answer.Source{Recording: "rec1", Section: answer.SectionSummary}},
		{Text: "Ряд Тейлора.", Source: note("n1", 1)},
		{Text: "Ряд Маклорена.", Source: note("n1", 2)},
		{Text: "Ряд Фурье.", Source: note("n2", 1)},
		{Text: "Ряд Лорана.", Source: note("n3", 1)},
	}, scope)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := req.Parse(`{"claims":[{"text":"Ряды бывают разные.","cites":[` +
		`{"chunk_id":"f1","quote":"сходится абсолютно"},{"chunk_id":"f2","quote":"Ряд Тейлора"},{"chunk_id":"f3","quote":"Ряд Маклорена"}]},` +
		`{"text":"И ещё ряды.","cites":[{"chunk_id":"f4","quote":"Ряд Фурье"},{"chunk_id":"f5","quote":"Ряд Лорана"}]}],"missing":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := answer.New("an1", "s1", req, rep, t0)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A citation of a note removed since shows as removed; one that cannot be checked is logged and shown as there; each
// note is checked once. Without the port nothing is removed; the stored answer is never changed.
func TestCard(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		notes  *fakeNotes
		want   []bool
		checks []shared.NoteID
		logged bool
	}{
		"removed and failing": {
			notes:  &fakeNotes{active: map[shared.NoteID]bool{"n1": true}, fail: map[shared.NoteID]bool{"n3": true}},
			want:   []bool{false, false, false, true, false},
			checks: []shared.NoteID{"n1", "n2", "n3"},
			logged: true,
		},
		"all there": {
			notes:  &fakeNotes{active: map[shared.NoteID]bool{"n1": true, "n2": true, "n3": true}},
			want:   []bool{false, false, false, false, false},
			checks: []shared.NoteID{"n1", "n2", "n3"},
		},
		"no notes port": {want: []bool{false, false, false, false, false}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			var logs strings.Builder
			e.svc.log = slog.New(slog.NewTextHandler(&logs, nil))
			if tc.notes != nil {
				e.svc.notes = tc.notes
			}
			a := cardAnswer(t)
			e.answers.byID[a.ID()] = a

			got, err := e.svc.Card(ctx, a.ID())
			if err != nil {
				t.Fatal(err)
			}
			if got.Answer != a || !reflect.DeepEqual(got.Removed, tc.want) || len(a.Sources()) != len(tc.want) {
				t.Fatalf("Card = %+v, want removed %v", got, tc.want)
			}
			if tc.notes != nil && !reflect.DeepEqual(tc.notes.checks, tc.checks) {
				t.Fatalf("checks = %v, want %v", tc.notes.checks, tc.checks)
			}
			if strings.Contains(logs.String(), opNoteCheck) != tc.logged || strings.Contains(logs.String(), "Ряд") {
				t.Fatalf("logs = %s", logs.String())
			}
		})
	}

	e := newEnv(t)
	e.svc.notes = &fakeNotes{}
	if _, err := e.svc.Card(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}
