package agent

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// secret is the student's text of the tests: it must never reach the planner, a card or the log.
const secret = "Секретная мысль про импульс"

func addText(target string) string {
	return `{"action":"add_text",` + target + `,"text":"` + secret + `\nвторая строка"}`
}

// noSecret fails when the student's text or title reached the planner, a card or the log.
func noSecret(t *testing.T, nw *notesWorld, r ran, words ...string) {
	t.Helper()
	for _, s := range []string{r.out.Result.ResultJSON, fmt.Sprint(r.cards), nw.log.String()} {
		for _, w := range append(words, secret, "вторая строка") {
			if strings.Contains(s, w) {
				t.Fatalf("%q reached the planner, a card or the log: %s", w, s)
			}
		}
	}
}

// Every action of note journals the compensation of ITS action (Complete checks kinds per tool only), and Undo puts
// back what was.
func TestNoteActionsAndUndo(t *testing.T) {
	n1 := mustRef(t, values.KindNote, "n1")
	tests := []struct {
		name   string
		st     func(t *testing.T) agentturn.Step
		args   string
		result string
		kind   action.CompensationKind
		check  func(t *testing.T, nw *notesWorld, e agentjournal.Entry) // after the action
		undone func(t *testing.T, nw *notesWorld)                       // after Undo
	}{
		{
			name: "add_text into the note", args: addText(`"note":"n1"`),
			result: `{"changed":true,"done":true,"id":"n1","parts":2}`, kind: action.CompRemoveNotePart,
			check: func(t *testing.T, nw *notesWorld, e agentjournal.Entry) {
				c := e.Compensation.(action.RemoveNotePart)
				if c.Note != n1 || !slices.Contains(nw.noteStore.state(t, "n1").Parts, shared.MaterialID(c.Part.ID())) {
					t.Fatalf("compensation %+v, parts %v", c, nw.noteStore.state(t, "n1").Parts)
				}
				m := nw.lib.materials[shared.MaterialID(c.Part.ID())]
				if m.Subject() != "s1" || m.Folder() != subject.SystemFolderID("s1", subject.SystemLectures) {
					t.Fatalf("text stored in %s/%s", m.Subject(), m.Folder())
				}
			},
			undone: func(t *testing.T, nw *notesWorld) {
				if got := nw.noteStore.state(t, "n1").Parts; !slices.Equal(got, []shared.MaterialID{"m1"}) {
					t.Fatalf("parts after undo = %v", got)
				}
			},
		},
		{
			name: "add_text to a day of the request's subject that has a note", st: func(t *testing.T) agentturn.Step { return inScope(t, "s1") },
			args: addText(`"date":"2026-10-15"`), result: `{"changed":true,"done":true,"id":"n1","parts":2}`, kind: action.CompRemoveNotePart,
		},
		{
			name: "add_text makes the note of a day", st: func(t *testing.T) agentturn.Step { return inScope(t, "s1") },
			args: addText(`"date":"2026-10-16","title":"Мысли"`), result: `{"changed":true,"created":true,"done":true,"id":"n2","parts":1}`,
			kind: action.CompRemoveNote,
			check: func(t *testing.T, nw *notesWorld, _ agentjournal.Entry) {
				st := nw.noteStore.state(t, "n2")
				if st.Subject != "s1" || st.Anchor.Kind() != note.AnchorDay || st.Anchor.Date().String() != "2026-10-16" || st.Title != "Мысли" {
					t.Fatalf("new note %+v", st)
				}
			},
			undone: func(t *testing.T, nw *notesWorld) {
				if nw.noteStore.state(t, "n2").Status != note.StatusRemoved {
					t.Fatal("the note the add made is still there")
				}
			},
		},
		{
			name: "add_text makes the note of a lesson, of the lesson's subject", args: addText(`"lesson":"l1"`),
			result: `{"changed":true,"created":true,"done":true,"id":"n2","parts":1}`, kind: action.CompRemoveNote,
			check: func(t *testing.T, nw *notesWorld, _ agentjournal.Entry) {
				if st := nw.noteStore.state(t, "n2"); st.Subject != "s1" || st.Anchor.Lesson() != "l1" {
					t.Fatalf("new note %+v", st)
				}
			},
		},
		{
			name: "add_upload into the note", args: `{"action":"add_upload","note":"n1","upload":"up1"}`,
			result: `{"changed":true,"done":true,"id":"n1","parts":2}`, kind: action.CompRemoveNotePart,
			check: func(t *testing.T, nw *notesWorld, e agentjournal.Entry) {
				if e.Compensation != (action.RemoveNotePart{Note: n1, Part: mustRef(t, values.KindMaterial, "up1")}) {
					t.Fatalf("compensation = %+v", e.Compensation)
				}
				if m := nw.lib.materials["up1"]; m.Subject() != "s1" || m.Folder() != subject.SystemFolderID("s1", subject.SystemLectures) {
					t.Fatalf("upload filed in %s/%s", m.Subject(), m.Folder())
				}
			},
		},
		{
			name: "add_upload makes the note of a day", st: func(t *testing.T) agentturn.Step { return inScope(t, "s1") },
			args: `{"action":"add_upload","date":"2026-10-16","upload":"up1"}`, result: `{"changed":true,"created":true,"done":true,"id":"n2","parts":1}`,
			kind: action.CompRemoveNote,
		},
		{
			name: "move to a lesson", args: `{"action":"move","note":"n1","lesson":"l1"}`,
			result: `{"changed":true,"done":true,"id":"n1"}`, kind: action.CompReanchorNote,
			check: func(t *testing.T, nw *notesWorld, e agentjournal.Entry) {
				want := action.ReanchorNote{Note: n1, Prior: action.NoteAnchor{Kind: action.AnchorDay, Date: mustDate(t, "2026-10-15")}}
				if e.Compensation != want || nw.noteStore.state(t, "n1").Anchor.Lesson() != "l1" {
					t.Fatalf("compensation %+v, anchor %+v", e.Compensation, nw.noteStore.state(t, "n1").Anchor)
				}
			},
			undone: func(t *testing.T, nw *notesWorld) {
				if a := nw.noteStore.state(t, "n1").Anchor; a.Kind() != note.AnchorDay || a.Date().String() != "2026-10-15" {
					t.Fatalf("anchor after undo = %+v", a)
				}
			},
		},
		{
			name: "move to a day of its own subject, outside any scope", args: `{"action":"move","note":"n1","date":"2026-10-17"}`,
			result: `{"changed":true,"done":true,"id":"n1"}`, kind: action.CompReanchorNote,
			check: func(t *testing.T, nw *notesWorld, _ agentjournal.Entry) {
				if st := nw.noteStore.state(t, "n1"); st.Anchor.Date().String() != "2026-10-17" || st.Subject != "s1" {
					t.Fatalf("moved %+v", st)
				}
			},
		},
		{
			name: "rename", args: `{"action":"rename","note":"n1","title":"Импульс, конспект"}`,
			result: `{"changed":true,"done":true,"id":"n1"}`, kind: action.CompRetitleNote,
			check: func(t *testing.T, nw *notesWorld, e agentjournal.Entry) {
				if e.Compensation != (action.RetitleNote{Note: n1, OldTitle: "Моя лекция"}) || nw.noteStore.state(t, "n1").Title != "Импульс, конспект" {
					t.Fatalf("compensation %+v, title %q", e.Compensation, nw.noteStore.state(t, "n1").Title)
				}
			},
			undone: func(t *testing.T, nw *notesWorld) {
				if got := nw.noteStore.state(t, "n1").Title; got != "Моя лекция" {
					t.Fatalf("title after undo = %q", got)
				}
			},
		},
		{
			name: "remove", args: `{"action":"remove","note":"n1"}`, result: `{"changed":true,"done":true,"id":"n1"}`, kind: action.CompRestoreNote,
			check: func(t *testing.T, nw *notesWorld, _ agentjournal.Entry) {
				if nw.noteStore.state(t, "n1").Status != note.StatusRemoved {
					t.Fatal("not removed")
				}
			},
			undone: func(t *testing.T, nw *notesWorld) {
				if nw.noteStore.state(t, "n1").Status != note.StatusActive {
					t.Fatal("not restored")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nw := newNotesWorld(t)
			st := req
			if tt.st != nil {
				st = tt.st(t)
			}
			r, e := nw.run(t, st, "c1", tt.args)
			if r.out.Result.IsError || r.out.Result.ResultJSON != tt.result {
				t.Fatalf("result = %+v, want %s", r.out.Result, tt.result)
			}
			if e.Status != action.StatusDone || e.Compensation == nil || e.Compensation.Kind() != tt.kind || !e.CanUndo {
				t.Fatalf("entry = %+v, want DONE with %s", e, tt.kind)
			}
			if len(r.cards) != 1 || r.cards[0] != (values.ActionLine{Action: e.ID}) {
				t.Fatalf("cards = %v, want the action line with «Отменить»", r.cards)
			}
			noSecret(t, nw, r, "Мысли", "Импульс, конспект", "Моя лекция")
			if tt.check != nil {
				tt.check(t, nw, e)
			}
			if u := undo(t, nw.world, e); u.Status != action.StatusUndone {
				t.Fatalf("undo = %+v", u)
			}
			if tt.undone != nil {
				tt.undone(t, nw)
			}
		})
	}
}

func mustDate(t *testing.T, s string) values.Date {
	t.Helper()
	d, err := values.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The text is a reply_text: 4000 characters (Cyrillic counts in characters, not bytes) are saved, 4001 are refused by
// the parser before anything runs.
func TestNoteTextBoundary(t *testing.T) {
	nw := newNotesWorld(t)
	longest := strings.Repeat("ж", values.MaxReplyTextRunes)
	r, e := nw.run(t, req, "c1", `{"action":"add_text","note":"n1","text":"`+longest+`"}`)
	if r.out.Result.IsError || e.Status != action.StatusDone {
		t.Fatalf("4000 characters: %+v, %+v", r.out.Result, e)
	}
	part := e.Compensation.(action.RemoveNotePart).Part
	if m := nw.lib.materials[shared.MaterialID(part.ID())]; m.Size() != int64(len(longest)) {
		t.Fatalf("stored %d bytes, want %d", m.Size(), len(longest))
	}
	_, err := values.ParseCallAt("c2", string(values.ToolNote), `{"action":"add_text","note":"n1","text":"`+longest+`ж"}`, values.CatalogWave10)
	if !errors.Is(err, values.ErrInvalidArguments) {
		t.Fatalf("4001 characters = %v", err)
	}
}

// What changes nothing answers changed:false without a journal entry and without «Отменить».
func TestNoteNoops(t *testing.T) {
	tests := []struct{ name, prepare, args string }{
		{"the same upload again", `{"action":"add_upload","note":"n1","upload":"up1"}`, `{"action":"add_upload","note":"n1","upload":"up1"}`},
		{"the anchor it has", "", `{"action":"move","note":"n1","date":"2026-10-15"}`},
		{"the title it has", "", `{"action":"rename","note":"n1","title":"  Моя лекция "}`},
		{"a removed note removed", `{"action":"remove","note":"n1"}`, `{"action":"remove","note":"n1"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nw := newNotesWorld(t)
			if tt.prepare != "" {
				nw.run(t, req, "c0", tt.prepare)
			}
			version := nw.noteStore.state(t, "n1").Version
			r, e := nw.run(t, req, "c1", tt.args)
			if r.out.Result.ResultJSON != `{"changed":false,"done":true,"id":"n1"}` || e.ID != "" || len(r.cards) != 0 {
				t.Fatalf("result = %+v, entry %+v, cards %v", r.out.Result, e, r.cards)
			}
			if nw.noteStore.state(t, "n1").Version != version {
				t.Fatal("a no-op saved the note")
			}
		})
	}

	t.Run("the same text again: «уже добавлено», no «Отменить»", func(t *testing.T) {
		nw := newNotesWorld(t)
		nw.run(t, req, "c0", addText(`"note":"n1"`))
		r, e := nw.run(t, req, "c1", addText(`"note":"n1"`))
		if r.out.Result.ResultJSON != `{"changed":false,"done":true,"id":"n1","parts":2}` || e.Status != action.StatusFailed || len(r.cards) != 0 {
			t.Fatalf("result = %+v, entry %+v, cards %v", r.out.Result, e, r.cards)
		}
	})
}

// IDs the use case does not know, a subject the request lacks and a removed subject are refused per call; nothing
// changes and nothing is journaled.
func TestNoteRefusals(t *testing.T) {
	removeS1 := func(_ *testing.T, nw *notesWorld) { nw.removeSubject("s1") }
	tests := []struct {
		name    string
		scope   string
		prepare func(t *testing.T, nw *notesWorld)
		args    string
		want    values.Refusal
	}{
		{"unknown note", "", nil, addText(`"note":"n9"`), values.RefuseUnknownID},
		{"unknown note to rename", "", nil, `{"action":"rename","note":"n9","title":"x"}`, values.RefuseUnknownID},
		{"unknown upload", "", nil, `{"action":"add_upload","note":"n1","upload":"up9"}`, values.RefuseUnknownID},
		{"a removed upload", "", func(_ *testing.T, nw *notesWorld) { _, _ = nw.lib.materials["up2"].Remove(now) },
			`{"action":"add_upload","note":"n1","upload":"up2"}`, values.RefuseNotFound},
		{"an upload filed under another subject", "", func(t *testing.T, nw *notesWorld) {
			nw.lib.subjects["s2"] = newSubject(t, "s2", "Химия")
			if _, err := nw.lib.Place(t.Context(), "up2", "s2", ""); err != nil {
				t.Fatal(err)
			}
		}, `{"action":"add_upload","note":"n1","upload":"up2"}`, values.RefuseInvalidArgs},
		{"unknown lesson", "", nil, addText(`"lesson":"l9"`), values.RefuseNotFound},
		{"move to an unknown lesson", "", nil, `{"action":"move","note":"n1","lesson":"l9"}`, values.RefuseNotFound},
		{"a day without the request's subject", "", nil, addText(`"date":"2026-10-16"`), values.RefuseInvalidArgs},
		{"a lesson of no library subject without the request's subject", "", nil, addText(`"lesson":"l2"`), values.RefuseInvalidArgs},
		{"a removed note", "", func(t *testing.T, nw *notesWorld) {
			if _, err := nw.notes.Remove(t.Context(), "n1"); err != nil {
				t.Fatal(err)
			}
		}, addText(`"note":"n1"`), values.RefuseNotFound},
		{"a note of a removed subject", "", removeS1, addText(`"note":"n1"`), values.RefuseUnknownID},
		{"rename a note of a removed subject", "", removeS1, `{"action":"rename","note":"n1","title":"x"}`, values.RefuseUnknownID},
		{"a day of a removed subject", "s1", removeS1, addText(`"date":"2026-10-16"`), values.RefuseUnknownID},
		{"a lesson whose subject is removed", "", removeS1, addText(`"lesson":"l1"`), values.RefuseInvalidArgs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nw := newNotesWorld(t)
			if tt.prepare != nil {
				tt.prepare(t, nw)
			}
			st := req
			if tt.scope != "" {
				st = inScope(t, tt.scope)
			}
			before := len(nw.noteStore.rows)
			version := nw.noteStore.rows["n1"].Version
			r, e := nw.run(t, st, "c1", tt.args)
			if !r.out.Result.IsError || !strings.Contains(r.out.Result.ResultJSON, `"`+string(tt.want)+`"`) || e.ID != "" {
				t.Fatalf("result = %+v, entry %+v; want %s, no entry", r.out.Result, e, tt.want)
			}
			if len(nw.noteStore.rows) != before || nw.noteStore.rows["n1"].Version != version {
				t.Fatal("a refused call changed the notes")
			}
			noSecret(t, nw, r)
		})
	}
}

// A lesson that names no subject of the library takes the subject of the request.
func TestNoteLessonFallsBackToTheRequestSubject(t *testing.T) {
	nw := newNotesWorld(t)
	r, e := nw.run(t, inScope(t, "s1"), "c1", addText(`"lesson":"l2"`))
	if r.out.Result.IsError || e.Compensation.Kind() != action.CompRemoveNote {
		t.Fatalf("result %+v, entry %+v", r.out.Result, e)
	}
	if st := nw.noteStore.state(t, "n2"); st.Subject != "s1" || st.Anchor.Lesson() != "l2" {
		t.Fatalf("note %+v", st)
	}
}

// The text goes in no log, also when the use case fails; the planner gets an enum.
func TestNoteTextIsNeverLogged(t *testing.T) {
	nw := newNotesWorld(t)
	nw.noteStore.saveErr = errors.New("disk I/O error")
	r, e := nw.run(t, req, "c1", addText(`"note":"n1"`))
	if r.out.Result.ResultJSON != `{"error":"failed"}` || e.Status != action.StatusFailed {
		t.Fatalf("result %+v, entry %+v", r.out.Result, e)
	}
	if nw.log.Len() == 0 {
		t.Fatal("the failure was not logged at all")
	}
	noSecret(t, nw, r)
}

// Undo of each action whose note changed since is «Изменено после»; nothing is written back.
func TestNoteUndoOfAChangedNote(t *testing.T) {
	retitle := func(t *testing.T, nw *notesWorld, id shared.NoteID) {
		if _, err := nw.notes.Retitle(t.Context(), id, "Своё название"); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name   string
		args   string
		scope  string
		kind   action.CompensationKind
		change func(t *testing.T, nw *notesWorld)
	}{
		{"add into the note, then the student adds a file", addText(`"note":"n1"`), "", action.CompRemoveNotePart, func(t *testing.T, nw *notesWorld) {
			if _, err := nw.notes.Paste(t.Context(), notes.PasteRequest{Note: "n1", Text: "своё"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"add made the note, then the student renames it", addText(`"date":"2026-10-16"`), "s1", action.CompRemoveNote, func(t *testing.T, nw *notesWorld) {
			retitle(t, nw, "n2")
		}},
		{"move, then the student renames", `{"action":"move","note":"n1","lesson":"l1"}`, "", action.CompReanchorNote, func(t *testing.T, nw *notesWorld) {
			retitle(t, nw, "n1")
		}},
		{"rename, then the student moves", `{"action":"rename","note":"n1","title":"Новое"}`, "", action.CompRetitleNote, func(t *testing.T, nw *notesWorld) {
			if _, err := nw.notes.Reanchor(t.Context(), "n1", notes.AnchorRef{Kind: note.AnchorUndated}); err != nil {
				t.Fatal(err)
			}
		}},
		{"remove, then the student restores and removes it", `{"action":"remove","note":"n1"}`, "", action.CompRestoreNote, func(t *testing.T, nw *notesWorld) {
			if _, err := nw.notes.Restore(t.Context(), "n1"); err != nil {
				t.Fatal(err)
			}
			if _, err := nw.notes.Remove(t.Context(), "n1"); err != nil {
				t.Fatal(err)
			}
		}},
		{"rename, then the subject is removed", `{"action":"rename","note":"n1","title":"Новое"}`, "", action.CompRetitleNote, func(_ *testing.T, nw *notesWorld) {
			nw.removeSubject("s1")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nw := newNotesWorld(t)
			st := req
			if tt.scope != "" {
				st = inScope(t, tt.scope)
			}
			_, e := nw.run(t, st, "c1", tt.args)
			if e.Status != action.StatusDone || e.Compensation.Kind() != tt.kind {
				t.Fatalf("entry = %+v, want DONE with %s", e, tt.kind)
			}
			tt.change(t, nw)
			snapshot := maps(nw)
			u := undo(t, nw.world, e)
			if u.Status != action.StatusUndoRefused || u.NoUndo != action.NoUndoChanged {
				t.Fatalf("undo = %+v, want «Изменено после»", u)
			}
			if fmt.Sprint(maps(nw)) != fmt.Sprint(snapshot) {
				t.Fatal("a refused undo changed the notes")
			}
		})
	}
}

// maps is the stored notes, for comparing before and after.
func maps(nw *notesWorld) map[shared.NoteID]note.State {
	nw.noteStore.mu.Lock()
	defer nw.noteStore.mu.Unlock()
	out := make(map[shared.NoteID]note.State, len(nw.noteStore.rows))
	for k, v := range nw.noteStore.rows {
		out[k] = v
	}
	return out
}

// A replayed call answers from its entry: nothing runs twice.
func TestNoteReplay(t *testing.T) {
	for _, args := range []string{addText(`"note":"n1"`), `{"action":"add_upload","note":"n1","upload":"up1"}`,
		`{"action":"move","note":"n1","lesson":"l1"}`, `{"action":"rename","note":"n1","title":"Новое"}`, `{"action":"remove","note":"n1"}`} {
		nw := newNotesWorld(t)
		first, _ := nw.run(t, req, "c1", args)
		version := nw.noteStore.state(t, "n1").Version
		second, _ := nw.run(t, req, "c1", args)
		if second.out.Result != first.out.Result || nw.noteStore.state(t, "n1").Version != version || len(nw.store.order) != 1 {
			t.Fatalf("%s: replay = %+v, first %+v", args, second.out.Result, first.out.Result)
		}
	}
}

// Recover settles a PENDING entry of each action by its intent after a crash between the use case and DONE, with the
// compensation of its action; the replay then answers from the entry and its Undo works.
func TestNoteRecover(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		args  string
		kind  action.CompensationKind
	}{
		{"add_upload into the note", "", `{"action":"add_upload","note":"n1","upload":"up1"}`, action.CompRemoveNotePart},
		{"add_text makes a note", "s1", addText(`"date":"2026-10-16"`), action.CompRemoveNote},
		{"add_upload makes a note of a lesson", "", `{"action":"add_upload","lesson":"l1","upload":"up1"}`, action.CompRemoveNote},
		{"move", "", `{"action":"move","note":"n1","lesson":"l1"}`, action.CompReanchorNote},
		{"rename", "", `{"action":"rename","note":"n1","title":"Новое"}`, action.CompRetitleNote},
		{"remove", "", `{"action":"remove","note":"n1"}`, action.CompRestoreNote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nw := newNotesWorld(t)
			st := req
			if tt.scope != "" {
				st = inScope(t, tt.scope)
			}
			c := call10(t, "c1", values.ToolNote, tt.args, actionpolicy.ClassU)
			crashAt(t, nw.world, st, c)
			e := recovered(t, nw.world, "c1")
			if e.Status != action.StatusDone || e.Compensation.Kind() != tt.kind {
				t.Fatalf("recovered = %+v, want DONE with %s", e, tt.kind)
			}
			r := nw.tools(t).run(t.Context(), st, c)
			if r.out.Result.IsError || !strings.Contains(r.out.Result.ResultJSON, `"changed":true`) {
				t.Fatalf("replay after Recover = %+v", r.out.Result)
			}
			if u := undo(t, nw.world, e); u.Status != action.StatusUndone {
				t.Fatalf("undo after Recover = %+v", u)
			}
		})
	}

	t.Run("an added text cannot be told from the student's change: UNKNOWN, no Undo", func(t *testing.T) {
		nw := newNotesWorld(t)
		crashAt(t, nw.world, req, call10(t, "c1", values.ToolNote, addText(`"note":"n1"`), actionpolicy.ClassU))
		if e := recovered(t, nw.world, "c1"); e.Status != action.StatusUnknown || e.CanUndo {
			t.Fatalf("recovered = %+v", e)
		}
	})

	t.Run("the use case did not run: FAILED", func(t *testing.T) {
		for _, args := range []string{addText(`"note":"n1"`), `{"action":"rename","note":"n1","title":"Новое"}`} {
			nw := newNotesWorld(t)
			p, ok, err := nw.actions.prepare(t.Context(), originOf(req), call10(t, "c1", values.ToolNote, args, actionpolicy.ClassU).Call)
			if !ok || err != nil {
				t.Fatal(ok, err)
			}
			if _, err := nw.journal.Begin(t.Context(), agentjournal.BeginInput{Thread: req.Thread, Turn: req.Turn, CallID: "c1",
				Tool: values.ToolNote, Intent: p.intent}); err != nil {
				t.Fatal(err)
			}
			if e := recovered(t, nw.world, "c1"); e.Status != action.StatusFailed {
				t.Fatalf("%s: recovered = %+v", args, e)
			}
		}
	})
}

func activityRecording(id shared.RecordingID, l shared.LessonID) activity.Recording {
	return activity.Recording{ID: id, LessonID: l}
}

// crashAt is crash for a request st (its scope gives the subject of a day).
func crashAt(t *testing.T, w *world, st agentturn.Step, c agentturn.Call) {
	t.Helper()
	p, ok, err := w.actions.prepare(t.Context(), originOf(st), c.Call)
	if !ok || err != nil {
		t.Fatalf("prepare: %v, %v", ok, err)
	}
	if _, err := w.journal.Begin(t.Context(), agentjournal.BeginInput{Thread: st.Thread, Turn: st.Turn, CallID: c.Call.ID(),
		Tool: c.Call.Tool(), Intent: p.intent}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.run(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// list_notes shows IDs, anchors, dates and counters of the active notes of a subject or a lesson, never a title, a
// text or a file name.
func TestListNotes(t *testing.T) {
	nw := newNotesWorld(t)
	nw.run(t, req, "c1", addText(`"lesson":"l1"`)) // n2 on the lesson l1
	if _, err := nw.notes.Remove(t.Context(), "n2"); err != nil {
		t.Fatal(err)
	}
	nw.run(t, req, "c2", `{"action":"add_upload","lesson":"l1","upload":"up1"}`) // n3 on l1
	// n4 on the recording r9 of the lesson l1, which is gone: it reads as its day.
	rec, err := note.NewAnchor(note.AnchorSpec{Kind: note.AnchorRecording, Recording: "r9", Date: mustNoteDate(t, "2026-10-20")})
	if err != nil {
		t.Fatal(err)
	}
	n4, err := note.New(note.Spec{ID: "n4", Subject: "s1", Anchor: rec, Title: "Запись", Parts: []shared.MaterialID{"m2"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := nw.noteStore.Save(t.Context(), n4); err != nil {
		t.Fatal(err)
	}
	nw.activity.recordings = append(nw.activity.recordings, activityRecording("r9", "l1"))

	list := func(args string) agentturn.Outcome {
		t.Helper()
		c := call10(t, "c9", values.ToolListNotes, args, actionpolicy.ClassR)
		return nw.tools(t).read(t.Context(), values.Scope{}, c)
	}
	bySubject := list(`{"subject":"s1"}`)
	want := `{"count":3,"notes":[` +
		`{"id":"n1","subject":"s1","anchor":"day","date":"2026-10-15","parts":1,"parts_without_text":0},` +
		`{"id":"n3","subject":"s1","anchor":"lesson","date":"2026-10-20","parts":1,"parts_without_text":0},` +
		`{"id":"n4","subject":"s1","anchor":"day","date":"2026-10-20","parts":1,"parts_without_text":0}],"truncated":false}`
	if bySubject.Result.IsError || bySubject.Result.ResultJSON != want {
		t.Fatalf("by subject = %s\nwant %s", bySubject.Result.ResultJSON, want)
	}
	byLesson := list(`{"lesson":"l1"}`)
	if !strings.Contains(byLesson.Result.ResultJSON, `"count":2`) || !strings.Contains(byLesson.Result.ResultJSON, `"id":"n3"`) ||
		!strings.Contains(byLesson.Result.ResultJSON, `"id":"n4"`) {
		t.Fatalf("by lesson = %s", byLesson.Result.ResultJSON)
	}
	for _, s := range []string{bySubject.Result.ResultJSON, byLesson.Result.ResultJSON} {
		for _, w := range []string{"Моя лекция", "Запись", "скан", secret, ".txt"} {
			if strings.Contains(s, w) {
				t.Fatalf("%q reached the planner: %s", w, s)
			}
		}
	}

	nw.removeSubject("s1")
	if gone := list(`{"subject":"s1"}`); gone.Result.ResultJSON != `{"count":0,"notes":[],"truncated":false}` {
		t.Fatalf("a removed subject = %s", gone.Result.ResultJSON)
	}
}

// Catalog v9 is what the Hub serves until TASK-326: the note tools are not in it.
func TestNoteToolsAreNotServedAtCatalog9(t *testing.T) {
	if values.CatalogVersion != 9 {
		t.Fatalf("CatalogVersion = %d", values.CatalogVersion)
	}
	for _, tool := range []values.Tool{values.ToolNote, values.ToolListNotes} {
		if _, err := values.ParseCall("c1", string(tool), `{"subject":"s1"}`); err == nil {
			t.Fatalf("%s parses at catalog 9", tool)
		}
		if slices.Contains(values.Tools(), tool) {
			t.Fatalf("%s is in catalog 9", tool)
		}
	}
}
