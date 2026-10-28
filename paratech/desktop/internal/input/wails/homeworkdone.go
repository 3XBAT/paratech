package wails

import (
	"context"
	"errors"
	"slices"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/shared"
)

const (
	opMarkDone    = "ui: mark assignments done"
	opRestoreDone = "ui: restore the done marks"
)

// Problems of MarkDone and RestoreDone as the page reads them; the per-assignment codes are homeworkflow.DoneCode.
const (
	doneInvalid     = "invalid"     // no assignments, more than homeworkflow.MaxMarkDone, or a request the core refused
	doneUnavailable = "unavailable" // the core failed: nothing is known about the outcome
)

// knownDoneCodes are the per-assignment codes the page reads; any other one is reported as failed.
var knownDoneCodes = map[homeworkflow.DoneCode]bool{
	homeworkflow.DoneChanged: true, homeworkflow.DoneUnchanged: true, homeworkflow.DoneNotFound: true,
	homeworkflow.DoneDeleted: true, homeworkflow.DoneConflict: true, homeworkflow.DoneFailed: true,
}

// DoneOutcomeItem is what happened to one assignment: Code is "changed", "unchanged", "not_found", "deleted",
// "conflict" or "failed"; Done and Version are the stored state for "changed" and "unchanged", false and 0 otherwise.
type DoneOutcomeItem struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Done    bool   `json:"done"`
	Version int64  `json:"version"`
}

// DonePriorItem is one changed assignment with the version the call saved; «Отменить» sends it back as it is.
type DonePriorItem struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// DoneResult answers MarkDone and RestoreDone. Problem "" (Outcomes has one entry per distinct id in the order
// given), "invalid" or "unavailable" (Outcomes and Prior empty). Prior lists exactly the changed assignments and
// PriorDone is the state before the call: «Отменить» is RestoreDone(PriorDone, Prior), offered only when Prior is not
// empty.
type DoneResult struct {
	Problem   string            `json:"problem"`
	Outcomes  []DoneOutcomeItem `json:"outcomes"`
	Prior     []DonePriorItem   `json:"prior"`
	PriorDone bool              `json:"priorDone"`
}

// MarkDone is «Выполнено» (done) or «Вернуть в работу» (not done) of 1..homeworkflow.MaxMarkDone assignments
// (ADR-0033 §9). It never returns an error: any failure is a problem code or the code of one assignment. A malformed
// id is "not_found" without reaching the core; an id given twice is reported once. The click is the student's own
// action: nothing is written to the agent journal.
func (s *HomeworkService) MarkDone(ctx context.Context, ids []string, done bool) DoneResult {
	if len(ids) == 0 || len(ids) > homeworkflow.MaxMarkDone {
		s.log.Info(opMarkDone, "result", doneInvalid, "ids", len(ids))
		return doneProblem(doneInvalid)
	}
	order, sent := []string{}, map[string]bool{}
	var pass []shared.AssignmentID
	for _, id := range ids {
		if slices.Contains(order, id) {
			continue
		}
		order = append(order, id)
		if aid, err := assignmentID(id); err == nil {
			pass, sent[id] = append(pass, aid), true
		}
	}
	var marks homeworkflow.DoneMarks
	if len(pass) > 0 {
		var err error
		if marks, err = s.proposals.MarkDone(ctx, pass, done); err != nil {
			return s.doneFailure(opMarkDone, err)
		}
	}
	return s.doneResult(opMarkDone, order, sent, marks, !done)
}

