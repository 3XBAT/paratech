package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

var hwT0 = time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)

// hwCalls is the call log shared by the homework fakes, so tests can check the order across them.
type hwCalls struct {
	mu    sync.Mutex
	calls []string
}

func (c *hwCalls) add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, name)
}

func (c *hwCalls) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

// fakeHWProposals is HomeworkProposals; err, when set, is returned by every method, and the per-method errors override it.
type fakeHWProposals struct {
	log          *hwCalls
	err          error
	confirmErr   error // returned by ConfirmSend
	declineErr   error
	proposal     homeworkflow.ProposalView // returned by ProposeSend and Get
	list         []homeworkflow.AssignmentView
	one          *homeworkflow.AssignmentView  // returned by Assignment when set
	corrected    homeworkflow.LessonCorrection // the last CorrectLesson
	shownSubject string                        // the last ConfirmSend
	shownFormat  proposal.Format               // the last ConfirmSend
	chosen       homeworkflow.ChosenTeacher    // the last ConfirmSend
	format       proposal.Format               // the last ProposeSend
	createErr    error                         // returned by CreateAssignment
	created      map[shared.AssignmentID]homeworkflow.NewAssignment
	createCalls  []homeworkflow.NewAssignment // every CreateAssignment input
	deleteErr    error                        // returned by DeleteAssignment
	restoreErr   error                        // returned by RestoreAssignment
	editErr      error                        // returned by EditAssignment
	versions     []int64                      // every expected version DeleteAssignment and RestoreAssignment got
	edits        []homeworkflow.AssignmentEdit
	states       []homeworkflow.ListState                            // every ListAssignmentsByState filter
	doneCodes    map[shared.AssignmentID]homeworkflow.DoneCode       // MarkDone and RestoreDone codes; default changed
	doneErr      error                                               // returned by MarkDone and RestoreDone
	marked       [][]shared.AssignmentID                             // every MarkDone list
	markedDone   []bool                                              // every MarkDone state
	restored     [][]homeworkflow.DonePrior                          // every RestoreDone prior
	doneOutcomes func(homeworkflow.DoneMarks) homeworkflow.DoneMarks // edits what MarkDone and RestoreDone return
}

func (f *fakeHWProposals) ListAssignmentsByState(_ context.Context, state homeworkflow.ListState) ([]homeworkflow.AssignmentView, error) {
	f.log.add("ListAssignments")
	f.states = append(f.states, state)
	return f.list, f.err
}

// MarkDone answers like the use case: one outcome per distinct id, changed ones at version 8 with their prior.
func (f *fakeHWProposals) MarkDone(_ context.Context, ids []shared.AssignmentID, done bool) (homeworkflow.DoneMarks, error) {
	f.log.add("MarkDone")
	f.marked, f.markedDone = append(f.marked, ids), append(f.markedDone, done)
	if err := cmpErr(f.doneErr, f.err); err != nil {
		return homeworkflow.DoneMarks{}, err
	}
	var m homeworkflow.DoneMarks
	for _, id := range ids {
		f.doneOutcome(&m, id, done, 8)
	}
	return f.edited(m), nil
}

// RestoreDone answers like the use case: a changed one is saved at the expected version + 1.
func (f *fakeHWProposals) RestoreDone(_ context.Context, prior []homeworkflow.DonePrior) (homeworkflow.DoneMarks, error) {
	f.log.add("RestoreDone")
	f.restored = append(f.restored, prior)
	if err := cmpErr(f.doneErr, f.err); err != nil {
		return homeworkflow.DoneMarks{}, err
	}
	var m homeworkflow.DoneMarks
	for _, p := range prior {
		f.doneOutcome(&m, p.ID, p.Done, p.Version+1)
	}
	return f.edited(m), nil
}

func (f *fakeHWProposals) doneOutcome(m *homeworkflow.DoneMarks, id shared.AssignmentID, done bool, version int64) {
	code, ok := f.doneCodes[id]
	if !ok {
		code = homeworkflow.DoneChanged
	}
	o := homeworkflow.DoneOutcome{ID: id, Code: code}
	switch code {
	case homeworkflow.DoneChanged:
		o.Done, o.Version = done, version
		m.Prior = append(m.Prior, homeworkflow.DonePrior{ID: id, Done: !done, Version: version})
	case homeworkflow.DoneUnchanged:
		o.Done, o.Version = done, 3
	case homeworkflow.DoneFailed:
		o.Err = errors.New(`save: open C:\Users\student\db: locked`)
	default:
		o.Err = errors.New(string(code))
	}
	m.Outcomes = append(m.Outcomes, o)
}

func (f *fakeHWProposals) edited(m homeworkflow.DoneMarks) homeworkflow.DoneMarks {
	if f.doneOutcomes != nil {
		return f.doneOutcomes(m)
	}
	return m
}

func (f *fakeHWProposals) Assignment(_ context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	f.log.add("Assignment")
	if f.one != nil {
		return *f.one, f.err
	}
	return homeworkflow.AssignmentView{ID: id, Title: "Rivers", UpdatedAt: hwT0}, f.err
}

func (f *fakeHWProposals) CorrectLesson(_ context.Context, c homeworkflow.LessonCorrection) (homeworkflow.AssignmentView, error) {
	f.log.add("CorrectLesson")
	f.corrected = c
	return homeworkflow.AssignmentView{
		ID: c.ID, Title: "Rivers", UpdatedAt: hwT0, Subject: c.Subject, SubjectSource: values.SourceUser,
		CorrectedSubject: c.Subject,
	}, f.err
}

