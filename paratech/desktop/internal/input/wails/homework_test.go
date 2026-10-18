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
	log        *hwCalls
	err        error
	confirmErr error // returned by ConfirmSend
	declineErr error
	proposal   homeworkflow.ProposalView // returned by ProposeSend and Get
	list       []homeworkflow.AssignmentView
}

func (f *fakeHWProposals) ListAssignments(context.Context) ([]homeworkflow.AssignmentView, error) {
	f.log.add("ListAssignments")
	return f.list, f.err
}

func (f *fakeHWProposals) Assignment(_ context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	f.log.add("Assignment")
	return homeworkflow.AssignmentView{ID: id, Title: "Rivers", UpdatedAt: hwT0}, f.err
}

func (f *fakeHWProposals) ProposeSend(context.Context, shared.AssignmentID) (homeworkflow.ProposalView, error) {
	f.log.add("ProposeSend")
	return f.proposal, f.err
}

func (f *fakeHWProposals) Get(context.Context, proposal.ID) (homeworkflow.ProposalView, error) {
	f.log.add("Get")
	return f.proposal, f.err
}

func (f *fakeHWProposals) ConfirmSend(context.Context, proposal.ID) (homeworkflow.ProposalView, error) {
	f.log.add("ConfirmSend")
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
	exportErr  error // returned by ExportDraft
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
	}
	e.svc = NewHomeworkService(e.core(), slog.New(slog.NewTextHandler(e.logs, nil)))
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
		_, err := s.ListAssignments(context.Background())
		return err
	}},
	{"OpenAssignment", func(s *HomeworkService, id string) error {
		_, err := s.OpenAssignment(context.Background(), id)
		return err
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
	{"PrepareSend", func(s *HomeworkService, id string) error {
		_, err := s.PrepareSend(context.Background(), id)
		return err
	}},
	{"ConfirmSend", func(s *HomeworkService, id string) error { return s.ConfirmSend(context.Background(), id) }},
	{"CancelSend", func(s *HomeworkService, id string) error { return s.CancelSend(context.Background(), id) }},
	{"Deliveries", func(s *HomeworkService, id string) error {
		_, err := s.Deliveries(context.Background(), id)
		return err
	}},
	{"MarkSent", func(s *HomeworkService, id string) error { _, err := s.MarkSent(context.Background(), id); return err }},
	{"SendAgain", func(s *HomeworkService, id string) error { _, err := s.SendAgain(context.Background(), id); return err }},
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
		}, want: ErrSendStale},
		{name: "unresolved earlier delivery", edit: func(e *hwEnv) {
			e.proposals.confirmErr = fmt.Errorf("send: %w", senddelivery.ErrUnresolvedDelivery)
		}, want: ErrSendUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHWEnv()
			tt.edit(e)
			if err := e.svc.ConfirmSend(context.Background(), "p1"); !errors.Is(err, tt.want) {
				t.Fatalf("ConfirmSend = %v, want %v", err, tt.want)
			}
			if got := e.calls.list(); !slices.Equal(got, []string{"ConfirmSend"}) {
				t.Fatalf("calls = %v, want only ConfirmSend", got)
			}
		})
	}
}

// Hard rule 7: when the execution fails (for example after the delivery was saved), the page may repeat ConfirmSend
// with the same proposal; the use case executes it again under the same key.
func TestConfirmSendCanBeRepeatedAfterAFailure(t *testing.T) {
	e := newHWEnv()
	e.proposals.confirmErr = errors.New("execute: mark executed: database is locked")
	err := e.svc.ConfirmSend(context.Background(), "p1")
	if !errors.Is(err, ErrSendIncomplete) || strings.Contains(err.Error(), "locked") {
		t.Fatalf("failed ConfirmSend = %v, want ErrSendIncomplete", err)
	}
	if !strings.Contains(ErrSendIncomplete.Error(), "второе письмо не уйдёт") {
		t.Fatalf("the text must say that repeating is safe: %q", ErrSendIncomplete)
	}

	e.proposals.confirmErr = nil
	if err := e.svc.ConfirmSend(context.Background(), "p1"); err != nil {
		t.Fatalf("repeated ConfirmSend = %v", err)
	}
	if got := e.calls.list(); !slices.Equal(got, []string{"ConfirmSend", "ConfirmSend"}) {
		t.Fatalf("calls = %v, want the same use case call twice", got)
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
	got, err := e.svc.PrepareSend(context.Background(), "a1")
	want := SendPreparation{
		ProposalID: "p1", Status: "ready", Recipient: "teacher@school.example", Subject: "Домашнее задание",
		AttachmentName: "homework.md", AttachmentSize: 42,
	}
	if err != nil || got != want {
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
		{proposal.ReasonNoArtifact, "сохраните черновик в файл"},
		{proposal.ReasonUnknownAssignment, "Задание не найдено"},
		{proposal.ReasonDueOutOfRange, msgRejectedDefault},
	}
	for _, tt := range tests {
		e := newHWEnv()
		e.proposals.proposal = homeworkflow.ProposalView{
			ID: "p2", Kind: proposal.KindSendHomework, Status: proposal.StatusRejected, Reason: tt.reason,
		}
		got, err := e.svc.PrepareSend(context.Background(), "a1")
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
		{AssignmentItem{}, `{"id":"","lessonId":"","title":"","description":"","due":"","userEdited":false,"updatedAt":""}`},
		{VersionItem{}, `{"number":0,"source":"","instruction":"","latest":false,"createdAt":""}`},
		{VersionText{}, `{"number":0,"source":"","title":"","body":"","instruction":"","latest":false,"createdAt":""}`},
		{AssignmentDetail{Versions: []VersionItem{}}, `{"assignment":{"id":"","lessonId":"","title":"","description":"","due":"","userEdited":false,"updatedAt":""},"versions":[],"latest":null}`},
		{GenerateStarted{}, `{"jobId":"","base":0}`},
		{GenerationStatus{}, `{"state":"","message":""}`},
		{ExportResult{}, `{"version":0,"fileName":""}`},
		{SendPreparation{}, `{"proposalId":"","status":"","reason":"","message":"","recipient":"","subject":"","attachmentName":"","attachmentSize":0}`},
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
		ErrDeliveryNotFound, ErrDeliveryActionInvalid,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/homework.ts does not list the error %q", e)
		}
	}
}
