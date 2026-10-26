package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// fakeWorks is the WorkFiles port over in-memory works, materials and subjects.
type fakeWorks struct {
	mu        sync.Mutex
	works     map[shared.WorkID]work.State
	materials map[shared.MaterialID]material.State
	subjects  []*subject.Subject
	err       error // returned by every call when set
}

func (f *fakeWorks) Work(_ context.Context, id shared.WorkID) (*work.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	st, ok := f.works[id]
	if !ok {
		return nil, fmt.Errorf("work %s: %w", id, shared.ErrNotFound)
	}
	return work.Restore(st)
}

func (f *fakeWorks) Material(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	st, ok := f.materials[id]
	if !ok {
		return nil, fmt.Errorf("material %s: %w", id, shared.ErrNotFound)
	}
	return material.Restore(st)
}

func (f *fakeWorks) Subjects(context.Context) ([]*subject.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subjects, f.err
}

// change applies fn to the stored material id, as the library would.
func (f *fakeWorks) change(t *testing.T, id shared.MaterialID, fn func(m *material.Material) (bool, error)) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := material.Restore(f.materials[id])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn(m); err != nil {
		t.Fatal(err)
	}
	st := m.State()
	st.Version++
	f.materials[id] = st
}

var _ WorkFiles = (*fakeWorks)(nil)

type workEnv struct {
	*env
	works *fakeWorks
	sub   *subject.Subject
}

const paperBytes = "PK paper of the work"

