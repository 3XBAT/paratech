package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/services/schedulesubjects"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// scheduleLibrary is the library use case with subjects from the schedule, as library.Service is.
type scheduleLibrary struct {
	*fakeLibrary
	*fakeScheduleSubjects
}

func newScheduleConfirm(uc *fakeScheduleSubjects, lib *fakeLibrary, log *slog.Logger) *AgentConfirmService {
	return newScheduleCards(NewLibraryService(scheduleLibrary{lib, uc}, nil, nil, log), log)
}

// newScheduleCards is the confirm cards bound to page, the library binding of the page.
func newScheduleCards(page *LibraryService, log *slog.Logger) *AgentConfirmService {
	return BindScheduleCards(NewAgentConfirmService(AgentConfirmCore{
		Proposals: newConfirmProposals(), Materials: fakeMaterials{}, Deletions: &fakeDeletion{},
	}, log), page)
}

func agentSchedule(tok string) library.ScheduleProposal {
	return library.ScheduleProposal{Token: tok, Omitted: 1, Groups: []schedulesubjects.Group{
		{Key: "матанализ", Name: "Матанализ", Doubtful: true, Members: []schedulesubjects.Member{
			{Key: "матанализ", Name: "Матанализ", Spellings: []string{"Матанализ"}},
			{Key: "матан", Name: "Матан", Spellings: []string{"Матан"}, Doubtful: true, Link: schedulesubjects.LinkAbbreviation},
		}},
		{Key: "физика", Name: "Физика", Subject: "s-phys", Aliases: []string{"физика, сем."}},
	}}
}

func TestScheduleCardWaitingShowsTheGroups(t *testing.T) {
	s := newScheduleConfirm(&fakeScheduleSubjects{proposal: agentSchedule(tokA)}, &fakeLibrary{}, discard())
	got, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "schedule_subjects" || got.TargetKind != "schedule_subjects" || got.TargetID != tokA ||
		got.State != agentStateWaiting || got.Schedule == nil || got.Schedule.Omitted != 1 || len(got.Schedule.Groups) != 2 {
		t.Fatalf("card = %+v", got)
	}
	g := got.Schedule.Groups
	if g[0].Action != ScheduleActionCreate || !g[0].Members[1].Doubtful || g[1].Action != ScheduleActionAliasOf || g[1].SubjectID != "s-phys" {
		t.Fatalf("groups = %+v", g)
	}
	raw, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(raw), `"schedule":{"groups":[{"key":"матанализ"`) {
		t.Fatalf("json %s, %v", raw, err)
	}
}

// TestScheduleCardStale: a card whose proposal changed (or one read after a restart) is stale, and «Да» on a stale
// token runs nothing: the student asks the agent again.
func TestScheduleCardStale(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokB), err: library.ErrStale}
	s := newScheduleConfirm(uc, &fakeLibrary{}, discard())

	got, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA)
	if err != nil || got.State != agentStateStale || got.Message != msgScheduleStale || len(got.Schedule.Groups) != 0 {
		t.Fatalf("card = %+v, %v", got, err)
	}
	got, err = s.ConfirmSchedule(t.Context(), tokA, nil)
	if err != nil || got.State != agentStateStale || got.Message != msgScheduleStale || got.Schedule.Result != nil {
		t.Fatalf("confirm = %+v, %v", got, err)
	}
	if !strings.Contains(got.Message, "попросите агента") || !strings.Contains(got.Message, "«Предметы»") {
		t.Errorf("the stale card offers no way on: %q", got.Message)
	}
	// Stale also shows for a card applied before a restart or from the page: it never claims nothing was created.
	if strings.Contains(got.Message, "Ничего не создано") {
		t.Errorf("the stale card claims nothing was created: %q", got.Message)
	}
	if _, err := s.UndoSchedule(t.Context(), tokA); !errors.Is(err, ErrAgentConfirmScheduleUndoGone) {
		t.Fatalf("undo of nothing: %v", err)
	}
}

