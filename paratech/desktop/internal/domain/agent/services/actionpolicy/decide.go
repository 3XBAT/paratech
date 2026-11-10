package actionpolicy

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/agent/values"
)

// ErrUnknownAction means the action of a call is not one the policy of its tool knows; the call is refused.
var ErrUnknownAction = errors.New("action has no policy")

// Decision is the policy of one call. It is decided here from the tool, the action enum the parser checked and the
// Facts the case read from local data, never from other model output (hard rule 3).
type Decision struct {
	Rule
	Card values.CardKind // the card a C or UI call creates; "" for R and U
	// NoUndo marks a U call that runs at once and has no compensation: recording{stop} (ADR-0033 §2, О-7 а). Its
	// journal entry is DONE without «Отменить»; the result line offers «Начать запись».
	NoUndo bool
}

// Decide returns the policy of a parsed call. A U decision then goes through NeedsPlan. mark_assignments_done is one
// U write and one journal entry for its whole list (ADR-0033 §9), however many assignments it names.
func Decide(call values.Call, f Facts) (Decision, error) {
	act, _ := call.Args().Enum(values.ArgAction)
	return decide(call.Tool(), act, f)
}

func decide(t values.Tool, act string, f Facts) (Decision, error) {
	r, err := Of(t)
	if err != nil {
		return Decision{}, err
	}
	d := Decision{Rule: r, Card: cards[t]}
	byAction, ok := actions[t]
	if !ok {
		if act != "" {
			return Decision{}, fmt.Errorf("%w: %s has no actions", ErrUnknownAction, t)
		}
		return d, nil
	}
	rule, ok := byAction[act]
	if !ok {
		return Decision{}, fmt.Errorf("%w: %s", ErrUnknownAction, t)
	}
	return rule(d, f), nil
}

// cards are the cards of the C and UI tools.
var cards = map[values.Tool]values.CardKind{
	values.ToolProposeDeadline:             values.CardConfirm,
	values.ToolProposeSend:                 values.CardConfirm,
	values.ToolProposeDeleteRecording:      values.CardConfirm,
	values.ToolProposeSubjectsFromSchedule: values.CardScheduleSubjects,
	values.ToolShowPlan:                    values.CardPlan,
	values.ToolAskQuestions:                values.CardQuestions,
	values.ToolReply:                       values.CardReply,
	values.ToolCannotDo:                    values.CardCannotDo,
	values.ToolOfferButton:                 values.CardOfferButton,
	values.ToolSetTeacher:                  values.CardTeacher,
	values.ToolClearTeacher:                values.CardTeacher,
	values.ToolSetProfileField:             values.CardProfileField,
}

type actionRule func(Decision, Facts) Decision

// actions are the per-action rules of the tools with an action enum (ADR-0033 §10). Every action of the catalog is
// listed; one outside is ErrUnknownAction.
var actions = map[values.Tool]map[string]actionRule{
	values.ToolChangeAssignment: {
		values.AssignmentEdit:    same,
		values.AssignmentRestore: same,
		values.AssignmentDelete:  deleteAssignment,
	},
	values.ToolNote: {
		values.NoteAddText: same, values.NoteAddUpload: same, values.NoteMove: same, values.NoteRename: same,
		values.NoteRemove: same, // soft: Undo restores
	},
	values.ToolRecording: {
		values.RecordingStart: same, // Undo stops and keeps the audio
		values.RecordingStop:  stopRecording,
	},
	values.ToolOfferButton: func() map[string]actionRule {
		out := map[string]actionRule{}
		for _, a := range values.OfferActions() {
			out[string(a)] = same
		}
		return out
	}(),
}

func same(d Decision, _ Facts) Decision { return d }

// deleteAssignment: a soft delete is U, but one with a Google event only goes through the «Да» card (О-6 б), as the
// event goes with it.
func deleteAssignment(d Decision, f Facts) Decision {
	if f.HasCalendarEvent {
		d.Class, d.Card = ClassC, values.CardDeleteAssignment
	}
	return d
}

// stopRecording runs at once without a compensation (О-7 а): the audio is kept, a new recording is a new start.
func stopRecording(d Decision, _ Facts) Decision {
	d.NoUndo = true
	return d
}
