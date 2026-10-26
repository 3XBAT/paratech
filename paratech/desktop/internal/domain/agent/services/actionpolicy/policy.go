package actionpolicy

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/agent/values"
)

// Class is the class of a tool.
type Class string

// Classes.
const (
	ClassR  Class = "R"  // read-only, free unless paid
	ClassU  Class = "U"  // reversible local, done at once with «Отменить»
	ClassC  Class = "C"  // irreversible: only a proposal/preview card; the student clicks
	ClassUI Class = "UI" // card only
)

// AgentCanExecute reports whether the agent may run a tool of the class itself. Class C is never executed by the
// agent: its call only creates a proposal card.
func (c Class) AgentCanExecute() bool { return c == ClassR || c == ClassU || c == ClassUI }

// IsWrite reports whether the class changes local state and counts as a write of the request.
func (c Class) IsWrite() bool { return c == ClassU }

// Rule is the policy entry of a tool.
type Rule struct {
	Class Class
	Paid  bool // starts paid work on the Hub (quarantined reader or a job of the strong model)
	// AlwaysPlan marks work that always goes through a plan: lecture text and turnkey writing (ADR-0021).
	AlwaysPlan bool
}

// ErrUnknownTool is returned for a tool outside the table.
var ErrUnknownTool = errors.New("tool has no policy")

// rules is the class table of catalog v6 (a test in desktop/tests compares it with the golden catalog) and of the
// retired tools (values.RetiredTools), so the journal entries and Undo of their stored actions keep working.
var rules = map[values.Tool]Rule{
	values.ToolListSubjects:           {Class: ClassR},
	values.ToolFindLessons:            {Class: ClassR},
	values.ToolListAssignments:        {Class: ClassR},
	values.ToolJobStatus:              {Class: ClassR},
	values.ToolDeliveryStatus:         {Class: ClassR},
	values.ToolSearch:                 {Class: ClassR},
	values.ToolAnswerQuestion:         {Class: ClassR, Paid: true},
	values.ToolInspectUpload:          {Class: ClassR, Paid: true},
	values.ToolCreateSubject:          {Class: ClassU},
	values.ToolCreateFolder:           {Class: ClassU},
	values.ToolPlaceUpload:            {Class: ClassU},
	values.ToolLinkMaterial:           {Class: ClassU},
	values.ToolSetLessonInfo:          {Class: ClassU},
	values.ToolMakeSummary:            {Class: ClassU, Paid: true},
	values.ToolMakeLectureText:        {Class: ClassU, Paid: true, AlwaysPlan: true},
	values.ToolFindHomework:           {Class: ClassU, Paid: true},
	values.ToolGenerateDraft:          {Class: ClassU, Paid: true},
	values.ToolEditDue:                {Class: ClassU},
	values.ToolCreateAssignment:       {Class: ClassU}, // one added at once; a second in the request needs a plan
	values.ToolTriageAssignment:       {Class: ClassU, Paid: true},
	values.ToolWriteWork:              {Class: ClassU, Paid: true, AlwaysPlan: true},
	values.ToolProposeDeadline:        {Class: ClassC},
	values.ToolProposeSend:            {Class: ClassC},
	values.ToolProposeDeleteRecording: {Class: ClassC},
	values.ToolShowPlan:               {Class: ClassUI},
	values.ToolAskQuestions:           {Class: ClassUI},
	values.ToolReply:                  {Class: ClassUI},
	values.ToolCannotDo:               {Class: ClassUI},
}

// Of returns the policy entry of a tool; a tool outside the table is ErrUnknownTool.
func Of(t values.Tool) (Rule, error) {
	r, ok := rules[t]
	if !ok {
		return Rule{}, fmt.Errorf("%w: %q", ErrUnknownTool, string(t))
	}
	return r, nil
}

// PlanThresholdMicroUSD is the cost of one paid job above which a plan is needed (D8, ≈ $0.05).
const PlanThresholdMicroUSD = 50_000

// Triage is the outcome of triage_assignment seen in the request.
type Triage string

// Triage outcomes. Zero means no triage in the request.
const (
	TriageFull       Triage = "full"
	TriagePartly     Triage = "partly"
	TriageImpossible Triage = "impossible"
)

// Facts are what the case knows about a write before it runs, counted over the whole request.
type Facts struct {
	WritesBefore  int   // U calls already admitted in this request
	CostMicroUSD  int64 // estimate for a paid call; zero or less means unknown and counts as above the threshold
	Triage        Triage
	ReplacesReady bool // the call would replace a ready result (a summary or lecture text that exists)
	// FreeTopic: the call is a work on a free topic (FreeTopic) the student allowed without the free-work card
	// («Больше не показывать», TASK-188): its unknown cost does not ask for a plan; it is the paid job without a plan.
	FreeTopic bool
}

// FreeTopic reports whether call is a work on a free topic: triage_assignment with a topic instead of an assignment
// or an upload (catalog v4, TASK-188).
func FreeTopic(call values.Call) bool {
	return call.Tool() == values.ToolTriageAssignment && call.Args().Has("topic")
}

// Reason tells why a plan is needed.
type Reason string

// Reasons for a plan.
const (
	ReasonNone        Reason = ""
	ReasonSecondWrite Reason = "second_write"
	ReasonCostly      Reason = "costly"
	ReasonAlwaysPlan  Reason = "always_plan"
	ReasonTriage      Reason = "triage_not_full"
	ReasonReplaces    Reason = "replaces_ready"
)

// NeedsPlan reports whether a U call may run only as a step of an approved plan (ADR-0021, design §2.4, D8): a second
// write in the request, a paid job above the threshold (or with an unknown cost), lecture text and turnkey writing,
// a call that would replace a ready result (TASK-123: its Undo can only stop the job, the old result is not kept),
// or a triage that said «partly» or «impossible». A free topic the student allowed without the card is not costly
// (Facts.FreeTopic): the student's standing choice stands for the plan of that one call. Other classes never need a
// plan here.
func NeedsPlan(t values.Tool, f Facts) (Reason, error) {
	r, err := Of(t)
	if err != nil {
		return ReasonNone, err
	}
	if r.Class != ClassU {
		return ReasonNone, nil
	}
	switch {
	case r.AlwaysPlan:
		return ReasonAlwaysPlan, nil
	case f.ReplacesReady:
		return ReasonReplaces, nil
	case f.WritesBefore > 0:
		return ReasonSecondWrite, nil
	case f.Triage == TriagePartly || f.Triage == TriageImpossible:
		return ReasonTriage, nil
	case r.Paid && (!f.FreeTopic || t != values.ToolTriageAssignment) &&
		(f.CostMicroUSD <= 0 || f.CostMicroUSD > PlanThresholdMicroUSD):
		return ReasonCostly, nil
	}
	return ReasonNone, nil
}

// ErrUnknownID means an ID in the arguments did not appear in a field of the same kind in the base, the scope or
// the tool results of this thread; the call is refused with is_error.
var ErrUnknownID = errors.New("id of unknown provenance")

// CheckProvenance checks every ref against the known IDs. The check is typed: a lesson ID seen as a lesson does not
// pass as a recording.
func CheckProvenance(refs []values.Ref, known values.RefSet) error {
	for _, r := range refs {
		if !known.Has(r) {
			return fmt.Errorf("%w: %s", ErrUnknownID, r.Kind())
		}
	}
	return nil
}
