package agent

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"time"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Library is the library (cases/library) as the action tools and their Undo use it.
type Library interface {
	SubjectLibrary
	Material(ctx context.Context, id shared.MaterialID) (*material.Material, error)
	CreateSubject(ctx context.Context, req library.NewSubject) (*subject.Subject, error)
	CreateFolder(ctx context.Context, id shared.SubjectID, name string) (subject.Folder, error)
	Place(ctx context.Context, id shared.MaterialID, sub shared.SubjectID, folder shared.FolderID) (*material.Material, error)
	Link(ctx context.Context, id shared.MaterialID, lesson shared.LessonID) (*material.Material, error)
	RemoveSubject(ctx context.Context, id shared.SubjectID) error
	RemoveFolder(ctx context.Context, id shared.SubjectID, folder shared.FolderID) error
	MoveToInbox(ctx context.Context, id shared.MaterialID) (*material.Material, error)
	Unlink(ctx context.Context, id shared.MaterialID, lesson shared.LessonID) (*material.Material, error)
}

// LessonInfo is the knowledge base (cases/knowledgebase): the lesson of a recording and the subject and kind the
// student (or the agent) set for it.
type LessonInfo interface {
	Lesson(ctx context.Context, id shared.RecordingID) (kb.Lesson, error)
	SetLessonInfo(ctx context.Context, id shared.RecordingID, subject, kind string) (kb.Lesson, error)
}

// AssignmentEdits reads and edits an assignment (cases/homeworkflow).
type AssignmentEdits interface {
	Assignment(ctx context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error)
	EditAssignment(ctx context.Context, e homeworkflow.AssignmentEdit) (homeworkflow.AssignmentView, error)
}

// Rerunner starts the post-processing of a recording again (cases/homeworkflow); created is false when the job of an
// earlier start was found instead.
type Rerunner interface {
	Retry(ctx context.Context, rec shared.RecordingID, work homeworkflow.Work) (id shared.JobID, created bool, err error)
	RedoSummary(ctx context.Context, rec shared.RecordingID) (id shared.JobID, created bool, err error)
}

// MaterialWork starts the summary or the homework search of a library material (cases/homeworkflow, TASK-122);
// created is false when a job of the same key was found instead.
type MaterialWork interface {
	Start(ctx context.Context, id shared.MaterialID, work homeworkflow.Work) (shared.JobID, bool, error)
}

// Drafts generates homework drafts (cases/homeworkflow).
type Drafts interface {
	ActiveGeneration(ctx context.Context, a shared.AssignmentID) (homeworkflow.GenerationView, bool, error)
	Generate(ctx context.Context, a shared.AssignmentID, instruction string) (homeworkflow.GenerateResult, error)
}

// JobCanceller cancels a job at the student's request (cases/jobrunner): the Undo of a paid action that still runs.
type JobCanceller interface {
	Cancel(ctx context.Context, id shared.JobID) error
}

// Tx runs fn in one write transaction of the local database: the check of a target's version and the change it guards
// commit together, so a change saved in between is never overwritten.
type Tx interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ActionDeps are the dependencies of Actions; every one except Materials, Turnkey and Log is required. Without
// Materials the tools on a material answer "unavailable", without Turnkey triage_assignment does.
type ActionDeps struct {
	Library     Library
	Lessons     LessonInfo
	Assignments AssignmentEdits
	Rerunner    Rerunner
	Materials   MaterialWork
	Turnkey     Turnkey
	Drafts      Drafts
	Jobs        Activity
	Canceller   JobCanceller
	Tx          Tx
	Clock       Clock
	Log         *slog.Logger
}

// Actions runs the reversible actions of the agent (class U, ADR-0021) on the use cases and is their Undo: it
// implements the agentjournal Compensator and Resolver ports (app wires it into the journal). It reports facts (the
// version and state of a target); the domain decides (action.PlanUndo, action.Settle).
type Actions struct {
	library     Library
	lessons     LessonInfo
	assignments AssignmentEdits
	rerunner    Rerunner
	materials   MaterialWork
	turnkey     Turnkey
	drafts      Drafts
	jobs        Activity
	canceller   JobCanceller
	tx          Tx
	clock       Clock
	log         *slog.Logger
}

