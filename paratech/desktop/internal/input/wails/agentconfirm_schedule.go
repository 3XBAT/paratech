package wails

import (
	"context"
	"sync"
)

// The schedule_subjects card (ADR-0033 §4, О-12 б): the agent asked for subjects from the schedule; the desktop built
// the package (cases/library ScheduleSubjects) and the journal holds its token. The card shows the groups as the page's
// review does, «Да» applies the student's choices through the page's own binding (LibraryService.CreateFromSchedule:
// the same choices, a doubtful member without a choice is merged, one subject per transaction, idempotent by token),
// and «Отменить всё» takes the outcome back (LibraryService.UndoFromSchedule). The agent loop never reaches this: only
// the student's click does (hard rule 3). Schedule text goes back as plain-text labels only, never into a log.

const (
	opScheduleCard    = "ui: agent schedule subjects card"
	opScheduleConfirm = "ui: agent schedule subjects"
	opScheduleUndo    = "ui: agent schedule subjects undo"
)

// agentCardScheduleSubjects is the kind of the card and its target kind as the panel reads them; the target ID is the
// token of the proposal (not a homework proposal, so not the kind "proposal").
const agentCardScheduleSubjects = "schedule_subjects"

// agentStateUndone is a schedule_subjects card after «Отменить всё».
const agentStateUndone = "undone"

// Messages of the schedule_subjects card.
const (
	// Neutral: stale also shows for a card applied before a restart or applied from the page.
	msgScheduleStale = "Предложение устарело: расписание или предметы изменились. Проверьте список на странице " +
		"«Предметы» или попросите агента предложить предметы ещё раз."
	msgSchedulePartial = "Создано не всё. Нажмите «Да» ещё раз — уже созданное не повторится, или «Отменить всё»."
)

// Errors of the schedule_subjects card.
const (
	ErrAgentConfirmScheduleBusy = agentConfirmError("Предметы из расписания сейчас создаются или отменяются. " +
		"Подождите немного и нажмите ещё раз.")
	ErrAgentConfirmScheduleName = agentConfirmError("Это название уже занято другим предметом или строкой списка. " +
		"Выберите другое и нажмите «Да».")
	ErrAgentConfirmScheduleUndoGone = agentConfirmError("Отменить отсюда уже нельзя. " +
		"Лишние предметы можно убрать на странице «Предметы».")
	ErrAgentConfirmScheduleUndoAgain = agentConfirmError("Отменилось не всё. Нажмите «Отменить всё» ещё раз — " +
		"повтор безопасен.")
)

// AgentScheduleCard is the content of a schedule_subjects card. Waiting: Groups and Omitted as ScheduleSubjects of the
// page gives them (the default action per group; a doubtful member is merged unless the student chooses). Done (or
// confirmed: stopped midway, «Да» again is safe): Result, whose skipped rows with RemovedSubjectID offer «Вернуть»
// (LibraryService.RestoreSubject at RemovedSubjectVersion, as the page does). Undone: Undo and the Result it took back.
type AgentScheduleCard struct {
	Groups  []ScheduleGroup       `json:"groups"`
	Omitted int                   `json:"omitted"`
	Result  *ScheduleCreateResult `json:"result"`
	Undo    *ScheduleUndoResult   `json:"undo"`
}

// scheduleCards remembers, in this run, the tokens whose «Да» stopped midway (with the choices it ran with), the undos
// and the «Нет», so a card read again says so; the outcome itself is the binding's (LibraryService journal).
type scheduleCards struct {
	mu       sync.Mutex
	partial  map[string][]ScheduleChoice // the choices of the «Да» that stopped midway
	undone   map[string]ScheduleUndoResult
	declined map[string]bool
}

func (c *scheduleCards) markUndone(token string, u ScheduleUndoResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.undone == nil {
		c.undone = map[string]ScheduleUndoResult{}
	}
	c.undone[token] = u
}

func (c *scheduleCards) markDeclined(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.declined == nil {
		c.declined = map[string]bool{}
	}
	c.declined[token] = true
}