// CreateAssignment stores by ID like the use case: the same ID is the same assignment.
func (f *fakeHWProposals) CreateAssignment(_ context.Context, in homeworkflow.NewAssignment) (homeworkflow.AssignmentView, error) {
	f.log.add("CreateAssignment")
	f.createCalls = append(f.createCalls, in)
	if err := cmpErr(f.createErr, f.err); err != nil {
		return homeworkflow.AssignmentView{}, err
	}
	if f.created == nil {
		f.created = map[shared.AssignmentID]homeworkflow.NewAssignment{}
	}
	f.created[in.ID] = in
	return homeworkflow.AssignmentView{ID: in.ID, Title: in.Title, Description: in.Note, UpdatedAt: hwT0}, nil
}

// DeleteAssignment and RestoreAssignment bump the version like the use case.
func (f *fakeHWProposals) DeleteAssignment(_ context.Context, id shared.AssignmentID, v int64) (homeworkflow.AssignmentView, error) {
	f.log.add("DeleteAssignment")
	f.versions = append(f.versions, v)
	if err := cmpErr(f.deleteErr, f.err); err != nil {
		return homeworkflow.AssignmentView{}, err
	}
	return homeworkflow.AssignmentView{ID: id, Title: "Rivers", Version: v + 1, UpdatedAt: hwT0, Deleted: true}, nil
}

func (f *fakeHWProposals) RestoreAssignment(_ context.Context, id shared.AssignmentID, v int64) (homeworkflow.AssignmentView, error) {
	f.log.add("RestoreAssignment")
	f.versions = append(f.versions, v)
	if err := cmpErr(f.restoreErr, f.err); err != nil {
		return homeworkflow.AssignmentView{}, err
	}
	return homeworkflow.AssignmentView{ID: id, Title: "Rivers", Version: v + 1, UpdatedAt: hwT0}, nil
}

func (f *fakeHWProposals) EditAssignment(_ context.Context, e homeworkflow.AssignmentEdit) (homeworkflow.AssignmentView, error) {
	f.log.add("EditAssignment")
	f.edits = append(f.edits, e)
	if err := cmpErr(f.editErr, f.err); err != nil {
		return homeworkflow.AssignmentView{}, err
	}
	return homeworkflow.AssignmentView{
		ID: e.ID, Title: e.Title, Description: e.Note, UserEdited: true, Version: e.ExpectedVersion + 1, UpdatedAt: hwT0,
	}, nil
}

func (f *fakeHWProposals) ProposeSend(_ context.Context, _ shared.AssignmentID, format proposal.Format) (homeworkflow.ProposalView, error) {
	f.log.add("ProposeSend")
	f.format = format
	return f.proposal, f.err
}

func (f *fakeHWProposals) Get(context.Context, proposal.ID) (homeworkflow.ProposalView, error) {
	f.log.add("Get")
	return f.proposal, f.err
}

func (f *fakeHWProposals) ConfirmSendTo(_ context.Context, _ proposal.ID, subject string, format proposal.Format,
	chosen homeworkflow.ChosenTeacher) (homeworkflow.ProposalView, error) {
	f.log.add("ConfirmSend")
	f.shownSubject, f.shownFormat, f.chosen = subject, format, chosen
	return f.proposal, cmpErr(f.confirmErr, f.err)
}

func (f *fakeHWProposals) Decline(context.Context, proposal.ID) (homeworkflow.ProposalView, error) {
	f.log.add("Decline")
	return f.proposal, cmpErr(f.declineErr, f.err)
}

func cmpErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// fakeHWDrafts is HomeworkDrafts.
type fakeHWDrafts struct {
	log        *hwCalls
	err        error
	versions   []homeworkflow.DraftVersionView
	generation homeworkflow.GenerationView
	active     *homeworkflow.GenerationView // returned by ActiveGeneration; nil: none
	exportErr  error                        // returned by ExportDraft and ExportDocx
	saveErr    error                        // returned by SaveDocx
	savedTo    string                       // the last SaveDocx path
	gotBase    int
	gotEdit    homeworkflow.DraftEdit
}

func (f *fakeHWDrafts) DraftVersions(context.Context, shared.AssignmentID) ([]homeworkflow.DraftVersionView, error) {
	f.log.add("DraftVersions")
	return f.versions, f.err
}

func (f *fakeHWDrafts) DraftVersion(_ context.Context, a shared.AssignmentID, n int) (homeworkflow.DraftVersionView, error) {
	f.log.add("DraftVersion")
	return homeworkflow.DraftVersionView{Assignment: a, Number: n, Origin: homeworkdraft.OriginEdited, Title: "T", Body: "B"}, f.err
}

func (f *fakeHWDrafts) GenerateFrom(_ context.Context, _ shared.AssignmentID, base int, _ string) (homeworkflow.GenerateResult, error) {
	f.log.add("GenerateFrom")
	f.gotBase = base
	return homeworkflow.GenerateResult{JobID: "j1", Created: true, Base: base}, f.err
}

func (f *fakeHWDrafts) Generation(context.Context, shared.JobID) (homeworkflow.GenerationView, error) {
	f.log.add("Generation")
	return f.generation, f.err
}