// NewActions checks the dependencies.
func NewActions(d ActionDeps) (*Actions, error) {
	if d.Library == nil || d.Lessons == nil || d.Assignments == nil || d.Rerunner == nil || d.Drafts == nil ||
		d.Jobs == nil || d.Canceller == nil || d.Tx == nil || d.Clock == nil {
		return nil, fmt.Errorf("agent: actions: %w: every dependency except Log is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Actions{
		library: d.Library, lessons: d.Lessons, assignments: d.Assignments, rerunner: d.Rerunner, materials: d.Materials,
		turnkey: d.Turnkey, drafts: d.Drafts,
		jobs: d.Jobs, canceller: d.Canceller, tx: d.Tx, clock: d.Clock, log: log,
	}, nil
}

// opUndoJob names the log line of a job the Undo could not cancel.
const opUndoJob = "agent: undo: job not cancelled"

// jobVersion is the version of a job as a target: the student never edits a job, so a running one is always the
// one the action started; whether it finished is Target.Final.
const jobVersion = 1

// errMovedOn means the target changed between the look before the action and the action: nothing is done.
var errMovedOn = errors.New("agent: the target changed before the action")

// --- the actions --------------------------------------------------------------------------------------

// prepared is what an action knows before it runs: the intent the journal records first and, when the state is
// already the wanted one, the result without any change (nothing is journaled then).
type prepared struct {
	intent action.Intent
	noop   *values.WriteRow
	run    func(ctx context.Context) (effect, error)
}

// effect is what an action did: its compensation, the target's version right after it and what the planner sees.
type effect struct {
	comp    action.Compensation
	version int64
	row     values.WriteRow
	created values.Ref // the subject or folder a create made
	failed  bool       // the use case changed nothing (a job or a work that existed already)
	// triage is what triage_assignment shows the planner instead of row; cards are the cards it adds.
	triage *values.TriageRow
	cards  []values.Card
}

// prepare looks at the target of call and returns how to run it; ok is false for a tool this desktop does not run.
func (a *Actions) prepare(ctx context.Context, call values.Call) (prepared, bool, error) {
	var (
		p   prepared
		err error
	)
	switch call.Tool() {
	case values.ToolCreateSubject:
		p, err = a.createSubject(call)
	case values.ToolCreateFolder:
		p, err = a.createFolder(ctx, call)
	case values.ToolPlaceUpload:
		p, err = a.placeUpload(ctx, call)
	case values.ToolLinkMaterial:
		p, err = a.linkMaterial(ctx, call)
	case values.ToolSetLessonInfo:
		p, err = a.setLessonInfo(ctx, call)
	case values.ToolEditDue:
		p, err = a.editDue(ctx, call)
	case values.ToolMakeSummary, values.ToolMakeLectureText, values.ToolFindHomework:
		p, err = a.postProcess(ctx, call)
	case values.ToolGenerateDraft:
		p, err = a.generateDraft(ctx, call)
	case values.ToolTriageAssignment:
		p, err = a.triage(call)
	default:
		return prepared{}, false, nil
	}
	if errors.Is(err, errUnavailable) {
		return prepared{}, false, nil
	}
	return p, true, err
}

// errUnavailable marks a variant of a tool that has no use case wired (the results of a material without Materials).
var errUnavailable = errors.New("agent: no use case for this variant yet")

func (a *Actions) createSubject(call values.Call) (prepared, error) {
	name, _ := call.Args().Name("name")
	semester, _ := call.Args().Name("semester")
	return prepared{
		intent: action.Intent{Name: string(name)},
		run: func(ctx context.Context) (effect, error) {
			sub, err := a.library.CreateSubject(ctx, library.NewSubject{Name: string(name), Semester: string(semester)})
			if err != nil {
				return effect{}, err
			}
			r, err := values.NewRef(values.KindSubject, string(sub.ID()))
			if err != nil {
				return effect{}, err
			}
			return effect{
				comp: action.RemoveSubject{Subject: r}, version: sub.Version(), row: values.WriteRow{ID: r, Changed: true}, created: r,
			}, nil
		},
	}, nil
}

func (a *Actions) createFolder(ctx context.Context, call values.Call) (prepared, error) {
	subRef, _ := call.Args().ID("subject")
	name, _ := call.Args().Name("name")
	sub, err := a.subject(ctx, shared.SubjectID(subRef.ID()))
	if err != nil {
		return prepared{}, err
	}
	before := sub.Version()
	return prepared{
		intent: action.Intent{Target: subRef, Before: before, Name: string(name)},
		run: func(ctx context.Context) (effect, error) {
			var e effect
			err := a.tx.Do(ctx, func(ctx context.Context) error {
				// Rebase trusts Before: a subject changed since then must not be built on.
				now, err := a.subject(ctx, sub.ID())
				if err != nil {
					return err
				}
				if now.Version() != before {
					return errMovedOn
				}
				f, err := a.library.CreateFolder(ctx, sub.ID(), string(name))
				if err != nil {
					return err
				}
				after, err := a.subject(ctx, sub.ID())
				if err != nil {
					return err
				}
				r, err := values.NewRef(values.KindFolder, string(f.ID()))
				if err != nil {
					return err
				}
				e = effect{
					comp: action.RemoveFolder{Subject: subRef, Folder: r}, version: after.Version(),
					row: values.WriteRow{ID: r, Changed: true}, created: r,
				}
				return nil
			})
			return e, err
		},
	}, nil
}

// placeUpload files an upload of «Входящие» in a folder of a subject, or in its «Материалы» linked to a lesson. Only
// an upload in «Входящие» is placed, so its Undo (back to «Входящие») restores what was.
func (a *Actions) placeUpload(ctx context.Context, call values.Call) (prepared, error) {
	args := call.Args()
	up, _ := args.ID("upload")
	subRef, _ := args.ID("subject")
	folderRef, _ := args.ID("folder")
	lessonRef, _ := args.ID("lesson")
	id, sub, lesson := shared.MaterialID(up.ID()), shared.SubjectID(subRef.ID()), shared.LessonID(lessonRef.ID())
	folder := shared.FolderID(folderRef.ID())
	if folder == "" {
		folder = subject.SystemFolderID(sub, subject.SystemMaterials)
	}
	m, err := a.library.Material(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	linked := !lessonRef.IsZero() && slices.Contains(m.Lessons(), lesson)
	if m.Subject() == sub && m.Folder() == folder && (lessonRef.IsZero() || linked) {
		return prepared{noop: &values.WriteRow{ID: materialRef(m)}}, nil
	}
	if !m.InInbox() {
		return prepared{}, fmt.Errorf("%w: material %s is not in «Входящие»", library.ErrInvalidRequest, id)
	}
	undo := action.ReturnToInbox{Material: materialRef(m)}
	if !lessonRef.IsZero() && !linked {
		undo.Lesson = lessonRef
	}
	before := m.Version()
	return prepared{
		intent: action.Intent{Target: undo.Material, Before: before, Prior: undo},
		run: func(ctx context.Context) (effect, error) {
			return a.changeMaterial(ctx, id, before, undo, func(ctx context.Context) (*material.Material, error) {
				placed, err := a.library.Place(ctx, id, sub, shared.FolderID(folderRef.ID()))
				if err != nil || lessonRef.IsZero() {
					return placed, err
				}
				return a.library.Link(ctx, id, lesson)
			})
		},
	}, nil
}

func (a *Actions) linkMaterial(ctx context.Context, call values.Call) (prepared, error) {
	mRef, _ := call.Args().ID("material")
	lessonRef, _ := call.Args().ID("lesson")
	id, lesson := shared.MaterialID(mRef.ID()), shared.LessonID(lessonRef.ID())
	m, err := a.library.Material(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	if slices.Contains(m.Lessons(), lesson) {
		return prepared{noop: &values.WriteRow{}}, nil
	}
	undo := action.UnlinkMaterial{Material: mRef, Lesson: lessonRef}
	before := m.Version()
	return prepared{
		intent: action.Intent{Target: mRef, Before: before, Prior: undo},
		run: func(ctx context.Context) (effect, error) {
			e, err := a.changeMaterial(ctx, id, before, undo, func(ctx context.Context) (*material.Material, error) {
				return a.library.Link(ctx, id, lesson)
			})
			e.row = values.WriteRow{Changed: true}
			return e, err
		},
	}, nil
}

// changeMaterial runs change in one transaction with the check that the material is still at version before.
func (a *Actions) changeMaterial(ctx context.Context, id shared.MaterialID, before int64, undo action.Compensation,
	change func(ctx context.Context) (*material.Material, error)) (effect, error) {
	var e effect
	err := a.tx.Do(ctx, func(ctx context.Context) error {
		m, err := a.library.Material(ctx, id)
		if err != nil {
			return err
		}
		if m.Version() != before {
			return errMovedOn
		}
		if m, err = change(ctx); err != nil {
			return err
		}
		e = effect{comp: undo, version: m.Version(), row: values.WriteRow{ID: materialRef(m), Changed: true}}
		return nil
	})
	return e, err
}

func (a *Actions) setLessonInfo(ctx context.Context, call values.Call) (prepared, error) {
	args := call.Args()
	recRef, _ := args.ID("recording")
	subRef, _ := args.ID("subject")
	kind, _ := args.Enum("kind")
	rec := shared.RecordingID(recRef.ID())
	sub, err := a.subject(ctx, shared.SubjectID(subRef.ID()))
	if err != nil {
		return prepared{}, err
	}
	name := sub.Name().String()
	l, err := a.lessons.Lesson(ctx, rec)
	if err != nil {
		return prepared{}, err
	}
	oldSubject, oldKind := correctionOf(l)
	if oldSubject == name && oldKind == kind {
		return prepared{noop: &values.WriteRow{}}, nil
	}
	undo := action.RestoreLessonInfo{Recording: recRef, OldSubject: oldSubject, OldKind: oldKind}
	next := action.RestoreLessonInfo{Recording: recRef, OldSubject: name, OldKind: kind}
	before := lessonVersion(l)
	return prepared{
		intent: action.Intent{Target: recRef, Before: before, Prior: undo, Next: next},
		run: func(ctx context.Context) (effect, error) {
			var e effect
			err := a.tx.Do(ctx, func(ctx context.Context) error {
				l, err := a.lessons.Lesson(ctx, rec)
				if err != nil {
					return err
				}
				if lessonVersion(l) != before {
					return errMovedOn
				}
				if l, err = a.lessons.SetLessonInfo(ctx, rec, name, kind); err != nil {
					return err
				}
				e = effect{comp: undo, version: lessonVersion(l), row: values.WriteRow{Changed: true}}
				return nil
			})
			return e, err
		},
	}, nil
}

// editDue sets the deadline of an assignment to a day; the time of day stays what it was (23:59 for a new one) in the
// student's zone. Undo restores the old deadline exactly (D7).
func (a *Actions) editDue(ctx context.Context, call values.Call) (prepared, error) {
	aRef, _ := call.Args().ID("assignment")
	day, _ := call.Args().Date("due")
	id := shared.AssignmentID(aRef.ID())
	v, err := a.assignments.Assignment(ctx, id)
	if err != nil {
		return prepared{}, err
	}
	loc := a.clock.Now().Location()
	due := dueOn(day, v.Due, loc)
	if !v.Due.IsZero() && dateOf(v.Due, loc) == day {
		return prepared{noop: &values.WriteRow{}}, nil
	}
	undo := action.RestoreDue{Assignment: aRef, OldDue: v.Due}
	before := v.Version
	return prepared{
		intent: action.Intent{Target: aRef, Before: before, Prior: undo, Next: action.RestoreDue{Assignment: aRef, OldDue: due}},
		run: func(ctx context.Context) (effect, error) {
			var e effect
			err := a.tx.Do(ctx, func(ctx context.Context) error {
				after, err := a.setDue(ctx, id, before, due)
				e = effect{comp: undo, version: after, row: values.WriteRow{Changed: true}}
				return err
			})
			return e, err
		},
	}, nil
}

// setDue changes only the deadline of an assignment still at version before and returns its new version.
func (a *Actions) setDue(ctx context.Context, id shared.AssignmentID, before int64, due time.Time) (int64, error) {
	v, err := a.assignments.Assignment(ctx, id)
	if err != nil {
		return 0, err
	}
	if v.Version != before {
		return 0, errMovedOn
	}
	v, err = a.assignments.EditAssignment(ctx, homeworkflow.AssignmentEdit{
		ID: id, Title: v.Title, Description: v.Description, Due: due,
	})
	if err != nil {
		return 0, err
	}
	return v.Version, nil
}

// dueOn is the deadline on day at the time of day of old in loc, or 23:59 without an old one.
func dueOn(day values.Date, old time.Time, loc *time.Location) time.Time {
	h, m := 23, 59
	if !old.IsZero() {
		o := old.In(loc)
		h, m = o.Hour(), o.Minute()
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
}

// postProcess starts the summary, lecture text or homework search of a recording again: a ready summary is made anew
// (its replacement needed a plan), else the failed or missing job is started. On a material (TASK-122) it starts the
// material's summary or homework search, a new job after an earlier one (replacing a ready summary needed a plan).
// The paid job has no intent: Recover cannot tell whether it started (UNKNOWN).
func (a *Actions) postProcess(ctx context.Context, call values.Call) (prepared, error) {
	if matRef, ok := call.Args().ID("material"); ok {
		return a.materialWork(call.Tool(), shared.MaterialID(matRef.ID()))
	}
	recRef, ok := call.Args().ID("recording")
	if !ok {
		return prepared{}, errUnavailable
	}
	rec := shared.RecordingID(recRef.ID())
	start := func(ctx context.Context) (shared.JobID, bool, error) {
		switch call.Tool() {
		case values.ToolMakeLectureText:
			return a.rerunner.Retry(ctx, rec, homeworkflow.WorkLectureText)
		case values.ToolFindHomework:
			return a.rerunner.Retry(ctx, rec, homeworkflow.WorkHomework)
		}
		l, err := a.lessons.Lesson(ctx, rec)
		if err != nil {
			return "", false, err
		}
		if l.Ready.Summary {
			return a.rerunner.RedoSummary(ctx, rec)
		}
		return a.rerunner.Retry(ctx, rec, homeworkflow.WorkSummary)
	}
	return prepared{run: func(ctx context.Context) (effect, error) {
		id, created, err := start(ctx)
		if err != nil {
			return effect{}, err
		}
		return jobEffect(id, created)
	}}, nil
}

// materialWork starts the summary (make_summary) or the homework search (find_homework) of a material. Its Undo is
// that of every paid job: cancel while it runs, refused once the result is ready.
func (a *Actions) materialWork(tool values.Tool, id shared.MaterialID) (prepared, error) {
	work := homeworkflow.WorkSummary
	switch tool {
	case values.ToolMakeSummary:
	case values.ToolFindHomework:
		work = homeworkflow.WorkHomework
	default:
		return prepared{}, errUnavailable
	}
	if a.materials == nil {
		return prepared{}, errUnavailable
	}
	return prepared{run: func(ctx context.Context) (effect, error) {
		job, created, err := a.materials.Start(ctx, id, work)
		if err != nil {
			return effect{}, err
		}
		return jobEffect(job, created)
	}}, nil
}

func (a *Actions) generateDraft(ctx context.Context, call values.Call) (prepared, error) {
	aRef, _ := call.Args().ID("assignment")
	instruction, _ := call.Args().Label("instruction")
	id := shared.AssignmentID(aRef.ID())
	if g, ok, err := a.drafts.ActiveGeneration(ctx, id); err != nil {
		return prepared{}, err
	} else if ok {
		// A generation runs already (the student's or an earlier one): it is not this action's to undo.
		j, err := values.NewRef(values.KindJob, string(g.JobID))
		if err != nil {
			return prepared{}, err
		}
		return prepared{noop: &values.WriteRow{Job: j}}, nil
	}
	return prepared{run: func(ctx context.Context) (effect, error) {
		res, err := a.drafts.Generate(ctx, id, string(instruction))
		if err != nil {
			return effect{}, err
		}
		return jobEffect(res.JobID, res.Created)
	}}, nil
}

// jobEffect is the effect of a paid job; a job that existed already (created false) is not this action's.
func jobEffect(id shared.JobID, created bool) (effect, error) {
	j, err := values.NewRef(values.KindJob, string(id))
	if err != nil {
		return effect{}, err
	}
	return effect{
		comp: action.CancelJob{Job: j}, version: jobVersion, row: values.WriteRow{Job: j, Changed: created}, failed: !created,
	}, nil
}

// --- Undo: the Compensator of the journal ------------------------------------------------------------------

// Check reads the state of the target of a compensation (agentjournal.Compensator).
func (a *Actions) Check(ctx context.Context, c action.Compensation) (action.Target, error) {
	switch v := c.(type) {
	case action.RemoveSubject:
		sub, err := a.subject(ctx, shared.SubjectID(v.Subject.ID()))
		if err != nil {
			return action.Target{}, gone(err)
		}
		return action.Target{Version: sub.Version()}, nil
	case action.RemoveFolder:
		sub, err := a.subject(ctx, shared.SubjectID(v.Subject.ID()))
		if err != nil {
			return action.Target{}, gone(err)
		}
		_, has := sub.Folder(shared.FolderID(v.Folder.ID()))
		return action.Target{Version: sub.Version(), Compensated: !has}, nil
	case action.ReturnToInbox:
		m, err := a.library.Material(ctx, shared.MaterialID(v.Material.ID()))
		if err != nil {
			return action.Target{}, gone(err)
		}
		linked := !v.Lesson.IsZero() && slices.Contains(m.Lessons(), shared.LessonID(v.Lesson.ID()))
		return action.Target{Version: m.Version(), Compensated: m.InInbox() && !linked}, nil
	case action.UnlinkMaterial:
		m, err := a.library.Material(ctx, shared.MaterialID(v.Material.ID()))
		if err != nil {
			return action.Target{}, gone(err)
		}
		linked := slices.Contains(m.Lessons(), shared.LessonID(v.Lesson.ID()))
		return action.Target{Version: m.Version(), Compensated: !linked}, nil
	case action.RestoreLessonInfo:
		l, err := a.lessons.Lesson(ctx, shared.RecordingID(v.Recording.ID()))
		if err != nil {
			return action.Target{}, gone(err)
		}
		s, k := correctionOf(l)
		return action.Target{Version: lessonVersion(l), Compensated: s == v.OldSubject && k == v.OldKind}, nil
	case action.RestoreDue:
		av, err := a.assignments.Assignment(ctx, shared.AssignmentID(v.Assignment.ID()))
		if err != nil {
			return action.Target{}, gone(err)
		}
		return action.Target{Version: av.Version, Compensated: av.Due.Equal(v.OldDue)}, nil
	case action.CancelJob:
		st, err := a.jobState(ctx, shared.JobID(v.Job.ID()))
		if err != nil {
			return action.Target{}, err
		}
		return action.Target{
			Version: jobVersion, Compensated: st == job.StatusCancelled || st == job.StatusFailed, Final: st == job.StatusCompleted,
		}, nil
	case action.RemoveWork:
		return a.checkWork(ctx, v)
	}
	return action.Target{}, fmt.Errorf("%w: %T", action.ErrInvalidCompensation, c)
}

// Apply applies a compensation if its target is still at expectedVersion, in one transaction with that check
// (agentjournal.Compensator), and returns the target's version after it (0 when it is gone or was already
// compensated, so nothing rebases on it). A job is cancelled
// through the job runner: a finished one is action.ErrResultReady.
func (a *Actions) Apply(ctx context.Context, c action.Compensation, expectedVersion int64) (int64, error) {
	if v, ok := c.(action.CancelJob); ok {
		if err := a.cancelJob(ctx, shared.JobID(v.Job.ID())); err != nil {
			return 0, err
		}
		return jobVersion, nil
	}
	if v, ok := c.(action.RemoveWork); ok {
		// The repository removes the work under its version itself; nothing is left to rebase on.
		return 0, a.removeWork(ctx, v, expectedVersion)
	}
	var after int64
	err := a.tx.Do(ctx, func(ctx context.Context) error {
		t, err := a.Check(ctx, c)
		if err != nil {
			return err
		}
		if t.Compensated {
			return nil // nothing applied: after stays 0, so no earlier action rebases on it
		}
		if t.Version != expectedVersion {
			return action.ErrTargetChanged
		}
		if err := changedIfConflict(a.compensate(ctx, c)); err != nil {
			return err
		}
		if t, err = a.Check(ctx, c); err == nil {
			after = t.Version
		} else if !errors.Is(err, shared.ErrNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return after, nil
}

func (a *Actions) compensate(ctx context.Context, c action.Compensation) error {
	switch v := c.(type) {
	case action.RemoveSubject:
		return a.library.RemoveSubject(ctx, shared.SubjectID(v.Subject.ID()))
	case action.RemoveFolder:
		return a.library.RemoveFolder(ctx, shared.SubjectID(v.Subject.ID()), shared.FolderID(v.Folder.ID()))
	case action.ReturnToInbox:
		id := shared.MaterialID(v.Material.ID())
		if _, err := a.library.MoveToInbox(ctx, id); err != nil {
			return err
		}
		if v.Lesson.IsZero() {
			return nil
		}
		_, err := a.library.Unlink(ctx, id, shared.LessonID(v.Lesson.ID()))
		return err
	case action.UnlinkMaterial:
		_, err := a.library.Unlink(ctx, shared.MaterialID(v.Material.ID()), shared.LessonID(v.Lesson.ID()))
		return err
	case action.RestoreLessonInfo:
		_, err := a.lessons.SetLessonInfo(ctx, shared.RecordingID(v.Recording.ID()), v.OldSubject, v.OldKind)
		return err
	case action.RestoreDue:
		id := shared.AssignmentID(v.Assignment.ID())
		av, err := a.assignments.Assignment(ctx, id)
		if err != nil {
			return err
		}
		_, err = a.setDue(ctx, id, av.Version, v.OldDue)
		return err
	}
	return fmt.Errorf("%w: %T", action.ErrInvalidCompensation, c)
}

// changedIfConflict reads a refusal of the use case that means the target is not as the action left it (a file was
// filed under a created subject, the material was removed) as a changed target.
func changedIfConflict(err error) error {
	switch {
	case errors.Is(err, library.ErrNotEmpty), errors.Is(err, library.ErrMaterialRemoved), errors.Is(err, errMovedOn):
		return fmt.Errorf("%w: %w", action.ErrTargetChanged, err)
	case errors.Is(err, library.ErrSubjectNotFound), errors.Is(err, library.ErrFolderNotFound):
		return nil // gone meanwhile: nothing left to undo
	}
	return err
}

func (a *Actions) cancelJob(ctx context.Context, id shared.JobID) error {
	err := a.canceller.Cancel(ctx, id)
	if err == nil {
		return nil
	}
	a.log.InfoContext(ctx, opUndoJob, "job", id.String(), "err", err)
	st, serr := a.jobState(ctx, id)
	switch {
	case serr != nil:
		return errors.Join(err, serr)
	case st == job.StatusCompleted:
		return fmt.Errorf("%w: %w", action.ErrResultReady, err)
	case st == job.StatusCancelled || st == job.StatusFailed:
		return nil
	}
	return fmt.Errorf("%w: %w", action.ErrCannotStop, err)
}

func (a *Actions) jobState(ctx context.Context, id shared.JobID) (job.Status, error) {
	states, err := a.jobs.JobStates(ctx, []shared.JobID{id})
	if err != nil {
		return "", err
	}
	for _, s := range states {
		if s.ID == id {
			return s.Status, nil
		}
	}
	return "", fmt.Errorf("job %s: %w", id, shared.ErrNotFound)
}

// --- Recover: the Resolver of the journal ---------------------------------------------------------------

// Observe tells what happened to the target of a PENDING entry after a crash (agentjournal.Resolver): its version
// now and whether the effect of the action is there. A paid job cannot be told (its key is the use case's own).
func (a *Actions) Observe(ctx context.Context, tool values.Tool, in action.Intent, since time.Time) (action.Observation, error) {
	switch tool {
	case values.ToolCreateSubject:
		return a.observeSubject(ctx, in, since)
	case values.ToolCreateFolder:
		sub, err := a.subject(ctx, shared.SubjectID(in.Target.ID()))
		if err != nil {
			return absent(err)
		}
		fn, err := subject.NewFolderName(in.Name)
		if err != nil {
			return action.Observation{Known: true, Version: sub.Version()}, nil
		}
		for _, f := range sub.Folders() {
			if f.Name().Key() == fn.Key() && sub.Version() != in.Before {
				r, err := values.NewRef(values.KindFolder, string(f.ID()))
				if err != nil {
					return action.Observation{}, err
				}
				return action.Observation{Known: true, Version: sub.Version(), Effect: true,
					Compensation: action.RemoveFolder{Subject: in.Target, Folder: r}}, nil
			}
		}
		return action.Observation{Known: true, Version: sub.Version()}, nil
	case values.ToolPlaceUpload, values.ToolLinkMaterial:
		m, err := a.library.Material(ctx, shared.MaterialID(in.Target.ID()))
		if err != nil {
			return absent(err)
		}
		effect := !m.InInbox()
		if u, ok := in.Prior.(action.UnlinkMaterial); ok {
			effect = slices.Contains(m.Lessons(), shared.LessonID(u.Lesson.ID()))
		}
		return observed(m.Version(), in, effect), nil
	case values.ToolSetLessonInfo:
		l, err := a.lessons.Lesson(ctx, shared.RecordingID(in.Target.ID()))
		if err != nil {
			return absent(err)
		}
		// Only the values the action set are its effect; without them (or with other ones) the outcome is unknown.
		next, ok := in.Next.(action.RestoreLessonInfo)
		s, k := correctionOf(l)
		return observed(lessonVersion(l), in, ok && s == next.OldSubject && k == next.OldKind), nil
	case values.ToolEditDue:
		v, err := a.assignments.Assignment(ctx, shared.AssignmentID(in.Target.ID()))
		if err != nil {
			return absent(err)
		}
		next, ok := in.Next.(action.RestoreDue)
		return observed(v.Version, in, ok && !next.OldDue.IsZero() && v.Due.Equal(next.OldDue)), nil
	}
	return action.Observation{}, nil
}

// observed is the observation of a target that changes in place: the effect is there when the version moved on
// from the one before and effect holds; its compensation is the prior one of the intent.
func observed(version int64, in action.Intent, effect bool) action.Observation {
	o := action.Observation{Known: true, Version: version}
	if effect && version != in.Before {
		o.Effect, o.Compensation = true, in.Prior
	}
	return o
}

// observeSubject finds the subject create_subject made: one of its name created at or after the entry.
func (a *Actions) observeSubject(ctx context.Context, in action.Intent, since time.Time) (action.Observation, error) {
	matches, err := a.library.ResolveSubjects(ctx, []string{in.Name})
	if err != nil {
		return action.Observation{}, err
	}
	m, ok := matches[in.Name]
	if !ok {
		return action.Observation{Known: true}, nil
	}
	sub, err := a.subject(ctx, m.ID)
	if err != nil {
		return absent(err)
	}
	if sub.CreatedAt().Before(since) {
		return action.Observation{Known: true}, nil // it existed before: the create was refused
	}
	r, err := values.NewRef(values.KindSubject, string(sub.ID()))
	if err != nil {
		return action.Observation{}, err
	}
	return action.Observation{
		Known: true, Version: sub.Version(), Effect: true, Compensation: action.RemoveSubject{Subject: r},
	}, nil
}

// absent is the observation of a target that is gone; other errors are returned (the entry waits for the next start).
func absent(err error) (action.Observation, error) {
	if errors.Is(gone(err), shared.ErrNotFound) {
		return action.Observation{Known: true}, nil
	}
	return action.Observation{}, err
}

// gone maps a "not found" of a use case to shared.ErrNotFound.
func gone(err error) error {
	switch {
	case errors.Is(err, library.ErrSubjectNotFound), errors.Is(err, library.ErrMaterialNotFound),
		errors.Is(err, knowledgebase.ErrRecordingNotFound), errors.Is(err, shared.ErrNotFound):
		return fmt.Errorf("%w: %w", shared.ErrNotFound, err)
	}
	return err
}

// --- helpers --------------------------------------------------------------------------------------------

// subject finds a subject of the library; a missing one is library.ErrSubjectNotFound.
func (a *Actions) subject(ctx context.Context, id shared.SubjectID) (*subject.Subject, error) {
	list, err := a.library.Subjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, s := range list {
		if s.ID() == id {
			return s, nil
		}
	}
	return nil, library.ErrSubjectNotFound
}

func materialRef(m *material.Material) values.Ref {
	r, _ := refOfID(values.KindMaterial, string(m.ID()))
	return r
}

// correctionOf is the subject and kind the student (or the agent) set for a lesson; empty when not set.
func correctionOf(l kb.Lesson) (subjectName, kind string) {
	if l.SubjectEdited {
		subjectName = l.Subject
	}
	if l.KindEdited {
		kind = l.Kind
	}
	return subjectName, kind
}

// lessonVersion is a version of the correction of a lesson: a digest of it (the recording's own version is not
// exposed), at least 1. Changing the correction and back gives the same version, which is the same state.
func lessonVersion(l kb.Lesson) int64 {
	s, k := correctionOf(l)
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%t\x00%s\x00%t\x00%s", l.SubjectEdited, s, l.KindEdited, k)
	return int64(h.Sum64()>>2) + 1
}