// created records a «Да» of token that did something with choices: the undo and «Нет» marks go, and partial tells
// whether it stopped midway (then the choices are kept for the next «Да»).
func (c *scheduleCards) created(token string, choices []ScheduleChoice, partial bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.undone, token)
	delete(c.declined, token)
	if c.partial == nil {
		c.partial = map[string][]ScheduleChoice{}
	}
	if partial {
		c.partial[token] = append([]ScheduleChoice{}, choices...)
	} else {
		delete(c.partial, token)
	}
}

func (c *scheduleCards) partialOf(token string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.partial[token]
	return ok
}

func (c *scheduleCards) declinedOf(token string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.declined[token]
}

// choicesFor is what a «Да» of token runs with. After a partial «Да» the card read again has no groups, so it may send
// fewer choices (or none): a group the student chose for before keeps that choice unless chosen anew, so a group set
// to «Не заводить» or renamed is never created by the defaults.
func (c *scheduleCards) choicesFor(token string, choices []ScheduleChoice) []ScheduleChoice {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, ok := c.partial[token]
	if !ok {
		return choices
	}
	given := make(map[string]bool, len(choices))
	for _, ch := range choices {
		given[ch.Group] = true
	}
	out := append([]ScheduleChoice{}, choices...)
	for _, ch := range prev {
		if !given[ch.Group] {
			out = append(out, ch)
		}
	}
	return out
}

func (c *scheduleCards) undoneOf(token string) (ScheduleUndoResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.undone[token]
	return u, ok
}

// scheduleSubjectsCard is the card of token: undone or done when this run applied it, else waiting while the token is
// still the one of the current proposal, else stale (also after a restart: the outcome is kept in memory only, as on
// the page).
func (s *AgentConfirmService) scheduleSubjectsCard(ctx context.Context, token string) (AgentConfirmCard, error) {
	lib, err := s.cardSchedule(token)
	if err != nil {
		return AgentConfirmCard{}, err
	}
	if u, ok := s.scheduleState.undoneOf(token); ok {
		card := scheduleCard(token, agentStateUndone)
		card.Schedule.Undo, card.Schedule.Result = &u, s.scheduleOutcome(ctx, lib, token)
		return card, nil
	}
	if res := s.scheduleOutcome(ctx, lib, token); res != nil {
		card := scheduleCard(token, agentStateDone)
		if s.scheduleState.partialOf(token) {
			card.State, card.Message = agentStateConfirmed, msgSchedulePartial
		}
		card.Schedule.Result = res
		return card, nil
	}
	if s.scheduleState.declinedOf(token) {
		return scheduleCard(token, agentStateDeclined), nil
	}

	view := lib.ScheduleSubjects(ctx)
	switch {
	case view.Problem != "":
		s.log.Warn(opScheduleCard, "problem", view.Problem)
		return AgentConfirmCard{}, ErrAgentConfirmUnavailable
	case view.Token != token:
		card := scheduleCard(token, agentStateStale)
		card.Message = msgScheduleStale
		return card, nil
	}
	card := scheduleCard(token, agentStateWaiting)
	card.Schedule.Groups, card.Schedule.Omitted = view.Groups, view.Omitted
	return card, nil
}

