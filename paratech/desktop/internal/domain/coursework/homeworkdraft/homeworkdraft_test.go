package homeworkdraft

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

func gen(base int, job shared.JobID) Generation {
	return Generation{Base: base, Job: job, Title: "Rivers", Body: "Rivers flow to the sea.", Instruction: "shorter", PromptVersion: 1}
}

// draftWith returns a draft of a1 with n alternating generated and edited versions.
func draftWith(t *testing.T, n int) *Draft {
	t.Helper()
	d, err := New("a1")
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if i%2 == 0 {
			if _, err := d.AddGenerated(gen(i, shared.JobID(fmt.Sprintf("j%d", i))), t0.Add(time.Duration(i)*time.Minute)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if _, err := d.Edit(i, "Rivers", fmt.Sprintf("edit %d", i), t0.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestNew(t *testing.T) {
	d, err := New("a1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Assignment() != "a1" || d.LatestNumber() != 0 || d.Version() != 0 || len(d.Revisions()) != 0 {
		t.Fatalf("new draft = %+v", d.State())
	}
	if _, ok := d.Latest(); ok {
		t.Fatal("an empty draft has a latest version")
	}
	for _, bad := range []shared.AssignmentID{"", "a/b", "CON"} {
		if _, err := New(bad); !errors.Is(err, ErrInvalidDraft) {
			t.Errorf("New(%q) = %v, want ErrInvalidDraft", bad, err)
		}
	}
}

// PRD section 10: generate, edit, regenerate with instructions; every step is a new numbered version.
func TestGenerateEditRegenerate(t *testing.T) {
	d, _ := New("a1")
	if added, err := d.AddGenerated(Generation{Base: 0, Job: "j1", Title: " Rivers ", Body: "Draft one.\n", PromptVersion: 1}, t0); err != nil || !added {
		t.Fatalf("first generation: %v, %v", added, err)
	}
	if changed, err := d.Edit(1, "Rivers", "Draft one, with my notes.", t0.Add(time.Minute)); err != nil || !changed {
		t.Fatalf("edit: %v, %v", changed, err)
	}
	g := gen(2, "j2")
	g.Instruction = "  make it shorter  "
	if added, err := d.AddGenerated(g, t0.Add(2*time.Minute)); err != nil || !added {
		t.Fatalf("regeneration: %v, %v", added, err)
	}

	revs := d.Revisions()
	if len(revs) != 3 || d.LatestNumber() != 3 {
		t.Fatalf("versions = %d", len(revs))
	}
	want := []struct {
		origin      Origin
		body        string
		instruction string
		job         shared.JobID
		prompt      int
	}{
		{OriginGenerated, "Draft one.", "", "j1", 1},
		{OriginEdited, "Draft one, with my notes.", "", "", 0},
		{OriginGenerated, "Rivers flow to the sea.", "make it shorter", "j2", 1},
	}
	for i, w := range want {
		r := revs[i]
		if r.Number() != i+1 || r.Origin() != w.origin || r.Body() != w.body || r.Title() != "Rivers" ||
			r.Instruction() != w.instruction || r.Job() != w.job || r.PromptVersion() != w.prompt ||
			!r.CreatedAt().Equal(t0.Add(time.Duration(i)*time.Minute)) {
			t.Fatalf("version %d = %+v", i+1, r)
		}
	}
	if r, ok := d.Revision(2); !ok || r.Origin() != OriginEdited {
		t.Fatalf("Revision(2) = %+v, %v", r, ok)
	}
	for _, n := range []int{0, 4, -1} {
		if _, ok := d.Revision(n); ok {
			t.Errorf("Revision(%d) found", n)
		}
	}
}

// Every change must start from the latest version.
func TestStaleBaseIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		apply  func(d *Draft) error
		before int
	}{
		{"generation from an older version", func(d *Draft) error { _, err := d.AddGenerated(gen(1, "j9"), t0); return err }, 2},
		{"generation from a future version", func(d *Draft) error { _, err := d.AddGenerated(gen(3, "j9"), t0); return err }, 2},
		{"generation on a draft that got its first version", func(d *Draft) error { _, err := d.AddGenerated(gen(0, "j9"), t0); return err }, 1},
		{"edit of an older version", func(d *Draft) error { _, err := d.Edit(1, "T", "mine", t0); return err }, 2},
		{"edit as if there were no draft", func(d *Draft) error { _, err := d.Edit(0, "T", "mine", t0); return err }, 1},
		{"unchanged edit of an older version", func(d *Draft) error { _, err := d.Edit(1, "Rivers", "Rivers flow to the sea.", t0); return err }, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := draftWith(t, tc.before)
			if err := tc.apply(d); !errors.Is(err, ErrStaleBase) {
				t.Fatalf("err = %v, want ErrStaleBase", err)
			}
			if d.LatestNumber() != tc.before {
				t.Fatalf("versions = %d, want %d (nothing added)", d.LatestNumber(), tc.before)
			}
		})
	}
}

// A job that runs again after it already added its answer adds nothing, even though its base is no longer the latest.
func TestGenerationIsAppliedOncePerJob(t *testing.T) {
	d, _ := New("a1")
	if added, err := d.AddGenerated(gen(0, "j1"), t0); err != nil || !added {
		t.Fatal(added, err)
	}
	if !d.ProducedBy("j1") || d.ProducedBy("j2") || d.ProducedBy("") {
		t.Fatal("ProducedBy is wrong")
	}
	added, err := d.AddGenerated(gen(0, "j1"), t0.Add(time.Minute))
	if err != nil || added || d.LatestNumber() != 1 {
		t.Fatalf("repeat = %v, %v; versions %d", added, err, d.LatestNumber())
	}
	// An edited version has no job, so an empty job id never counts as applied.
	if _, err := d.Edit(1, "T", "mine", t0); err != nil || d.ProducedBy("") {
		t.Fatalf("edit: %v; ProducedBy(\"\") = %v", err, d.ProducedBy(""))
	}
}

func TestUnchangedEditAddsNothing(t *testing.T) {
	d := draftWith(t, 1)
	changed, err := d.Edit(1, "  Rivers ", "Rivers flow to the sea.\n", t0.Add(time.Hour))
	if err != nil || changed || d.LatestNumber() != 1 {
		t.Fatalf("unchanged edit = %v, %v; versions %d", changed, err, d.LatestNumber())
	}
	if changed, err := d.Edit(1, "Rivers and seas", "Rivers flow to the sea.", t0.Add(time.Hour)); err != nil || !changed {
		t.Fatalf("title change = %v, %v", changed, err)
	}
}

func TestFirstVersionWrittenByHand(t *testing.T) {
	d, _ := New("a1")
	if changed, err := d.Edit(0, "My homework", "All by myself.", t0); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if r, _ := d.Latest(); r.Origin() != OriginEdited || r.Number() != 1 {
		t.Fatalf("latest = %+v", r)
	}
}

func TestInvalidContentIsRefused(t *testing.T) {
	long := strings.Repeat("я", MaxBodyRunes+1)
	cases := []struct {
		name        string
		title, body string
	}{
		{"empty title", " ", "body"},
		{"multi-line title", "a\nb", "body"},
		{"title with a line separator", "a b", "body"},
		{"long title", strings.Repeat("я", MaxTitleRunes+1), "body"},
		{"title not UTF-8", "\xff", "body"},
		{"empty body", "T", "\n\t "},
		{"long body", "T", long},
		{"body with a control character", "T", "a\x00b"},
		{"body not UTF-8", "T", "\xffbody"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := draftWith(t, 1)
			if _, err := d.Edit(1, tc.title, tc.body, t0); !errors.Is(err, ErrInvalidContent) {
				t.Errorf("Edit = %v, want ErrInvalidContent", err)
			}
			g := gen(1, "j9")
			g.Title, g.Body = tc.title, tc.body
			if _, err := d.AddGenerated(g, t0); !errors.Is(err, ErrInvalidContent) {
				t.Errorf("AddGenerated = %v, want ErrInvalidContent", err)
			}
			if d.LatestNumber() != 1 {
				t.Fatalf("versions = %d, want 1", d.LatestNumber())
			}
		})
	}

	// The limits themselves are accepted; a body may have line breaks and tabs.
	d := draftWith(t, 1)
	if _, err := d.Edit(1, strings.Repeat("я", MaxTitleRunes), strings.Repeat("я", MaxBodyRunes-3)+"\n\t.", t0); err != nil {
		t.Fatalf("edit at the limits: %v", err)
	}
}

func TestInvalidGenerationIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Generation)
	}{
		{"no job", func(g *Generation) { g.Job = "" }},
		{"bad job id", func(g *Generation) { g.Job = "a/b" }},
		{"no prompt version", func(g *Generation) { g.PromptVersion = 0 }},
		{"instruction with control characters", func(g *Generation) { g.Instruction = "a\x07b" }},
		{"instruction too large", func(g *Generation) { g.Instruction = strings.Repeat("x", values.MaxInstructionBytes+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := New("a1")
			g := gen(0, "j1")
			tc.change(&g)
			if _, err := d.AddGenerated(g, t0); !errors.Is(err, ErrInvalidContent) {
				t.Fatalf("err = %v, want ErrInvalidContent", err)
			}
			if d.LatestNumber() != 0 {
				t.Fatal("a version was added")
			}
		})
	}
	d, _ := New("a1")
	if _, err := d.AddGenerated(gen(0, "j1"), time.Time{}); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("zero time: %v", err)
	}
	if _, err := d.Edit(0, "T", "B", time.Time{}); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("edit at zero time: %v", err)
	}
}

func TestMaxRevisions(t *testing.T) {
	d := draftWith(t, MaxRevisions)
	if err := d.CanAppend(MaxRevisions); !errors.Is(err, ErrFull) {
		t.Fatalf("CanAppend on a full draft = %v, want ErrFull", err)
	}
	if _, err := d.AddGenerated(gen(MaxRevisions, "j-last"), t0); !errors.Is(err, ErrFull) {
		t.Fatalf("AddGenerated = %v, want ErrFull", err)
	}
	if _, err := d.Edit(MaxRevisions, "T", "something new", t0); !errors.Is(err, ErrFull) {
		t.Fatalf("Edit = %v, want ErrFull", err)
	}
	latest, _ := d.Latest()
	if changed, err := d.Edit(MaxRevisions, latest.Title(), latest.Body(), t0); err != nil || changed {
		t.Fatalf("unchanged edit of a full draft = %v, %v", changed, err)
	}
	if err := d.CanAppend(MaxRevisions - 1); !errors.Is(err, ErrStaleBase) {
		t.Fatalf("stale base on a full draft = %v, want ErrStaleBase first", err)
	}
	if err := draftWith(t, 2).CanAppend(2); err != nil {
		t.Fatalf("CanAppend = %v", err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	d := draftWith(t, 3)
	d.Persisted(4)
	st := d.State()
	back, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if back.Version() != 4 || back.Assignment() != "a1" || back.LatestNumber() != 3 {
		t.Fatalf("restored = %+v", back.State())
	}
	for i, r := range back.Revisions() {
		if r != d.Revisions()[i] {
			t.Fatalf("version %d: %+v != %+v", i+1, r, d.Revisions()[i])
		}
	}

	// The copies handed out do not alias the aggregate.
	st.Revisions[0].Body = "changed"
	revs := d.Revisions()
	revs[0] = Revision{}
	if r, _ := d.Revision(1); r.Body() == "changed" || r.Number() != 1 {
		t.Fatal("State or Revisions aliases the aggregate")
	}
}

func TestRestoreRejectsInconsistentState(t *testing.T) {
	valid := func() State {
		d := draftWith(t, 2)
		d.Persisted(1)
		return d.State()
	}
	cases := []struct {
		name   string
		change func(*State)
	}{
		{"never stored", func(s *State) { s.Version = 0 }},
		{"bad assignment", func(s *State) { s.Assignment = "" }},
		{"no versions", func(s *State) { s.Revisions = nil }},
		{"too many versions", func(s *State) {
			r := s.Revisions[1]
			for len(s.Revisions) <= MaxRevisions {
				r.Number = len(s.Revisions) + 1
				s.Revisions = append(s.Revisions, r)
			}
		}},
		{"gap in numbering", func(s *State) { s.Revisions[1].Number = 3 }},
		{"numbering from 0", func(s *State) { s.Revisions[0].Number = 0 }},
		{"unknown origin", func(s *State) { s.Revisions[0].Origin = "imported" }},
		{"edited with a job", func(s *State) { s.Revisions[1].Job = "j7" }},
		{"edited with an instruction", func(s *State) { s.Revisions[1].Instruction = "x" }},
		{"edited with a prompt version", func(s *State) { s.Revisions[1].PromptVersion = 1 }},
		{"generated without a job", func(s *State) { s.Revisions[0].Job = "" }},
		{"generated without a prompt version", func(s *State) { s.Revisions[0].PromptVersion = 0 }},
		{"bad instruction", func(s *State) { s.Revisions[0].Instruction = "\x00" }},
		{"empty body", func(s *State) { s.Revisions[0].Body = "" }},
		{"bad title", func(s *State) { s.Revisions[1].Title = "a\nb" }},
		{"missing time", func(s *State) { s.Revisions[1].CreatedAt = time.Time{} }},
		{"two versions of one job", func(s *State) {
			r := s.Revisions[0]
			r.Number = 3
			s.Revisions = append(s.Revisions, r)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := valid()
			tc.change(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("Restore = %v, want ErrInvalidDraft", err)
			}
		})
	}
	if _, err := Restore(valid()); err != nil {
		t.Fatalf("control: %v", err)
	}
}

func TestOriginValid(t *testing.T) {
	for _, o := range []Origin{OriginGenerated, OriginEdited} {
		if !o.Valid() {
			t.Errorf("%s invalid", o)
		}
	}
	if Origin("").Valid() || Origin("imported").Valid() {
		t.Error("unknown origin valid")
	}
}

func TestMarkdown(t *testing.T) {
	d := draftWith(t, 1)
	r, _ := d.Latest()
	if got, want := r.Markdown(), "# Rivers\n\nRivers flow to the sea.\n"; got != want {
		t.Fatalf("Markdown = %q, want %q", got, want)
	}
}