func (f *fakeHWDrafts) EditDraft(_ context.Context, e homeworkflow.DraftEdit) (homeworkflow.DraftVersionView, error) {
	f.log.add("EditDraft")
	f.gotEdit = e
	return homeworkflow.DraftVersionView{Assignment: e.Assignment, Number: e.Base + 1, Origin: homeworkdraft.OriginEdited,
		Title: e.Title, Body: e.Body, Latest: true, CreatedAt: hwT0}, f.err
}

func (f *fakeHWDrafts) ExportDraft(_ context.Context, a shared.AssignmentID, n int) (homeworkflow.DraftExport, error) {
	f.log.add("ExportDraft")
	if f.exportErr != nil {
		return homeworkflow.DraftExport{}, f.exportErr
	}
	ref, err := proposal.ExportRef(a)
	if err != nil {
		return homeworkflow.DraftExport{}, err
	}
	return homeworkflow.DraftExport{Version: n, File: ref}, f.err
}

func (f *fakeHWDrafts) ActiveGeneration(context.Context, shared.AssignmentID) (homeworkflow.GenerationView, bool, error) {
	f.log.add("ActiveGeneration")
	if f.active == nil {
		return homeworkflow.GenerationView{}, false, f.err
	}
	return *f.active, true, f.err
}

func (f *fakeHWDrafts) ExportDocx(_ context.Context, a shared.AssignmentID, n int) (homeworkflow.DraftExport, error) {
	f.log.add("ExportDocx")
	if f.exportErr != nil {
		return homeworkflow.DraftExport{}, f.exportErr
	}
	ref, err := proposal.ExportRefOf(a, proposal.FormatDocx)
	if err != nil {
		return homeworkflow.DraftExport{}, err
	}
	return homeworkflow.DraftExport{Version: n, File: ref}, f.err
}

func (f *fakeHWDrafts) SaveDocx(_ context.Context, _ shared.AssignmentID, _ int, path string) error {
	f.log.add("SaveDocx")
	f.savedTo = path
	return cmpErr(f.saveErr, f.err)
}

func (f *fakeHWDrafts) DocxName(context.Context, shared.AssignmentID) (string, error) {
	f.log.add("DocxName")
	return "Rivers.docx", f.err
}

// fakeDocxPicker is the save dialog of a Word file: the path it answers and an error.
type fakeDocxPicker struct {
	path string
	err  error
	name string // the proposed name
}

func (f *fakeDocxPicker) PickDocxFile(_ context.Context, name string) (string, error) {
	f.name = name
	return f.path, f.err
}

// fakeHWDeliveries is HomeworkDeliveries.
type fakeHWDeliveries struct {
	log   *hwCalls
	err   error
	views []senddelivery.View
}

func (f *fakeHWDeliveries) ListForAssignment(context.Context, shared.AssignmentID) ([]senddelivery.View, error) {
	f.log.add("ListForAssignment")
	return f.views, f.err
}

func (f *fakeHWDeliveries) Subject() string { return "Домашнее задание" }

func (f *fakeHWDeliveries) MarkSent(_ context.Context, id shared.DeliveryID) (senddelivery.View, error) {
	f.log.add("MarkSent")
	return senddelivery.View{ID: id, Status: delivery.StatusSent}, f.err
}

func (f *fakeHWDeliveries) SendAgain(_ context.Context, id shared.DeliveryID) (senddelivery.View, error) {
	f.log.add("SendAgain")
	return senddelivery.View{ID: id, Status: delivery.StatusPending}, f.err
}

// hwEnv is a HomeworkService over the fakes, logging into logs.
type hwEnv struct {
	calls      *hwCalls
	proposals  *fakeHWProposals
	drafts     *fakeHWDrafts
	deliveries *fakeHWDeliveries
	picker     *fakeDocxPicker
	logs       *bytes.Buffer
	svc        *HomeworkService
}

func newHWEnv() *hwEnv {
	calls := &hwCalls{}
	e := &hwEnv{
		calls: calls, logs: &bytes.Buffer{},
		proposals: &fakeHWProposals{log: calls, proposal: homeworkflow.ProposalView{
			ID: "p1", Kind: proposal.KindSendHomework, Status: proposal.StatusValidated, AssignmentID: "a1",
			Recipient: "teacher@school.example", AttachmentName: "homework.md", AttachmentSize: 42,
		}},
		drafts:     &fakeHWDrafts{log: calls},
		deliveries: &fakeHWDeliveries{log: calls},
		picker:     &fakeDocxPicker{path: `C:\Users\student\Documents\Rivers.docx`},
	}
	e.svc = NewHomeworkService(e.core(), slog.New(slog.NewTextHandler(e.logs, nil))).withDocxPicker(e.picker)
	return e
}

func (e *hwEnv) core() HomeworkCore {
	return HomeworkCore{Proposals: e.proposals, Drafts: e.drafts, Deliveries: e.deliveries}
}

// validHomework is a complete HomeworkCore for the shell tests.
func validHomework() HomeworkCore { return newHWEnv().core() }

func (e *hwEnv) called(name string) bool { return slices.Contains(e.calls.list(), name) }

// hwCall is one binding call with fixed arguments; id replaces the id argument when not empty.
type hwCall struct {
	name string
	call func(s *HomeworkService, id string) error
}