// ConfirmSchedule is «Да» on a schedule_subjects card: the student's choices (as the page sends them) are applied to
// the proposal of token. Stale: nothing ran, the card says to ask the agent again. Stopped midway: the card is
// confirmed with what was done, «Да» again does the rest without repeating anything, «Отменить всё» takes it back.
func (s *AgentConfirmService) ConfirmSchedule(ctx context.Context, token string, choices []ScheduleChoice) (AgentConfirmCard, error) {
	lib, err := s.cardSchedule(token)
	if err != nil {
		return AgentConfirmCard{}, err
	}
	choices = s.scheduleState.choicesFor(token, choices)
	out := lib.CreateFromSchedule(ctx, token, choices)
	did := len(out.Created) > 0 || len(out.Aliased) > 0
	if did {
		s.scheduleState.created(token, choices, out.Problem != "")
	}
	s.log.Info(opScheduleConfirm, "created", len(out.Created), "aliased", len(out.Aliased), "skipped", len(out.Skipped),
		"problem", out.Problem)

	switch {
	case out.Problem == "":
		card := scheduleCard(token, agentStateDone)
		card.Schedule.Result = &out
		return card, nil
	case did:
		card := scheduleCard(token, agentStateConfirmed)
		card.Message, card.Schedule.Result = msgSchedulePartial, &out
		return card, nil
	}
	switch out.Problem {
	case ScheduleProblemStale:
		card := scheduleCard(token, agentStateStale)
		card.Message = msgScheduleStale
		return card, nil
	case ScheduleProblemBusy:
		return AgentConfirmCard{}, ErrAgentConfirmScheduleBusy
	case ScheduleProblemNameTaken:
		return AgentConfirmCard{}, ErrAgentConfirmScheduleName
	case ScheduleProblemInvalid:
		return AgentConfirmCard{}, ErrAgentConfirmInvalid
	}
	return AgentConfirmCard{}, ErrAgentConfirmUnavailable
}

// UndoSchedule is «Отменить всё» on a schedule_subjects card: the outcome this run recorded for token is taken back
// (also a partial one); a subject that holds files or notes stays (Undo.Kept). A repeated undo is safe.
func (s *AgentConfirmService) UndoSchedule(ctx context.Context, token string) (AgentConfirmCard, error) {
	lib, err := s.cardSchedule(token)
	if err != nil {
		return AgentConfirmCard{}, err
	}
	u := lib.UndoFromSchedule(ctx, token)
	s.log.Info(opScheduleUndo, "removed", len(u.Removed), "kept", len(u.Kept), "problem", u.Problem)
	switch u.Problem {
	case "":
	case ScheduleProblemInvalid:
		return AgentConfirmCard{}, ErrAgentConfirmScheduleUndoGone
	case ScheduleProblemBusy:
		return AgentConfirmCard{}, ErrAgentConfirmScheduleBusy
	default:
		return AgentConfirmCard{}, ErrAgentConfirmScheduleUndoAgain
	}
	s.scheduleState.markUndone(token, u)
	card := scheduleCard(token, agentStateUndone)
	card.Schedule.Undo, card.Schedule.Result = &u, s.scheduleOutcome(ctx, lib, token)
	return card, nil
}

// declineScheduleSubjects is «Нет»: nothing is created; the card remembers it in this run, so read again it does not
// wait.
func (s *AgentConfirmService) declineScheduleSubjects(token string) (AgentConfirmCard, error) {
	if _, err := s.cardSchedule(token); err != nil {
		return AgentConfirmCard{}, err
	}
	s.scheduleState.markDeclined(token)
	return scheduleCard(token, agentStateDeclined), nil
}

// scheduleOutcome is what this run created for token (every try merged), with the versions of the removed subjects
// a skipped row may restore read now; nil when nothing was created or aliased.
func (s *AgentConfirmService) scheduleOutcome(ctx context.Context, lib *LibraryService, token string) *ScheduleCreateResult {
	res, ok := lib.schedule.journal.lookup(token)
	if !ok {
		return nil
	}
	out, _ := scheduleCreateResult(res)
	lib.removedVersions(ctx, out.Skipped)
	return &out
}

// cardSchedule checks the target of the card: a token of the schedule proposal, with subjects from the schedule wired.
func (s *AgentConfirmService) cardSchedule(token string) (*LibraryService, error) {
	if s.schedule == nil || !scheduleTokenShape(token) {
		return nil, ErrAgentConfirmInvalid
	}
	return s.schedule, nil
}

// scheduleTokenShape reports whether token looks like a token of cases/library: 64 lowercase hex digits.
func scheduleTokenShape(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, r := range token {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func scheduleCard(token, state string) AgentConfirmCard {
	return AgentConfirmCard{
		Kind: agentCardScheduleSubjects, TargetKind: agentCardScheduleSubjects, TargetID: token, State: state,
		Schedule: &AgentScheduleCard{Groups: []ScheduleGroup{}},
	}
}