// TestScheduleConfirmPassesTheChoices: «Да» sends the student's choices as the page does; a doubtful member without a
// choice stays merged (О-12 б: the use case's default), a renamed one is taken separately.
func TestScheduleConfirmPassesTheChoices(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA), res: library.ScheduleResult{
		Created: []library.ScheduleCreated{{ID: "sched-1", Key: "матанализ", Name: "Матанализ"}},
	}}
	s := newScheduleConfirm(uc, &fakeLibrary{}, discard())

	if _, err := s.ConfirmSchedule(t.Context(), tokA, nil); err != nil {
		t.Fatal(err)
	}
	if uc.gotToken != tokA || len(uc.gotChoices) != 0 {
		t.Fatalf("defaults: token %q, choices %+v", uc.gotToken, uc.gotChoices)
	}

	choices := []ScheduleChoice{
		{Group: "матанализ", Action: ScheduleActionCreate, Members: []ScheduleMemberChoice{{Key: "матан", Action: ScheduleActionSeparate, Name: "Матан 2"}}},
		{Group: "физика", Action: ScheduleActionSkip},
	}
	if _, err := s.ConfirmSchedule(t.Context(), tokA, choices); err != nil {
		t.Fatal(err)
	}
	want := []schedulesubjects.Choice{
		{Group: "матанализ", Action: schedulesubjects.GroupCreate, Members: []schedulesubjects.MemberChoice{
			{Member: "матан", Action: schedulesubjects.MemberSeparately, Name: "Матан 2"},
		}},
		{Group: "физика", Action: schedulesubjects.GroupSkip},
	}
	if !reflect.DeepEqual(uc.gotChoices, want) {
		t.Fatalf("choices = %+v\nwant %+v", uc.gotChoices, want)
	}
}

// TestScheduleConfirmDoneThenReadAgain: the outcome stays with the card: read again (the proposal has changed by then,
// since the subjects exist) it is still done with the same result.
func TestScheduleConfirmDoneThenReadAgain(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA), res: library.ScheduleResult{
		Created: []library.ScheduleCreated{{ID: "sched-1", Key: "матанализ", Name: "Матанализ"}},
		Aliased: []library.ScheduleAliased{{Subject: "s-phys", Aliases: []string{"физика, сем."}}},
	}}
	s := newScheduleConfirm(uc, &fakeLibrary{}, discard())

	got, err := s.ConfirmSchedule(t.Context(), tokA, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := &ScheduleCreateResult{
		Created: []ScheduleCreatedSubject{{ID: "sched-1", Key: "матанализ", Name: "Матанализ"}},
		Aliased: []ScheduleAliases{{SubjectID: "s-phys", Aliases: []string{"физика, сем."}}},
		Skipped: []ScheduleSkippedRow{},
	}
	if got.State != agentStateDone || !reflect.DeepEqual(got.Schedule.Result, want) {
		t.Fatalf("confirm = %+v (%+v)", got, got.Schedule.Result)
	}

	uc.mu.Lock()
	uc.proposal = agentSchedule(tokB)
	uc.mu.Unlock()
	again, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA)
	if err != nil || again.State != agentStateDone || !reflect.DeepEqual(again.Schedule.Result, want) {
		t.Fatalf("read again = %+v, %v", again, err)
	}
}

