package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of propose_subjects_from_schedule, for the log.
const (
	opScheduleSubjects        = "agent: subjects from the schedule"
	opScheduleSubjectsJournal = "agent: subjects from the schedule journal"
)

// scheduleGroupPrefix starts the ID of a group on a schedule-subjects card.
const scheduleGroupPrefix = "g-"

// ScheduleSubjects is the proposal of subjects from the schedule (cases/library, the use case of the page, TASK-310).
// It has no method that creates or takes back subjects: only the student's «Да» in input/wails does (ADR-0033 §4,
// hard rule 3).
type ScheduleSubjects interface {
	ScheduleSubjects(ctx context.Context) (library.ScheduleProposal, error)
}

// proposeSubjects runs propose_subjects_from_schedule (class C, a desktop-built batch): the use case groups the
// schedule strings, the journal records the call PROPOSED with the proposal's token as its target (so the card is read
// again after a restart: the token is checked against a fresh proposal) and the panel gets the card of the token and
// its groups. The planner gets only the number of groups: no name, no schedule string, no token, no ID. No group
// gives nothing_to_create and no card. A call run again under the same ID answers from its journal entry.
func (t tools) proposeSubjects(ctx context.Context, st agentturn.Step, c agentturn.Call) ran {
	id, tool := c.Call.ID(), c.Call.Tool()
	refuse := func(why values.Refusal) ran {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, why)}, write: true}
	}
	if t.journal == nil || t.subjects == nil {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseUnavailable)}}
	}
	ctx = context.WithoutCancel(ctx) // a proposal once made is real; its card must reach the panel

	if e, err := t.journal.Find(ctx, st.Thread, st.Turn, id); err == nil {
		return t.replaySubjects(ctx, c, e)
	} else if !errors.Is(err, shared.ErrNotFound) {
		t.log.WarnContext(ctx, opScheduleSubjectsJournal, "err", err)
		return refuse(values.RefuseFailed)
	}

	p, err := t.subjects.ScheduleSubjects(ctx)
	if err != nil {
		// The use case puts no schedule string into its errors.
		t.log.WarnContext(ctx, opScheduleSubjects, "err", err)
		return refuse(values.RefuseFailed)
	}
	if len(p.Groups) == 0 {
		return ran{out: agentturn.Outcome{Result: values.NothingToCreateResult(id)}, write: true}
	}
	card, ok := scheduleCard(p)
	if !ok {
		t.log.WarnContext(ctx, opScheduleSubjects, "groups", len(p.Groups), "err", "card not addressable")
		return refuse(values.RefuseFailed)
	}
	res, err := values.ScheduleSubjectsResult(id, len(card.Groups))
	if err != nil {
		return refuse(values.RefuseFailed)
	}

	in := agentjournal.BeginInput{Thread: st.Thread, Turn: st.Turn, CallID: id, Tool: tool}
	if _, err := t.journal.Propose(ctx, in, card.Proposal); err != nil {
		if e, ferr := t.journal.Find(ctx, st.Thread, st.Turn, id); ferr == nil {
			return t.replaySubjects(ctx, c, e) // the same call was journaled meanwhile
		}
		// The card still works: the binding reads it from the token alone; only its journal line is missing.
		t.log.WarnContext(ctx, opScheduleSubjectsJournal, "err", err)
	}
	return ran{out: agentturn.Outcome{Result: res}, write: true, cards: []values.Card{card}}
}

// replaySubjects answers a call that has a journal entry, without a second card: the number of groups when the
// proposal is still the one of the entry, else no count. A call ID reused for another tool is refused.
func (t tools) replaySubjects(ctx context.Context, c agentturn.Call, e agentjournal.Entry) ran {
	id := c.Call.ID()
	if e.Tool != c.Call.Tool() || e.Status != action.StatusProposed || e.Proposal.Kind() != values.KindProposal {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseInvalidArgs)}, write: true}
	}
	groups := 0
	if p, err := t.subjects.ScheduleSubjects(ctx); err == nil && p.Token == e.Proposal.ID() {
		groups = min(len(p.Groups), values.MaxScheduleGroups)
	}
	res, err := values.ScheduleSubjectsResult(id, groups)
	if err != nil {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseFailed)}, write: true}
	}
	return ran{out: agentturn.Outcome{Result: res}, write: true}
}

// scheduleCard is the card of proposal p: its token and one ID per group (scheduleGroupID). false when the token is
// not addressable or the groups do not fit a card.
func scheduleCard(p library.ScheduleProposal) (values.ScheduleSubjectsCard, bool) {
	target, ok := refOfID(values.KindProposal, p.Token)
	if !ok {
		return values.ScheduleSubjectsCard{}, false
	}
	card := values.ScheduleSubjectsCard{Proposal: target, Groups: make([]values.Ref, 0, len(p.Groups))}
	for _, g := range p.Groups {
		r, ok := refOfID(values.KindScheduleGroup, scheduleGroupID(g.Key))
		if !ok {
			return values.ScheduleSubjectsCard{}, false
		}
		card.Groups = append(card.Groups, r)
	}
	return card, card.Validate() == nil
}

// scheduleGroupID is the ID of a group on the card: a hash of its key, which is schedule text and never an ID. Keys
// are distinct within a proposal, so the IDs are.
func scheduleGroupID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return scheduleGroupPrefix + hex.EncodeToString(sum[:16])
}