// everyHomeworkCall calls every method of HomeworkService; the id argument is the one the method validates.
var everyHomeworkCall = []hwCall{
	{"ListAssignments", func(s *HomeworkService, _ string) error {
		_, err := s.ListAssignments(context.Background(), "")
		return err
	}},
	// A malformed id is not_found before any use case call; a failure is a problem code, shown as the page's error.
	{"MarkDone", func(s *HomeworkService, id string) error {
		return doneErr(s.MarkDone(context.Background(), []string{id}, true))
	}},
	{"RestoreDone", func(s *HomeworkService, id string) error {
		return doneErr(s.RestoreDone(context.Background(), false, []DonePriorItem{{ID: id, Version: 8}}))
	}},
	{"OpenAssignment", func(s *HomeworkService, id string) error {
		_, err := s.OpenAssignment(context.Background(), id)
		return err
	}},
	{"CorrectLesson", func(s *HomeworkService, id string) error {
		_, err := s.CorrectLesson(context.Background(), id, "Физика", "")
		return err
	}},
	// The request token is the id the method validates; a failure is a problem code, shown here as the page's error.
	{"CreateAssignment", func(s *HomeworkService, id string) error {
		res := s.CreateAssignment(context.Background(), CreateRequest{RequestID: id, Title: "Read chapter 3"})
		switch {
		case res.Problem == createInvalid && res.Field == fieldRequestID:
			return ErrHomeworkInvalidID
		case res.Problem == createFailed:
			return ErrHomeworkUnavailable
		case res.Problem != "":
			return errors.New(res.Problem)
		}
		return nil
	}},
	// An invalid id is not_found before any use case call (TestHomeworkValidatesIDs checks the calls).
	{"DeleteAssignment", func(s *HomeworkService, id string) error {
		return changeErr(s.DeleteAssignment(context.Background(), id, 1).Problem)
	}},
	{"RestoreAssignment", func(s *HomeworkService, id string) error {
		return changeErr(s.RestoreAssignment(context.Background(), id, 2).Problem)
	}},
	{"EditAssignment", func(s *HomeworkService, id string) error {
		return changeErr(s.EditAssignment(context.Background(), EditRequest{ID: id, Version: 1, Title: "Read chapter 3"}).Problem)
	}},
	{"DraftVersion", func(s *HomeworkService, id string) error {
		_, err := s.DraftVersion(context.Background(), id, 1)
		return err
	}},
	{"Generate", func(s *HomeworkService, id string) error {
		_, err := s.Generate(context.Background(), id, 1, "shorter")
		return err
	}},
	{"GenerationStatus", func(s *HomeworkService, id string) error {
		_, err := s.GenerationStatus(context.Background(), id)
		return err
	}},
	{"EditDraft", func(s *HomeworkService, id string) error {
		_, err := s.EditDraft(context.Background(), id, 1, "Title", "Body")
		return err
	}},
	{"ExportDraft", func(s *HomeworkService, id string) error {
		_, err := s.ExportDraft(context.Background(), id, 1)
		return err
	}},
	{"ExportDocx", func(s *HomeworkService, id string) error {
		_, err := s.ExportDocx(context.Background(), id, 1)
		return err
	}},
	{"SaveDocx", func(s *HomeworkService, id string) error {
		_, err := s.SaveDocx(context.Background(), id, 1)
		return err
	}},
	{"DraftStatus", func(s *HomeworkService, id string) error {
		_, err := s.DraftStatus(context.Background(), id)
		return err
	}},
	{"PrepareSend", func(s *HomeworkService, id string) error {
		_, err := s.PrepareSend(context.Background(), id, "txt")
		return err
	}},
	{"ConfirmSend", func(s *HomeworkService, id string) error {
		return s.ConfirmSend(context.Background(), id, "ДЗ", "txt", "", "")
	}},
	{"CancelSend", func(s *HomeworkService, id string) error { return s.CancelSend(context.Background(), id) }},
	{"Deliveries", func(s *HomeworkService, id string) error {
		_, err := s.Deliveries(context.Background(), id)
		return err
	}},
	{"MarkSent", func(s *HomeworkService, id string) error { _, err := s.MarkSent(context.Background(), id); return err }},
	{"SendAgain", func(s *HomeworkService, id string) error { _, err := s.SendAgain(context.Background(), id); return err }},
}

// changeErr shows a problem code of DeleteAssignment, RestoreAssignment or EditAssignment as the page's error.
func changeErr(problem string) error {
	switch problem {
	case "":
		return nil
	case changeNotFound:
		return ErrHomeworkInvalidID
	case createFailed:
		return ErrHomeworkUnavailable
	}
	return errors.New(problem)
}

// doneErr shows a DoneResult of one id as the page's error.
func doneErr(res DoneResult) error {
	switch {
	case res.Problem == doneUnavailable:
		return ErrHomeworkUnavailable
	case res.Problem != "":
		return errors.New(res.Problem)
	case len(res.Outcomes) == 1 && res.Outcomes[0].Code == string(homeworkflow.DoneNotFound):
		return ErrHomeworkInvalidID
	case len(res.Outcomes) != 1 || res.Outcomes[0].Code != string(homeworkflow.DoneChanged):
		return fmt.Errorf("outcomes %+v", res.Outcomes)
	}
	return nil
}

func TestEveryHomeworkMethodIsCovered(t *testing.T) {
	if n := len(everyHomeworkCall); n != reflect.TypeFor[*HomeworkService]().NumMethod() {
		t.Fatalf("everyHomeworkCall has %d calls, HomeworkService has %d methods", n, reflect.TypeFor[*HomeworkService]().NumMethod())
	}
}