// TestScheduleConfirmPartialThenUndoAll: a failure midway leaves the card confirmed with what was done; «Отменить
// всё» takes exactly that back (the use case gets the stored result, not one from the page), the card reads undone
// afterwards, and a repeated «Да» clears the mark.
func TestScheduleConfirmPartialThenUndoAll(t *testing.T) {
	uc := &fakeScheduleSubjects{
		proposal: agentSchedule(tokA), res: partialResult(), err: fmt.Errorf("%w: disk", library.ErrUnavailable),
		undo: library.ScheduleUndo{
			Removed: []shared.SubjectID{"sched-1"}, Unaliased: []library.ScheduleAliased{{Subject: "s-phys", Aliases: []string{"физика, сем."}}},
		},
	}
	s := newScheduleConfirm(uc, &fakeLibrary{}, discard())

	got, err := s.ConfirmSchedule(t.Context(), tokA, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != agentStateConfirmed || got.Message != msgSchedulePartial || got.Schedule.Result == nil ||
		len(got.Schedule.Result.Created) != 1 || got.Schedule.Result.Problem != ScheduleProblemUnavailable {
		t.Fatalf("partial = %+v (%+v)", got, got.Schedule.Result)
	}

	read, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA)
	if err != nil || read.State != agentStateConfirmed || read.Message != msgSchedulePartial || len(read.Schedule.Result.Created) != 1 {
		t.Fatalf("partial read again = %+v, %v", read, err)
	}

	undone, err := s.UndoSchedule(t.Context(), tokA)
	if err != nil {
		t.Fatal(err)
	}
	if len(uc.undos) != 1 || uc.undos[0].token != tokA || !reflect.DeepEqual(uc.undos[0].res, partialResult()) {
		t.Fatalf("the use case undid %+v", uc.undos)
	}
	if undone.State != agentStateUndone || undone.Schedule.Undo == nil || !reflect.DeepEqual(undone.Schedule.Undo.Removed, []string{"sched-1"}) {
		t.Fatalf("undone = %+v", undone)
	}
	if read, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA); err != nil || read.State != agentStateUndone || read.Schedule.Undo == nil {
		t.Fatalf("undone read again = %+v, %v", read, err)
	}

	uc.set(partialResult(), nil)
	if again, err := s.ConfirmSchedule(t.Context(), tokA, nil); err != nil || again.State != agentStateDone {
		t.Fatalf("confirm after undo = %+v, %v", again, err)
	}
	if read, _ := s.Card(t.Context(), agentCardScheduleSubjects, tokA); read.State != agentStateDone {
		t.Fatalf("after a new «Да» the card reads %s", read.State)
	}
}

// TestScheduleConfirmRemovedNameOffersRestore: a row skipped because only a removed subject holds the name (also a
// renamed one) carries that subject and its version, so the card shows «Вернуть» (LibraryService.RestoreSubject), on
// the answer of «Да» and when the card is read again.
func TestScheduleConfirmRemovedNameOffersRestore(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA), res: library.ScheduleResult{
		Created: []library.ScheduleCreated{{ID: "sched-1", Key: "матанализ", Name: "Матанализ"}},
		Skipped: []library.ScheduleSkipped{{Key: "матан", Removed: "s1", Reason: library.ReasonNameTaken}},
	}}
	lib := &fakeLibrary{removed: map[shared.SubjectID]*subject.Subject{"s1": removedSubject(t, 8)}}
	s := newScheduleConfirm(uc, lib, discard())

	rename := []ScheduleChoice{{Group: "матанализ", Action: ScheduleActionCreate, Members: []ScheduleMemberChoice{
		{Key: "матан", Action: ScheduleActionSeparate, Name: "Физика"},
	}}}
	want := ScheduleSkippedRow{Key: "матан", Reason: ScheduleReasonNameTaken, RemovedSubjectID: "s1", RemovedSubjectVersion: 8}
	got, err := s.ConfirmSchedule(t.Context(), tokA, rename)
	if err != nil || got.State != agentStateDone || !reflect.DeepEqual(got.Schedule.Result.Skipped, []ScheduleSkippedRow{want}) {
		t.Fatalf("confirm = %+v, %v", got.Schedule.Result, err)
	}
	read, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA)
	if err != nil || !reflect.DeepEqual(read.Schedule.Result.Skipped, []ScheduleSkippedRow{want}) {
		t.Fatalf("read again = %+v, %v", read.Schedule.Result, err)
	}
}