// RestoreDone is «Отменить» of MarkDone or of an earlier RestoreDone: priorDone and prior are exactly the PriorDone
// and Prior that call returned. Every assignment still at its version gets priorDone back; one changed meanwhile (also
// by a background reprocess) is "conflict" and stays as it is. An entry with a malformed id or a negative version is
// "not_found" without reaching the core; the first entry of an id wins. An empty prior or more than
// homeworkflow.MaxMarkDone entries is "invalid": the page offers no «Отменить» without a prior. The result undoes this
// call again.
func (s *HomeworkService) RestoreDone(ctx context.Context, priorDone bool, prior []DonePriorItem) DoneResult {
	if len(prior) == 0 || len(prior) > homeworkflow.MaxMarkDone {
		s.log.Info(opRestoreDone, "result", doneInvalid, "ids", len(prior))
		return doneProblem(doneInvalid)
	}
	order, sent := []string{}, map[string]bool{}
	var pass []homeworkflow.DonePrior
	for _, p := range prior {
		if slices.Contains(order, p.ID) {
			continue
		}
		order = append(order, p.ID)
		if aid, err := assignmentID(p.ID); err == nil && p.Version >= 0 {
			pass, sent[p.ID] = append(pass, homeworkflow.DonePrior{ID: aid, Done: priorDone, Version: p.Version}), true
		}
	}
	var marks homeworkflow.DoneMarks
	if len(pass) > 0 {
		var err error
		if marks, err = s.proposals.RestoreDone(ctx, pass); err != nil {
			return s.doneFailure(opRestoreDone, err)
		}
	}
	return s.doneResult(opRestoreDone, order, sent, marks, !priorDone)
}

// doneResult reports every id of order: the core's code for one sent to it ("failed" if the core left it out or
// gave an unknown code), "not_found" for a malformed one. Prior lists exactly the outcomes "changed".
func (s *HomeworkService) doneResult(op string, order []string, sent map[string]bool, marks homeworkflow.DoneMarks,
	priorDone bool) DoneResult {
	byID := make(map[string]homeworkflow.DoneOutcome, len(marks.Outcomes))
	for _, o := range marks.Outcomes {
		byID[string(o.ID)] = o
	}
	out := DoneResult{Outcomes: make([]DoneOutcomeItem, 0, len(order)), Prior: []DonePriorItem{}, PriorDone: priorDone}
	counts := map[homeworkflow.DoneCode]int{}
	for _, id := range order {
		item := DoneOutcomeItem{ID: id, Code: string(homeworkflow.DoneNotFound)}
		if sent[id] {
			item.Code = string(homeworkflow.DoneFailed)
			if o, ok := byID[id]; ok && knownDoneCodes[o.Code] {
				item.Code = string(o.Code)
				if o.Code == homeworkflow.DoneChanged || o.Code == homeworkflow.DoneUnchanged {
					item.Done, item.Version = o.Done, o.Version
				}
				if o.Code == homeworkflow.DoneFailed {
					s.log.Error(op, "err", o.Err)
				}
			}
		}
		counts[homeworkflow.DoneCode(item.Code)]++
		out.Outcomes = append(out.Outcomes, item)
		// The prior of a changed one is the version it saved (homeworkflow.DoneMarks.Prior): one list to trust.
		if item.Code == string(homeworkflow.DoneChanged) {
			out.Prior = append(out.Prior, DonePriorItem{ID: id, Version: item.Version})
		}
	}
	s.log.Info(op, "changed", counts[homeworkflow.DoneChanged], "unchanged", counts[homeworkflow.DoneUnchanged],
		"not_found", counts[homeworkflow.DoneNotFound], "deleted", counts[homeworkflow.DoneDeleted],
		"conflict", counts[homeworkflow.DoneConflict], "failed", counts[homeworkflow.DoneFailed])
	return out
}

// doneFailure maps a refused request to "invalid" and anything else to "unavailable"; the cause goes to the log.
func (s *HomeworkService) doneFailure(op string, err error) DoneResult {
	if errors.Is(err, homeworkflow.ErrInvalidRequest) {
		s.log.Warn(op, "result", doneInvalid, "err", err)
		return doneProblem(doneInvalid)
	}
	s.log.Error(op, "err", err)
	return doneProblem(doneUnavailable)
}

func doneProblem(problem string) DoneResult {
	return DoneResult{Problem: problem, Outcomes: []DoneOutcomeItem{}, Prior: []DonePriorItem{}}
}