// Ids are checked before any use case is called.
func TestHomeworkValidatesIDs(t *testing.T) {
	for _, c := range everyHomeworkCall {
		if c.name == "ListAssignments" {
			continue
		}
		for _, id := range []string{"", "a b", "x\n", strings.Repeat("x", 300)} {
			e := newHWEnv()
			if err := c.call(e.svc, id); !errors.Is(err, ErrHomeworkInvalidID) {
				t.Errorf("%s(%q) = %v, want ErrHomeworkInvalidID", c.name, id, err)
			}
			if calls := e.calls.list(); len(calls) != 0 {
				t.Errorf("%s(%q) reached the use cases: %v", c.name, id, calls)
			}
		}
	}
}

// A failure the page cannot fix shows one fixed text; the cause is logged, never shown.
func TestHomeworkHidesCauses(t *testing.T) {
	cause := errors.New(`open C:\Users\student\db: locked`)
	for _, c := range everyHomeworkCall {
		t.Run(c.name, func(t *testing.T) {
			e := newHWEnv()
			e.proposals.err, e.drafts.err, e.deliveries.err = cause, cause, cause
			want := ErrHomeworkUnavailable
			if c.name == "ConfirmSend" {
				want = ErrSendIncomplete // a send may have been saved: the text says repeating is safe
			}
			err := c.call(e.svc, "x1")
			if !errors.Is(err, want) || strings.Contains(err.Error(), "student") {
				t.Fatalf("err = %v, want %v", err, want)
			}
			if !strings.Contains(e.logs.String(), "locked") {
				t.Fatalf("the cause is not logged: %q", e.logs.String())
			}
		})
	}
}

// AC-14: only ConfirmSend reaches the use case's ConfirmSend, and only SendAgain resends.
func TestOnlyConfirmSendExecutes(t *testing.T) {
	for _, c := range everyHomeworkCall {
		e := newHWEnv()
		e.drafts.generation = homeworkflow.GenerationView{JobID: "j1", Status: job.StatusRunning}
		if err := c.call(e.svc, "x1"); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if c.name == "ConfirmSend" {
			if got := e.calls.list(); !slices.Equal(got, []string{"ConfirmSend"}) {
				t.Fatalf("ConfirmSend calls = %v, want only the use case's ConfirmSend", got)
			}
			continue
		}
		if e.called("ConfirmSend") {
			t.Errorf("%s called ConfirmSend: %v", c.name, e.calls.list())
		}
		if c.name != "SendAgain" && e.called("SendAgain") {
			t.Errorf("%s called SendAgain", c.name)
		}
	}
}

func TestConfirmSendErrors(t *testing.T) {
	tests := []struct {
		name string
		edit func(e *hwEnv)
		want error
		read bool // reads the proposal after ConfirmSend
	}{
		{name: "rejected proposal", edit: func(e *hwEnv) {
			e.proposals.confirmErr = fmt.Errorf("confirm: %w", proposal.ErrInvalidTransition)
		}, want: ErrSendCannotConfirm},
		{name: "not a send proposal", edit: func(e *hwEnv) {
			e.proposals.confirmErr = fmt.Errorf("confirm: %w: export_homework", homeworkflow.ErrNotASend)
		}, want: ErrSendNotFound},
		{name: "unknown proposal", edit: func(e *hwEnv) { e.proposals.err = fmt.Errorf("get: %w", shared.ErrNotFound) },
			want: ErrSendNotFound},
		{name: "stale after the confirmation", edit: func(e *hwEnv) {
			e.proposals.confirmErr = fmt.Errorf("execute: %w: homework_artifact_changed", proposal.ErrStale)
		}, want: ErrSendStale, read: true}, // reads the proposal for its reason (TASK-231)
		{name: "unresolved earlier delivery", edit: func(e *hwEnv) {
			e.proposals.confirmErr = fmt.Errorf("send: %w", senddelivery.ErrUnresolvedDelivery)
		}, want: ErrSendUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHWEnv()
			tt.edit(e)
			if err := e.svc.ConfirmSend(context.Background(), "p1", "ДЗ", "txt", "", ""); !errors.Is(err, tt.want) {
				t.Fatalf("ConfirmSend = %v, want %v", err, tt.want)
			}
			want := []string{"ConfirmSend"}
			if tt.read {
				want = append(want, "Get")
			}
			if got := e.calls.list(); !slices.Equal(got, want) {
				t.Fatalf("calls = %v, want %v", got, want)
			}
		})
	}
}

// Hard rule 7: when the execution fails (for example after the delivery was saved), the page may repeat ConfirmSend
// with the same proposal; the use case executes it again under the same key.
func TestConfirmSendCanBeRepeatedAfterAFailure(t *testing.T) {
	e := newHWEnv()
	e.proposals.confirmErr = errors.New("execute: mark executed: database is locked")
	err := e.svc.ConfirmSend(context.Background(), "p1", "ДЗ", "txt", "", "")
	if !errors.Is(err, ErrSendIncomplete) || strings.Contains(err.Error(), "locked") {
		t.Fatalf("failed ConfirmSend = %v, want ErrSendIncomplete", err)
	}
	if !strings.Contains(ErrSendIncomplete.Error(), "второе письмо не уйдёт") {
		t.Fatalf("the text must say that repeating is safe: %q", ErrSendIncomplete)
	}

	e.proposals.confirmErr = nil
	if err := e.svc.ConfirmSend(context.Background(), "p1", "ДЗ", "txt", "", ""); err != nil {
		t.Fatalf("repeated ConfirmSend = %v", err)
	}
	if got := e.calls.list(); !slices.Equal(got, []string{"ConfirmSend", "ConfirmSend"}) {
		t.Fatalf("calls = %v, want the same use case call twice", got)
	}
	if e.proposals.shownSubject != "ДЗ" {
		t.Fatalf("shown subject = %q, want the dialog's", e.proposals.shownSubject)
	}
}