func TestScheduleConfirmRefusals(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		choices []ScheduleChoice
		err     error
		want    error
	}{
		{"a token of another shape", "p-send", nil, nil, ErrAgentConfirmInvalid},
		{"an upper-case token", strings.ToUpper(tokA), nil, nil, ErrAgentConfirmInvalid},
		{"an unknown action", tokA, []ScheduleChoice{{Group: "физика", Action: "delete"}}, nil, ErrAgentConfirmInvalid},
		{"a name of another subject", tokA, nil, fmt.Errorf("%w: x", library.ErrNameTaken), ErrAgentConfirmScheduleName},
		{"a bad choice", tokA, nil, schedulesubjects.ErrUnknownGroup, ErrAgentConfirmInvalid},
		{"the library fails", tokA, nil, library.ErrUnavailable, ErrAgentConfirmUnavailable},
		{"busy", tokA, nil, library.ErrBusy, ErrAgentConfirmScheduleBusy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA), err: tt.err}
			s := newScheduleConfirm(uc, &fakeLibrary{}, discard())
			if _, err := s.ConfirmSchedule(t.Context(), tt.token, tt.choices); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}

	// Without subjects from the schedule wired the kind is not one of this desktop.
	plain := newConfirmService(newConfirmProposals())
	if _, err := plain.Card(t.Context(), agentCardScheduleSubjects, tokA); !errors.Is(err, ErrAgentConfirmInvalid) {
		t.Errorf("card without the schedule: %v", err)
	}
	if _, err := plain.ConfirmSchedule(t.Context(), tokA, nil); !errors.Is(err, ErrAgentConfirmInvalid) {
		t.Errorf("confirm without the schedule: %v", err)
	}
	notWired := newScheduleCards(NewLibraryService(&fakeLibrary{}, nil, nil, discard()), discard())
	if _, err := notWired.UndoSchedule(t.Context(), tokA); !errors.Is(err, ErrAgentConfirmInvalid) {
		t.Errorf("undo with a library without the schedule: %v", err)
	}
	if _, err := newScheduleCards(nil, discard()).Card(t.Context(), agentCardScheduleSubjects, tokA); !errors.Is(err, ErrAgentConfirmInvalid) {
		t.Errorf("card without a library: %v", err)
	}

	// The generic Confirm never applies a package: the choices are needed.
	s := newScheduleConfirm(&fakeScheduleSubjects{proposal: agentSchedule(tokA)}, &fakeLibrary{}, discard())
	if _, err := s.Confirm(t.Context(), agentCardScheduleSubjects, tokA, "", "", "", ""); !errors.Is(err, ErrAgentConfirmInvalid) {
		t.Errorf("Confirm of a schedule card: %v", err)
	}
}

func TestScheduleUndoProblems(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA), res: partialResult(), undoErr: library.ErrUnavailable}
	s := newScheduleConfirm(uc, &fakeLibrary{}, discard())
	if _, err := s.ConfirmSchedule(t.Context(), tokA, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UndoSchedule(t.Context(), tokA); !errors.Is(err, ErrAgentConfirmScheduleUndoAgain) {
		t.Fatalf("failed undo: %v", err)
	}
	if read, _ := s.Card(t.Context(), agentCardScheduleSubjects, tokA); read.State != agentStateDone {
		t.Fatalf("after a failed undo the card reads %s", read.State)
	}

	// After a restart the outcome is gone: the undo says so instead of doing nothing silently.
	fresh := newScheduleConfirm(uc, &fakeLibrary{}, discard())
	if _, err := fresh.UndoSchedule(t.Context(), tokA); !errors.Is(err, ErrAgentConfirmScheduleUndoGone) {
		t.Fatalf("undo after a restart: %v", err)
	}
	// A restart is not the only cause (the page may have taken it back), so the message names none.
	if msg := ErrAgentConfirmScheduleUndoGone.Error(); strings.Contains(msg, "перезапуск") || !strings.Contains(msg, "«Предметы»") {
		t.Errorf("undo gone says %q", msg)
	}
}