// newWorkEnv is a Service with the works: the done work w1 of assignment a1, whose paper m1 «Реферат.docx» is filed
// in «ДЗ» of subject s1, and the recipient of the settings.
func newWorkEnv(t *testing.T) *workEnv {
	t.Helper()
	e := &workEnv{env: newEnv(t), works: &fakeWorks{works: map[shared.WorkID]work.State{}, materials: map[shared.MaterialID]material.State{}}}
	e.assignments["a1"] = true
	e.recipient = "teacher@school.example"
	sub, err := subject.New(subject.Spec{ID: "s1", Name: "История", Semester: libvalues.SemesterOf(t0)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.sub, e.works.subjects = sub, []*subject.Subject{sub}
	m := e.addMaterial(t, "m1", "Реферат.docx", paperBytes, material.TypeDOCX)
	e.addDoneWork(t, "w1", work.Source{Assignment: "a1"}, "s1", m)

	svc, err := New(Deps{
		Proposals: proposalRepo{e.store}, Assignments: assignmentRepo{e.store}, AssignmentList: assignmentList{e.store},
		Artifacts: e.files, Recipients: recipients{e.store}, Mailer: e.mailer, Exporter: e.exporter, Calendar: e.calendar,
		Recordings: noRecordings{}, Lessons: noLessons{}, Clock: e.clock, IDs: ids{e.store}, Tx: tx{e.store},
		Waiting: waitingList{e.store}, Works: e.works,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// addMaterial stores data as the library does (materials/<sha256>) and files it in «ДЗ» of s1.
func (e *workEnv) addMaterial(t *testing.T, id shared.MaterialID, name, data string, typ material.Type) *material.Material {
	t.Helper()
	m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf([]byte(data)), Size: int64(len(data)), Type: typ, Name: name}, t0)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := e.sub.SystemFolder(subject.SystemHomework)
	if _, err := m.Place("s1", f.ID(), t0); err != nil {
		t.Fatal(err)
	}
	ref, err := material.BlobRef(m.Digest())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.files.Write(context.Background(), ref, strings.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	st := m.State()
	st.Version = 1
	e.works.mu.Lock()
	e.works.materials[id] = st
	e.works.mu.Unlock()
	return m
}

// addDoneWork stores a done paper of src whose file is m; sub is the subject of its lesson ("" none).
func (e *workEnv) addDoneWork(t *testing.T, id shared.WorkID, src work.Source, sub shared.SubjectID, m *material.Material) {
	t.Helper()
	w, err := work.New(id, src, t0)
	if err != nil {
		t.Fatal(err)
	}
	answer := work.Answer{
		WorkType: work.TypeReferat, Requirements: work.Requirements{Volume: "10 страниц"}, Feasibility: work.FeasibleFull,
		Questions: []work.Question{}, Assumptions: []string{"Объём по умолчанию"},
	}
	o, err := work.ParseOutline(`{"topic":"Русь","sections":[{"title":"Введение","words":200}]}`)
	if err != nil {
		t.Fatal(err)
	}
	text, err := work.ParseSection(`{"paragraphs":["Абзац."],"sources":[]}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return w.Record(1, answer, t0) },
		func() error { return w.ConfirmRequirements(t0) },
		func() error {
			return w.StartWriting(work.Snapshot{Subject: sub, SubjectName: "История", Title: "Реферат"}, t0)
		},
		func() error { return w.SetOutline(o, t0) },
		func() error { return w.ApproveOutline(t0) },
		func() error { return w.WriteSection(1, 1, text, t0) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if m != nil {
		ref, _ := material.BlobRef(m.Digest())
		if err := w.Finish(work.File{Material: m.ID(), Artifact: ref.String(), Content: w.ContentKey()}, t0); err != nil {
			t.Fatal(err)
		}
	}
	st := w.State()
	st.Version = 1
	e.works.mu.Lock()
	e.works.works[id] = st
	e.works.mu.Unlock()
}

func (e *workEnv) propose(t *testing.T, id shared.WorkID) ProposalView {
	t.Helper()
	v, err := e.svc.ProposeSendWork(context.Background(), id)
	if err != nil {
		t.Fatalf("ProposeSendWork: %v", err)
	}
	return v
}

func (e *workEnv) read(t *testing.T, ref shared.ArtifactRef) string {
	t.Helper()
	rc, err := e.files.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The confirmation path: the card shows the settings' address, the subject and the file's own name; «Да» copies the
// pinned file under that name and sends the copy once, keyed by the proposal.
func TestSendWorkConfirmPath(t *testing.T) {
	e := newWorkEnv(t)
	v := e.propose(t, "w1")
	if v.Status != proposal.StatusValidated || v.Kind != proposal.KindSendWork || v.Recipient != "teacher@school.example" ||
		v.Subject == "" || v.AttachmentName != "Реферат.docx" || v.AttachmentSize != int64(len(paperBytes)) ||
		v.AttachmentFormat != proposal.FormatDocx || v.WorkID != "w1" || v.AssignmentID != "a1" {
		t.Fatalf("proposal view %+v", v)
	}
	if n := len(e.mailer.Calls()); n != 0 {
		t.Fatalf("%d emails before «Да»", n)
	}

	done, err := e.svc.ConfirmSendWork(context.Background(), v.ID, v.Subject, v.AttachmentFormat)
	if err != nil || done.Status != proposal.StatusExecuted {
		t.Fatalf("ConfirmSendWork = %+v, %v", done, err)
	}
	calls := e.mailer.Calls()
	if len(calls) != 1 {
		t.Fatalf("%d emails, want 1", len(calls))
	}
	cmd := calls[0]
	want := "exports/sent/" + string(v.ID) + "/Реферат.docx"
	if cmd.IdempotencyKey() != "proposal:"+string(v.ID) || cmd.Attachment().String() != want || cmd.Subject() != v.Subject ||
		cmd.Recipient().Address() != "teacher@school.example" || cmd.AssignmentID() != "a1" ||
		cmd.AttachmentDigest().Size() != int64(len(paperBytes)) {
		t.Fatalf("command %+v", cmd)
	}
	if got := e.read(t, cmd.Attachment()); got != paperBytes {
		t.Fatalf("the copy holds %q", got)
	}
}

// A second «Да», also after a failed send, is safe: an executed send sends nothing more, and a confirmed one whose
// mailer call failed (an outcome the mailer could not tell) repeats the same command under the same key, which the
// delivery flow never turns into a second email.
func TestSendWorkConfirmTwice(t *testing.T) {
	ctx := context.Background()
	e := newWorkEnv(t)
	v := e.propose(t, "w1")
	for range 2 {
		if got, err := e.svc.ConfirmSendWork(ctx, v.ID, v.Subject, v.AttachmentFormat); err != nil || got.Status != proposal.StatusExecuted {
			t.Fatalf("ConfirmSendWork = %+v, %v", got, err)
		}
	}
	if n := len(e.mailer.Calls()); n != 1 {
		t.Fatalf("%d emails after two «Да», want 1", n)
	}

	f := newWorkEnv(t)
	w := f.propose(t, "w1")
	f.mailer.Fail(errors.New("smtp: connection reset after DATA"))
	if _, err := f.svc.ConfirmSendWork(ctx, w.ID, w.Subject, w.AttachmentFormat); err == nil {
		t.Fatal("a failed send reported success")
	}
	if got, _ := f.svc.Get(ctx, w.ID); got.Status != proposal.StatusConfirmed {
		t.Fatalf("after a failed send %s, want CONFIRMED", got.Status)
	}
	f.mailer.Fail(nil)
	if got, err := f.svc.ConfirmSendWork(ctx, w.ID, w.Subject, w.AttachmentFormat); err != nil || got.Status != proposal.StatusExecuted {
		t.Fatalf("second «Да» = %+v, %v", got, err)
	}
	calls := f.mailer.Calls()
	if len(calls) != 2 || calls[0].IdempotencyKey() != calls[1].IdempotencyKey() || calls[0] != calls[1] {
		t.Fatalf("repeat sent another command: %+v", calls)
	}
}

// What the student confirmed must hold at «Да»: else STALE with the reason and no email.
func TestSendWorkStale(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, e *workEnv)
		format proposal.Format
		reason proposal.RejectReason
	}{
		{"file changed after the proposal (a new version of the work)", func(t *testing.T, e *workEnv) {
			m := e.addMaterial(t, "m2", "Реферат.docx", "PK another paper", material.TypeDOCX)
			e.addDoneWork(t, "w1", work.Source{Assignment: "a1"}, "s1", m)
		}, proposal.FormatDocx, proposal.ReasonArtifactChanged},
		{"stored file damaged", func(t *testing.T, e *workEnv) {
			ref, _ := material.BlobRef(material.DigestOf([]byte(paperBytes)))
			if err := e.files.Write(context.Background(), ref, strings.NewReader("PK tampered!!!!!!!!!")); err != nil {
				t.Fatal(err)
			}
		}, proposal.FormatDocx, proposal.ReasonNoArtifact},
		{"file deleted from the library", func(t *testing.T, e *workEnv) {
			e.works.change(t, "m1", func(m *material.Material) (bool, error) { return m.Remove(t0.Add(time.Minute)) })
		}, proposal.FormatDocx, proposal.ReasonNoArtifact},
		{"work deleted", func(_ *testing.T, e *workEnv) {
			e.works.mu.Lock()
			delete(e.works.works, "w1")
			e.works.mu.Unlock()
		}, proposal.FormatDocx, proposal.ReasonNoArtifact},
		{"file moved from ДЗ to «Входящие»", func(t *testing.T, e *workEnv) {
			e.works.change(t, "m1", func(m *material.Material) (bool, error) { return m.MoveToInbox(t0.Add(time.Minute)) })
		}, proposal.FormatDocx, proposal.ReasonFileMoved},
		{"file moved to «Материалы»", func(t *testing.T, e *workEnv) {
			f, _ := e.sub.SystemFolder(subject.SystemMaterials)
			e.works.change(t, "m1", func(m *material.Material) (bool, error) { return m.Place("s1", f.ID(), t0.Add(time.Minute)) })
		}, proposal.FormatDocx, proposal.ReasonFileMoved},
		{"recipient removed", func(_ *testing.T, e *workEnv) { e.recipient = "" }, proposal.FormatDocx, proposal.ReasonRecipientChanged},
		{"another format shown", func(*testing.T, *workEnv) {}, proposal.FormatPptx, proposal.ReasonFormatChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			e := newWorkEnv(t)
			v := e.propose(t, "w1")
			tc.change(t, e)
			if _, err := e.svc.ConfirmSendWork(ctx, v.ID, v.Subject, tc.format); !errors.Is(err, proposal.ErrStale) {
				t.Fatalf("ConfirmSendWork = %v, want ErrStale", err)
			}
			got, err := e.svc.Get(ctx, v.ID)
			if err != nil || got.Status != proposal.StatusStale || got.Reason != tc.reason {
				t.Fatalf("proposal %+v, %v; want STALE %s", got, err, tc.reason)
			}
			if n := len(e.mailer.Calls()); n != 0 {
				t.Fatalf("%d emails from a stale send", n)
			}
		})
	}
}

// A send that cannot be proposed is stored REJECTED with its reason and offers no «Да».
func TestSendWorkRejected(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, e *workEnv)
		reason proposal.RejectReason
	}{
		{"no recipient", func(_ *testing.T, e *workEnv) { e.recipient = "" }, proposal.ReasonNoRecipient},
		{"work not finished", func(t *testing.T, e *workEnv) { e.addDoneWork(t, "w1", work.Source{Assignment: "a1"}, "s1", nil) }, proposal.ReasonNoArtifact},
		{"not in ДЗ", func(t *testing.T, e *workEnv) {
			e.works.change(t, "m1", func(m *material.Material) (bool, error) { return m.MoveToInbox(t0) })
		}, proposal.ReasonFileMoved},
		{"a presentation file for a paper", func(t *testing.T, e *workEnv) {
			m := e.addMaterial(t, "m3", "Доклад.pptx", "PK slides", material.TypePPTX)
			e.addDoneWork(t, "w1", work.Source{Assignment: "a1"}, "s1", m)
		}, proposal.ReasonNoArtifact},
		{"assignment gone", func(_ *testing.T, e *workEnv) { delete(e.assignments, "a1") }, proposal.ReasonUnknownAssignment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newWorkEnv(t)
			tc.setup(t, e)
			v := e.propose(t, "w1")
			if v.Status != proposal.StatusRejected || v.Reason != tc.reason {
				t.Fatalf("proposal %s %s, want REJECTED %s", v.Status, v.Reason, tc.reason)
			}
			if _, err := e.svc.ConfirmSendWork(context.Background(), v.ID, v.Subject, proposal.FormatDocx); !errors.Is(err, proposal.ErrInvalidTransition) {
				t.Fatalf("«Да» on a rejected send = %v", err)
			}
			if n := len(e.mailer.Calls()); n != 0 {
				t.Fatalf("%d emails", n)
			}
		})
	}
}

// A work without an assignment (an uploaded file or a topic) is never sent: no_assignment, nothing stored.
func TestSendWorkWithoutAssignment(t *testing.T) {
	topic, err := work.NewTopic("req-1", "Реформы Петра", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]work.Source{"uploaded file": {Material: "m9"}, "topic": {Topic: topic}} {
		t.Run(name, func(t *testing.T) {
			e := newWorkEnv(t)
			m := e.addMaterial(t, "m2", "Работа.docx", "PK topic paper", material.TypeDOCX)
			e.addDoneWork(t, "w2", src, "s1", m)
			if v, err := e.svc.ProposeSendWork(context.Background(), "w2"); !errors.Is(err, work.ErrNoAssignment) {
				t.Fatalf("ProposeSendWork = %+v, %v; want work.ErrNoAssignment", v, err)
			}
			if n := len(e.proposals); n != 0 {
				t.Fatalf("%d proposals stored", n)
			}
		})
	}
}

// A work without a subject keeps its paper in «Входящие»: that is where it was filed, so it may be sent.
func TestSendWorkWithoutSubjectFromInbox(t *testing.T) {
	e := newWorkEnv(t)
	e.works.change(t, "m1", func(m *material.Material) (bool, error) { return m.MoveToInbox(t0) })
	e.addDoneWork(t, "w1", work.Source{Assignment: "a1"}, "", e.material(t, "m1"))
	if v := e.propose(t, "w1"); v.Status != proposal.StatusValidated {
		t.Fatalf("proposal %s %s, want VALIDATED", v.Status, v.Reason)
	}
	// The same for a subject removed since: the library left the paper in «Входящие».
	e.addDoneWork(t, "w1", work.Source{Assignment: "a1"}, "s-gone", e.material(t, "m1"))
	if v := e.propose(t, "w1"); v.Status != proposal.StatusValidated {
		t.Fatalf("proposal %s %s, want VALIDATED", v.Status, v.Reason)
	}
}

func (e *workEnv) material(t *testing.T, id shared.MaterialID) *material.Material {
	t.Helper()
	m, err := e.works.Material(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Pressing «Отправить преподавателю» again returns the send that waits for «Да»; a new file is a new send.
func TestSendWorkReusesTheWaitingSend(t *testing.T) {
	e := newWorkEnv(t)
	first := e.propose(t, "w1")
	if again := e.propose(t, "w1"); again.ID != first.ID {
		t.Fatalf("second press made %s, want %s", again.ID, first.ID)
	}
	if n := len(e.proposals); n != 1 {
		t.Fatalf("%d proposals stored, want 1", n)
	}
	m := e.addMaterial(t, "m2", "Реферат.docx", "PK new version", material.TypeDOCX)
	e.addDoneWork(t, "w1", work.Source{Assignment: "a1"}, "s1", m)
	if other := e.propose(t, "w1"); other.ID == first.ID {
		t.Fatal("a send of another file reused the old one")
	}
}

// Each confirmation is for its own kind; the services refuse without the works.
func TestSendWorkKindsAndWiring(t *testing.T) {
	ctx := context.Background()
	e := newWorkEnv(t)
	v := e.propose(t, "w1")
	if _, err := e.svc.ConfirmSend(ctx, v.ID, v.Subject, proposal.FormatDocx); !errors.Is(err, ErrNotASend) {
		t.Fatalf("ConfirmSend of a work send = %v", err)
	}
	ref, _ := proposal.ExportRef("a1")
	if err := e.files.Write(ctx, ref, strings.NewReader("my homework")); err != nil {
		t.Fatal(err)
	}
	hw, err := e.svc.ProposeSend(ctx, "a1", proposal.FormatText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmSendWork(ctx, hw.ID, hw.Subject, proposal.FormatText); !errors.Is(err, ErrNotASend) {
		t.Fatalf("ConfirmSendWork of a homework send = %v", err)
	}
	if n := len(e.mailer.Calls()); n != 0 {
		t.Fatalf("%d emails", n)
	}
	if _, err := newEnv(t).svc.ProposeSendWork(ctx, "w1"); !errors.Is(err, ErrNoWorks) {
		t.Fatalf("without the works: %v", err)
	}
	if _, err := e.svc.ProposeSendWork(ctx, "nope"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing work: %v", err)
	}
	if _, err := e.svc.ProposeSendWork(ctx, "../w"); err == nil {
		t.Fatal("an invalid work id was accepted")
	}
	e.works.err = errors.New("disk I/O error")
	if _, err := e.svc.Get(ctx, v.ID); err == nil {
		t.Fatal("a failed read of the work was hidden")
	}
}