func TestCancelSend(t *testing.T) {
	e := newHWEnv()
	if err := e.svc.CancelSend(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	if got := e.calls.list(); !slices.Equal(got, []string{"Get", "Decline"}) {
		t.Fatalf("calls = %v, want Get, Decline", got)
	}

	e = newHWEnv()
	e.proposals.declineErr = fmt.Errorf("decline: %w", proposal.ErrInvalidTransition)
	if err := e.svc.CancelSend(context.Background(), "p1"); !errors.Is(err, ErrSendAlreadyDecided) {
		t.Fatalf("CancelSend of a decided proposal = %v", err)
	}
	e = newHWEnv()
	e.proposals.proposal.Kind = proposal.KindDeadlineEvent
	if err := e.svc.CancelSend(context.Background(), "p1"); !errors.Is(err, ErrSendNotFound) || e.called("Decline") {
		t.Fatalf("CancelSend of another kind = %v, calls %v", err, e.calls.list())
	}
}

func TestPrepareSend(t *testing.T) {
	e := newHWEnv()
	got, err := e.svc.PrepareSend(context.Background(), "a1", "txt")
	want := SendPreparation{
		ProposalID: "p1", Status: "ready", Recipient: "teacher@school.example", Subject: "Домашнее задание",
		AttachmentName: "homework.md", AttachmentSize: 42, AttachmentFormat: "txt",
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("PrepareSend = %+v, %v\nwant %+v", got, err, want)
	}
	if calls := e.calls.list(); !slices.Equal(calls, []string{"ProposeSend"}) {
		t.Fatalf("calls = %v: preparing must not confirm", calls)
	}

	tests := []struct {
		reason proposal.RejectReason
		text   string
	}{
		{proposal.ReasonNoRecipient, "«Подключения»"},
		{proposal.ReasonNoArtifact, "сохраните решение в файл"},
		{proposal.ReasonUnknownAssignment, "Задание не найдено"},
		{proposal.ReasonDueOutOfRange, msgRejectedDefault},
	}
	for _, tt := range tests {
		e := newHWEnv()
		e.proposals.proposal = homeworkflow.ProposalView{
			ID: "p2", Kind: proposal.KindSendHomework, Status: proposal.StatusRejected, Reason: tt.reason,
		}
		got, err := e.svc.PrepareSend(context.Background(), "a1", "txt")
		if err != nil || got.Status != "rejected" || got.Reason != string(tt.reason) || !strings.Contains(got.Message, tt.text) ||
			got.Recipient != "" || got.Subject != "" {
			t.Errorf("%s: PrepareSend = %+v, %v", tt.reason, got, err)
		}
	}
}

func TestDeliveries(t *testing.T) {
	e := newHWEnv()
	e.deliveries.views = []senddelivery.View{
		{ID: "d2", Status: delivery.StatusUnknown, Recipient: "teacher@school.example", Subject: "Домашнее задание",
			Attempts: 1, LastError: "smtp: no reply after the message", ConfirmedAt: hwT0, CreatedAt: hwT0,
			OutcomeUnknown: true, CanMarkSent: true, CanSendAgain: true},
		{ID: "d1", Status: delivery.StatusSent, Attempts: 1, SentAt: hwT0, CreatedAt: hwT0},
	}
	got, err := e.svc.Deliveries(context.Background(), "a1")
	if err != nil || len(got) != 2 {
		t.Fatalf("Deliveries = %+v, %v", got, err)
	}
	if u := got[0]; u.Status != "UNKNOWN" || !u.CanMarkSent || !u.CanSendAgain || u.Notice != msgUnknownOutcome ||
		u.LastError == "" || u.ConfirmedAt != "2026-10-16T09:30:00Z" || u.SentAt != "" {
		t.Errorf("unknown delivery = %+v", u)
	}
	if s := got[1]; s.Status != "SENT" || s.CanMarkSent || s.CanSendAgain || s.Notice != "" || s.SentAt == "" {
		t.Errorf("sent delivery = %+v", s)
	}
	if !strings.Contains(msgUnknownOutcome, "проверьте сами или отправьте ещё раз под свою ответственность") {
		t.Error("the owner's text of an unknown outcome changed")
	}
}

func TestResolveDelivery(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"ok", nil, nil},
		{"not unknown any more", fmt.Errorf("mark: %w", delivery.ErrInvalidTransition), ErrDeliveryActionInvalid},
		{"missing", fmt.Errorf("get: %w", shared.ErrNotFound), ErrDeliveryNotFound},
	}
	for _, tt := range tests {
		for _, c := range everyHomeworkCall {
			if c.name != "MarkSent" && c.name != "SendAgain" {
				continue
			}
			e := newHWEnv()
			e.deliveries.err = tt.err
			if err := c.call(e.svc, "d1"); !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Errorf("%s %s = %v, want %v", c.name, tt.name, err, tt.want)
			}
			if calls := e.calls.list(); !slices.Equal(calls, []string{c.name}) {
				t.Errorf("%s calls = %v", c.name, calls)
			}
		}
	}
}