func TestScheduleDecline(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA)}
	s := newScheduleConfirm(uc, &fakeLibrary{}, discard())
	got, err := s.Decline(t.Context(), agentCardScheduleSubjects, tokA)
	if err != nil || got.State != agentStateDeclined || got.TargetID != tokA {
		t.Fatalf("decline = %+v, %v", got, err)
	}
	if uc.creates != 0 {
		t.Error("«Нет» created something")
	}
	// Read again with the token still current, the card stays declined instead of waiting.
	if read, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA); err != nil || read.State != agentStateDeclined {
		t.Fatalf("declined read again = %+v, %v", read, err)
	}
	if _, err := s.Decline(t.Context(), agentCardScheduleSubjects, "x"); !errors.Is(err, ErrAgentConfirmInvalid) {
		t.Errorf("decline of a bad token: %v", err)
	}
}

// TestScheduleCardLogsNoScheduleText: names and spellings of the schedule go to the card as labels, never to the log.
func TestScheduleCardLogsNoScheduleText(t *testing.T) {
	log, rec := newScheduleRecordLog()
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA), res: partialResult(), err: library.ErrUnavailable}
	s := newScheduleConfirm(uc, &fakeLibrary{}, log)
	if _, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmSchedule(t.Context(), tokA, []ScheduleChoice{{Group: "матанализ", Action: ScheduleActionCreate, Name: "Матанализ 2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UndoSchedule(t.Context(), tokA); err != nil {
		t.Fatal(err)
	}
	for _, v := range rec.texts() {
		for _, text := range []string{"атан", "изика", tokA} {
			if strings.Contains(v, text) {
				t.Errorf("the log holds %q: %q", text, v)
			}
		}
	}
}

// TestPanelShowsTheWave10Cards: the panel no longer drops a schedule_subjects or an offer_button card; the schedule
// card names its token as the target the binding reads, a button its action and typed ID only.
func TestPanelShowsTheWave10Cards(t *testing.T) {
	proposal, err := values.NewRef(values.KindProposal, tokA)
	if err != nil {
		t.Fatal(err)
	}
	group, err := values.NewRef(values.KindScheduleGroup, "g-1")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := cardView(values.ScheduleSubjectsCard{Proposal: proposal, Groups: []values.Ref{group}})
	if !ok || v.Kind != "schedule_subjects" || v.Target == nil || *v.Target != (AgentRefView{Kind: agentCardScheduleSubjects, ID: tokA}) {
		t.Fatalf("schedule card view = %+v, %v", v, ok)
	}

	for _, a := range values.OfferActions() {
		_, kind, _ := values.OfferTarget(a)
		card := values.OfferButtonCard{Action: a}
		if kind != "" {
			if card.Target, err = values.NewRef(kind, "x1"); err != nil {
				t.Fatal(err)
			}
		}
		v, ok := cardView(card)
		if !ok || v.Kind != "offer_button" || v.Offer == nil || v.Offer.Action != string(a) {
			t.Fatalf("%s: view = %+v, %v", a, v, ok)
		}
		if (kind == "") != (v.Offer.Target == nil) || (kind != "" && *v.Offer.Target != (AgentRefView{Kind: string(kind), ID: "x1"})) {
			t.Fatalf("%s: target = %+v", a, v.Offer.Target)
		}
		raw, err := json.Marshal(v.Offer)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 2 {
			t.Fatalf("%s: offer json %s", a, raw)
		}
	}
	if _, ok := cardView(values.OfferButtonCard{Action: "open_url"}); ok {
		t.Error("an unknown button is shown")
	}
}

// TestSchedulePartialKeepsTheChoices: after a «Да» that stopped midway the card read again has no groups, so a second
// «Да» may send no choices; the earlier ones still hold (a skipped group is never created by the defaults), a group
// chosen anew takes the new choice, and once done the next «Да» runs only what it sends.
func TestSchedulePartialKeepsTheChoices(t *testing.T) {
	uc := &fakeScheduleSubjects{proposal: agentSchedule(tokA), res: partialResult(), err: library.ErrUnavailable}
	s := newScheduleConfirm(uc, &fakeLibrary{}, discard())

	first := []ScheduleChoice{
		{Group: "матанализ", Action: ScheduleActionCreate, Name: "Матанализ 1"},
		{Group: "физика", Action: ScheduleActionSkip},
	}
	if got, err := s.ConfirmSchedule(t.Context(), tokA, first); err != nil || got.State != agentStateConfirmed {
		t.Fatalf("partial = %+v, %v", got, err)
	}

	steps := []struct {
		name    string
		choices []ScheduleChoice
		err     error
		want    []schedulesubjects.Choice
	}{
		{
			name: "no choices: the earlier ones", err: library.ErrUnavailable,
			want: []schedulesubjects.Choice{
				{Group: "матанализ", Action: schedulesubjects.GroupCreate, Name: "Матанализ 1"},
				{Group: "физика", Action: schedulesubjects.GroupSkip},
			},
		},
		{
			name:    "one group anew: it wins, the other is kept",
			choices: []ScheduleChoice{{Group: "матанализ", Action: ScheduleActionCreate, Name: "Матанализ 2"}},
			want: []schedulesubjects.Choice{
				{Group: "матанализ", Action: schedulesubjects.GroupCreate, Name: "Матанализ 2"},
				{Group: "физика", Action: schedulesubjects.GroupSkip},
			},
		},
		{name: "done: nothing kept"},
	}
	for _, st := range steps {
		uc.set(partialResult(), st.err)
		if _, err := s.ConfirmSchedule(t.Context(), tokA, st.choices); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		uc.mu.Lock()
		got := uc.gotChoices
		uc.mu.Unlock()
		if len(got) != len(st.want) || (len(st.want) > 0 && !reflect.DeepEqual(got, st.want)) {
			t.Fatalf("%s: choices = %+v\nwant %+v", st.name, got, st.want)
		}
	}
}

// TestScheduleCardSharesThePageBinding: the card and the page run on one binding, so the card takes back what the
// page created, and a «Да» on the card while the page creates answers busy.
func TestScheduleCardSharesThePageBinding(t *testing.T) {
	uc := &fakeScheduleSubjects{
		proposal: agentSchedule(tokA), res: partialResult(),
		undo: library.ScheduleUndo{Removed: []shared.SubjectID{"sched-1"}},
	}
	page := NewLibraryService(scheduleLibrary{&fakeLibrary{}, uc}, nil, nil, discard())
	s := newScheduleCards(page, discard())

	if out := page.CreateFromSchedule(t.Context(), tokA, nil); out.Problem != "" {
		t.Fatalf("page create = %+v", out)
	}
	if read, err := s.Card(t.Context(), agentCardScheduleSubjects, tokA); err != nil || read.State != agentStateDone {
		t.Fatalf("card after the page's create = %+v, %v", read, err)
	}
	if undone, err := s.UndoSchedule(t.Context(), tokA); err != nil || undone.State != agentStateUndone || len(uc.undos) != 1 {
		t.Fatalf("card undo of the page's batch = %+v, %v (%d undos)", undone, err, len(uc.undos))
	}

	uc.mu.Lock()
	uc.block, uc.started = make(chan struct{}), make(chan struct{}, 1)
	uc.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		page.CreateFromSchedule(t.Context(), tokA, nil)
	}()
	<-uc.started
	_, err := s.ConfirmSchedule(t.Context(), tokA, nil)
	close(uc.block)
	<-done
	if !errors.Is(err, ErrAgentConfirmScheduleBusy) {
		t.Fatalf("card «Да» while the page creates: %v", err)
	}
}

// TestOpenServicesSharesTheLibraryWithTheCards: the shell gives the confirm cards the page's own library binding.
func TestOpenServicesSharesTheLibraryWithTheCards(t *testing.T) {
	opts := validOptions()
	opts.OpenCore = func(context.Context) (Core, error) {
		c := validCore()
		c.Library = scheduleLibrary{&fakeLibrary{}, &fakeScheduleSubjects{proposal: agentSchedule(tokA)}}
		return c, nil
	}
	sh, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := sh.OpenServices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if b.AgentConfirm.schedule == nil || b.AgentConfirm.schedule != b.Library {
		t.Fatalf("the cards have %p, the page %p", b.AgentConfirm.schedule, b.Library)
	}
}