func TestGenerationStatus(t *testing.T) {
	tests := []struct {
		status  job.Status
		failure error
		state   string
		text    string
	}{
		{job.StatusQueued, nil, "generating", ""},
		{job.StatusRunning, nil, "generating", ""},
		{job.StatusRetryWait, nil, "generating", ""},
		{job.StatusWaitingForNetwork, nil, "waitingNetwork", msgWaitingNetwork},
		{job.StatusWaitingForQuota, nil, "waitingQuota", msgWaitingQuota},
		{job.StatusCompleted, nil, "done", ""},
		{job.StatusCancelled, nil, "failed", msgGenerationCancel},
		{job.StatusFailed, homeworkflow.ErrDraftChanged, "failed", "Черновик изменился во время генерации. Сгенерируйте ещё раз."},
		{job.StatusFailed, homeworkflow.ErrDraftInvalidAnswer, "failed", "Сервис конспектов ответил в неверном виде. Попробуйте ещё раз."},
		{job.StatusFailed, homeworkflow.ErrDraftLLM, "failed", msgGenerationFailed},
		{job.StatusFailed, nil, "failed", msgGenerationFailed},
	}
	for _, tt := range tests {
		e := newHWEnv()
		e.drafts.generation = homeworkflow.GenerationView{JobID: "j1", Status: tt.status, Failure: tt.failure}
		got, err := e.svc.GenerationStatus(context.Background(), "j1")
		if err != nil || got.State != tt.state || got.Message != tt.text {
			t.Errorf("%s/%v: GenerationStatus = %+v, %v, want %s %q", tt.status, tt.failure, got, err, tt.state, tt.text)
		}
	}

	e := newHWEnv()
	e.drafts.err = fmt.Errorf("not a draft job: %w", shared.ErrNotFound)
	if _, err := e.svc.GenerationStatus(context.Background(), "j1"); !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("missing job = %v", err)
	}
}

func TestGenerateAndEditErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"stale base", fmt.Errorf("g: %w", homeworkdraft.ErrStaleBase), ErrDraftStale},
		{"full", fmt.Errorf("g: %w", homeworkdraft.ErrFull), ErrDraftFull},
		{"missing assignment", fmt.Errorf("g: %w", shared.ErrNotFound), ErrHomeworkNotFound},
	}
	for _, tt := range tests {
		for _, name := range []string{"Generate", "EditDraft"} {
			e := newHWEnv()
			e.drafts.err = tt.err
			call := everyHomeworkCall[slices.IndexFunc(everyHomeworkCall, func(c hwCall) bool { return c.name == name })]
			if err := call.call(e.svc, "a1"); !errors.Is(err, tt.want) {
				t.Errorf("%s %s = %v, want %v", name, tt.name, err, tt.want)
			}
		}
	}

	e := newHWEnv()
	e.drafts.err = fmt.Errorf("g: %w", values.ErrInvalidDraftRequest)
	if _, err := e.svc.Generate(context.Background(), "a1", 0, "\x00"); !errors.Is(err, ErrInstructionInvalid) {
		t.Errorf("bad instruction = %v", err)
	}
	e.drafts.err = fmt.Errorf("g: %w", homeworkflow.ErrGenerateRetries)
	if _, err := e.svc.Generate(context.Background(), "a1", 0, ""); !errors.Is(err, ErrGenerateRetries) {
		t.Errorf("too many retries = %v", err)
	}
	e.drafts.err = fmt.Errorf("e: %w", homeworkdraft.ErrInvalidContent)
	if _, err := e.svc.EditDraft(context.Background(), "a1", 0, "", ""); !errors.Is(err, ErrDraftInvalid) {
		t.Errorf("bad text = %v", err)
	}

	e = newHWEnv()
	if got, err := e.svc.Generate(context.Background(), "a1", 3, "x"); err != nil || got.JobID != "j1" || e.drafts.gotBase != 3 {
		t.Errorf("Generate = %+v, %v, base %d", got, err, e.drafts.gotBase)
	}
	got, err := e.svc.EditDraft(context.Background(), "a1", 2, "Мой заголовок", "<b>текст</b>")
	if err != nil || got.Number != 3 || got.Body != "<b>текст</b>" || e.drafts.gotEdit.Base != 2 {
		t.Errorf("EditDraft = %+v, %v: text crosses as it is", got, err)
	}
}

func TestOpenAssignment(t *testing.T) {
	e := newHWEnv()
	got, err := e.svc.OpenAssignment(context.Background(), "a1")
	if err != nil || got.Latest != nil || got.Versions == nil || len(got.Versions) != 0 || got.Assignment.ID != "a1" {
		t.Fatalf("without a draft: %+v, %v", got, err)
	}

	e.drafts.versions = []homeworkflow.DraftVersionView{
		{Number: 1, Origin: homeworkdraft.OriginGenerated, Title: "T1", Body: "B1", Instruction: "", CreatedAt: hwT0},
		{Number: 2, Origin: homeworkdraft.OriginEdited, Title: "T2", Body: "B2", Latest: true, CreatedAt: hwT0},
	}
	got, err = e.svc.OpenAssignment(context.Background(), "a1")
	if err != nil || len(got.Versions) != 2 || got.Latest == nil || got.Latest.Number != 2 || got.Latest.Body != "B2" ||
		got.Versions[0].Source != "generated" || got.Versions[1].Source != "edited" || !got.Versions[1].Latest {
		t.Fatalf("OpenAssignment = %+v, %v", got, err)
	}

	e.proposals.err = fmt.Errorf("a: %w", shared.ErrNotFound)
	if _, err := e.svc.OpenAssignment(context.Background(), "a1"); !errors.Is(err, ErrHomeworkNotFound) {
		t.Fatalf("missing assignment = %v", err)
	}
}

func TestExportDraft(t *testing.T) {
	e := newHWEnv()
	got, err := e.svc.ExportDraft(context.Background(), "a1", 2)
	if err != nil || got != (ExportResult{Version: 2, FileName: "homework.md"}) {
		t.Fatalf("ExportDraft = %+v, %v", got, err)
	}
	for _, n := range []int{0, -1} {
		if _, err := e.svc.ExportDraft(context.Background(), "a1", n); !errors.Is(err, ErrDraftVersionNotFound) {
			t.Errorf("ExportDraft(%d) = %v", n, err)
		}
	}
	e.drafts.err = fmt.Errorf("x: %w", shared.ErrNotFound)
	if _, err := e.svc.ExportDraft(context.Background(), "a1", 9); !errors.Is(err, ErrDraftVersionNotFound) {
		t.Errorf("missing version = %v", err)
	}
	e.drafts.exportErr = fmt.Errorf("export: %w", homeworkflow.ErrExportLocked)
	if _, err := e.svc.ExportDraft(context.Background(), "a1", 2); !errors.Is(err, ErrExportLocked) {
		t.Errorf("export during an unresolved email = %v, want ErrExportLocked", err)
	}
}

// The page reads these names (frontend/src/bindings/homework.ts).
func TestHomeworkJSONNames(t *testing.T) {
	tests := []struct {
		v    any
		want string
	}{
		{AssignmentItem{}, `{"id":"","lessonId":"","title":"","description":"","due":"","userEdited":false,"version":0,"updatedAt":"","subject":"","subjectSource":"","subjectFromLesson":false,"kind":"","teacher":"","teacherSource":"","lessonDate":"","correctedSubject":"","correctedTeacher":"","done":false,"doneAt":""}`},
		{VersionItem{}, `{"number":0,"source":"","instruction":"","latest":false,"createdAt":""}`},
		{VersionText{}, `{"number":0,"source":"","title":"","body":"","instruction":"","latest":false,"createdAt":""}`},
		{AssignmentDetail{Versions: []VersionItem{}}, `{"assignment":{"id":"","lessonId":"","title":"","description":"","due":"","userEdited":false,"version":0,"updatedAt":"","subject":"","subjectSource":"","subjectFromLesson":false,"kind":"","teacher":"","teacherSource":"","lessonDate":"","correctedSubject":"","correctedTeacher":"","done":false,"doneAt":""},"versions":[],"latest":null,"generation":null}`},
		{GenerateStarted{}, `{"jobId":"","base":0}`},
		{GenerationStatus{}, `{"state":"","message":"","percent":0}`},
		{ActiveGeneration{}, `{"jobId":"","state":"","message":"","percent":0}`},
		{DraftStatus{}, `{"latestVersion":0,"generation":null}`},
		{ExportResult{}, `{"version":0,"fileName":""}`},
		{DocxSaveResult{}, `{"status":"","message":"","name":""}`},
		{SendPreparation{}, `{"proposalId":"","status":"","reason":"","message":"","recipient":"","recipientName":"","role":"","subjectId":"","options":null,"subject":"","attachmentName":"","attachmentSize":0,"attachmentFormat":""}`},
		{DoneResult{}, `{"problem":"","outcomes":null,"prior":null,"priorDone":false}`},
		{DoneOutcomeItem{}, `{"id":"","code":"","done":false,"version":0}`},
		{DonePriorItem{}, `{"id":"","version":0}`},
		{DeliveryItem{}, `{"id":"","status":"","recipient":"","subject":"","attempts":0,"lastError":"","confirmedAt":"","sentAt":"","createdAt":"","canMarkSent":false,"canSendAgain":false,"notice":""}`},
	}
	for _, tt := range tests {
		raw, err := json.Marshal(tt.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tt.want {
			t.Errorf("%T JSON = %s\nwant %s", tt.v, raw, tt.want)
		}
	}
}

// The page shows the error texts as they are; the frontend wrapper lists each one.
func TestFrontendKnowsHomeworkErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/homework.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{
		ErrHomeworkUnavailable, ErrHomeworkInvalidID, ErrHomeworkNotFound, ErrDraftVersionNotFound, ErrDraftStale,
		ErrDraftInvalid, ErrDraftFull, ErrInstructionInvalid, ErrGenerateRetries, ErrGenerationNotFound, ErrSendNotFound,
		ErrSendCannotConfirm, ErrSendAlreadyDecided, ErrSendStale, ErrSendUnresolved, ErrSendIncomplete, ErrExportLocked,
		ErrDeliveryNotFound, ErrDeliveryActionInvalid, ErrGenerationRunning, ErrSendFormat, // +2: TASK-094
		ErrSendAssignmentDeleted, // TASK-231
		ErrSendChooseTeacher,     // TASK-237
		ErrHomeworkListState,     // TASK-315
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/homework.ts does not list the error %q", e)
		}
	}
	// The agent card lists the texts of its own binding too (TASK-237).
	agent, err := os.ReadFile("../../../frontend/src/bindings/agentConfirm.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agent), `"`+ErrSendChooseTeacher.Error()+`"`) {
		t.Errorf("frontend/src/bindings/agentConfirm.ts does not list the error %q", ErrSendChooseTeacher)
	}
}
