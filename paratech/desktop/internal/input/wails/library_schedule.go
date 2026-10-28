package wails

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/services/schedulesubjects"
)

// Subjects from the schedule (TASK-311, ADR-0033 §4, §7). Schedule strings are untrusted text (ADR-0022): they go
// back to the page as labels only (plain text) and never into a log record or an error; the log holds counts and
// problem codes.

// Operation names of the log.
const (
	opScheduleSubjectsList   = "ui: subjects from the schedule"
	opScheduleSubjectsCreate = "ui: create subjects from the schedule"
	opScheduleSubjectsUndo   = "ui: undo subjects from the schedule"
)

// Problems of the schedule calls; "" is success.
const (
	ScheduleProblemStale       = "stale"       // the schedule or the subjects changed: ask ScheduleSubjects again
	ScheduleProblemInvalid     = "invalid"     // a bad token, choice or name, or an Undo of a token this run did not create
	ScheduleProblemNameTaken   = "name_taken"  // a new (renamed) name is a spelling of another subject or of the package
	ScheduleProblemBusy        = "busy"        // another create or undo of the schedule runs, or the data changed meanwhile
	ScheduleProblemUnavailable = "unavailable" // the library could not be read or written, or the call took too long
)

// Group and member actions of the page.
const (
	ScheduleActionCreate   = "create"
	ScheduleActionAliasOf  = "alias_of"
	ScheduleActionSkip     = "skip"
	ScheduleActionKeep     = "keep"
	ScheduleActionSeparate = "separate"
)

// Lesson kind codes of ScheduleKindCount; "seminar" counts seminars and practices.
const (
	ScheduleKindLecture = "lecture"
	ScheduleKindSeminar = "seminar"
	ScheduleKindLab     = "lab"
	ScheduleKindUnknown = "unknown"
)

// Reason codes of skipped rows (the first three), of kept subjects and of kept spellings.
const (
	ScheduleReasonNameTaken      = "name_taken"
	ScheduleReasonTooManyAliases = "too_many_aliases"
	ScheduleReasonSubjectGone    = "subject_gone"
	ScheduleReasonHasFiles       = "has_files"
	ScheduleReasonHasNotes       = "has_notes"
	ScheduleReasonStale          = "stale"
)

// Member link codes: why a member is in its group.
const (
	ScheduleLinkSame         = "same"
	ScheduleLinkAbbreviation = "abbreviation"
	ScheduleLinkTypo         = "typo"
)

const (
	// scheduleCallTimeout bounds a create or undo: the retry and undo search is ctx-bounded and takes seconds at worst,
	// so a hang ends as unavailable.
	scheduleCallTimeout = 30 * time.Second

	// scheduleJournalSize is how many tokens the binding remembers the outcome of for «Отменить всё».
	scheduleJournalSize = 8

	// maxScheduleChoices bounds the choices and the member choices of one group; the use case keeps 200 rows at most.
	maxScheduleChoices = 256
)

// LibraryScheduleUseCase is subjects from the schedule (ARCH-42); satisfied by library.Service, so NewLibraryService
// attaches it when the library use case has it.
type LibraryScheduleUseCase interface {
	ScheduleSubjects(ctx context.Context) (library.ScheduleProposal, error)
	CreateFromSchedule(ctx context.Context, token string, choices []schedulesubjects.Choice) (library.ScheduleResult, error)
	UndoFromSchedule(ctx context.Context, token string, res library.ScheduleResult) (library.ScheduleUndo, error)
}

var _ LibraryScheduleUseCase = (*library.Service)(nil)

// ScheduleKindCount is the lessons of one kind.
type ScheduleKindCount struct {
	Kind    string `json:"kind"`
	Lessons int    `json:"lessons"`
}

// ScheduleMember is an obvious group inside a proposed group. Doubtful: linked by an abbreviation or a typo (Link),
// the student may take it "separate" or "skip" it. Names and spellings are schedule text: plain text only.
type ScheduleMember struct {
	Key        string              `json:"key"`
	Name       string              `json:"name"`
	Spellings  []string            `json:"spellings"`
	Kinds      []ScheduleKindCount `json:"kinds"`
	Lessons    int                 `json:"lessons"`
	Recordings int                 `json:"recordings"`
	Doubtful   bool                `json:"doubtful"`
	Link       string              `json:"link"`
}

// ScheduleGroup is a proposed subject. Action is the default: "create" a new subject Name, or "alias_of" the existing
// subject SubjectID (SubjectName; the spellings become its aliases). Kinds lists the non-zero kinds in a fixed order.
type ScheduleGroup struct {
	Key         string              `json:"key"`
	Name        string              `json:"name"`
	Aliases     []string            `json:"aliases"`
	Kinds       []ScheduleKindCount `json:"kinds"`
	Lessons     int                 `json:"lessons"`
	Recordings  int                 `json:"recordings"`
	Doubtful    bool                `json:"doubtful"`
	Members     []ScheduleMember    `json:"members"`
	Action      string              `json:"action"`
	SubjectID   string              `json:"subjectId"`
	SubjectName string              `json:"subjectName"`
}

// ScheduleSubjectsView answers ScheduleSubjects. Token goes back to CreateFromSchedule and UndoFromSchedule. Omitted
// counts the least used strings left out. Problem is "" or "unavailable". No groups is not a problem.
type ScheduleSubjectsView struct {
	Token   string          `json:"token"`
	Groups  []ScheduleGroup `json:"groups"`
	Omitted int             `json:"omitted"`
	Problem string          `json:"problem"`
}

// ScheduleMemberChoice is the student's choice for a member: "keep" (merged into the group), "separate" (a doubtful
// member only, its own subject, renamed when Name is set) or "skip".
type ScheduleMemberChoice struct {
	Key    string `json:"key"`
	Action string `json:"action"`
	Name   string `json:"name"`
}

// ScheduleChoice is the student's choice for a group: "create" (renamed when Name is set), "alias_of" or "skip". A
// group or member without a choice takes its default.
type ScheduleChoice struct {
	Group   string                 `json:"group"`
	Action  string                 `json:"action"`
	Name    string                 `json:"name"`
	Members []ScheduleMemberChoice `json:"members"`
}

// ScheduleCreatedSubject is a subject the package created; Key is its group or member key.
type ScheduleCreatedSubject struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ScheduleAliases is spellings of a subject: added by the package, or removed again by the undo.
type ScheduleAliases struct {
	SubjectID string   `json:"subjectId"`
	Aliases   []string `json:"aliases"`
}

// ScheduleSkippedRow is a row not done: Key for a subject to create, SubjectID for spellings to add. Reason is
// "name_taken", "too_many_aliases" or "subject_gone".
type ScheduleSkippedRow struct {
	Key       string `json:"key"`
	SubjectID string `json:"subjectId"`
	Reason    string `json:"reason"`
}

// ScheduleCreateResult answers CreateFromSchedule. Problem is "", stale, invalid, name_taken, busy or unavailable;
// with a problem the lists still hold what was done before it, and UndoFromSchedule takes that back.
type ScheduleCreateResult struct {
	Created []ScheduleCreatedSubject `json:"created"`
	Aliased []ScheduleAliases        `json:"aliased"`
	Skipped []ScheduleSkippedRow     `json:"skipped"`
	Problem string                   `json:"problem"`
}

// ScheduleKeptSubject is a subject of the package the undo left; Reason is "has_files" or "has_notes".
type ScheduleKeptSubject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ScheduleKeptAliases is spellings the undo left; Reason is "stale" (the schedule or the subjects changed, so they are
// not proven to be of the package).
type ScheduleKeptAliases struct {
	SubjectID string   `json:"subjectId"`
	Aliases   []string `json:"aliases"`
	Reason    string   `json:"reason"`
}

// ScheduleUndoResult answers UndoFromSchedule. Problem is "", invalid, busy or unavailable; with a problem the lists
// hold what was undone before it.
type ScheduleUndoResult struct {
	Removed     []string              `json:"removed"`
	Kept        []ScheduleKeptSubject `json:"kept"`
	Unaliased   []ScheduleAliases     `json:"unaliased"`
	KeptAliases []ScheduleKeptAliases `json:"keptAliases"`
	Problem     string                `json:"problem"`
}

// scheduleJournal remembers the outcome of CreateFromSchedule per token, so the undo takes back what the core did,
// never a list from the page. In memory only: after a restart an undo is invalid.
type scheduleJournal struct {
	mu    sync.Mutex
	order []string // oldest first
	byTok map[string]library.ScheduleResult
}

// record keeps res for token unless it did nothing. A retry is merged into the earlier outcome, never replaces it: a
// retry that fails sooner than the first run returns less, and the undo must still take back all the token did.
func (j *scheduleJournal) record(token string, res library.ScheduleResult) {
	if len(res.Created) == 0 && len(res.Aliased) == 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.byTok == nil {
		j.byTok = map[string]library.ScheduleResult{}
	}
	for i, t := range j.order {
		if t == token {
			j.order = append(j.order[:i], j.order[i+1:]...)
			break
		}
	}
	j.order = append(j.order, token)
	j.byTok[token] = mergeScheduleResult(j.byTok[token], res)
	for len(j.order) > scheduleJournalSize {
		delete(j.byTok, j.order[0])
		j.order = j.order[1:]
	}
}

func (j *scheduleJournal) lookup(token string) (library.ScheduleResult, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	res, ok := j.byTok[token]
	return res, ok
}

// mergeScheduleResult is the union of two outcomes of one token: Created by ID (first-seen order, newer data for the
// same ID), Aliased per subject (existing spellings first, exact duplicates dropped), Skipped of the newer outcome.
func mergeScheduleResult(old, newer library.ScheduleResult) library.ScheduleResult {
	out := library.ScheduleResult{
		Created: slices.Clone(old.Created),
		Aliased: make([]library.ScheduleAliased, 0, len(old.Aliased)+len(newer.Aliased)),
		Skipped: slices.Clone(newer.Skipped),
	}

	createdAt := make(map[string]int, len(out.Created)+len(newer.Created))
	for i, c := range out.Created {
		createdAt[string(c.ID)] = i
	}
	for _, c := range newer.Created {
		if i, ok := createdAt[string(c.ID)]; ok {
			out.Created[i] = c
			continue
		}
		createdAt[string(c.ID)] = len(out.Created)
		out.Created = append(out.Created, c)
	}

	aliasedAt := map[string]int{}
	spellings := map[string]map[string]bool{}
	for _, a := range slices.Concat(old.Aliased, newer.Aliased) {
		subj := string(a.Subject)
		i, ok := aliasedAt[subj]
		if !ok {
			i = len(out.Aliased)
			aliasedAt[subj], spellings[subj] = i, map[string]bool{}
			out.Aliased = append(out.Aliased, library.ScheduleAliased{Subject: a.Subject, Aliases: []string{}})
		}
		for _, sp := range a.Aliases {
			if !spellings[subj][sp] {
				spellings[subj][sp] = true
				out.Aliased[i].Aliases = append(out.Aliased[i].Aliases, sp)
			}
		}
	}
	return out
}

// scheduleState is the part of LibraryService the schedule calls need. running admits one create or undo at a time
// without blocking: a second call answers busy at once, which is the page's busy signal. No other binding lock is
// held, so the rest of the library and ScheduleSubjects stay callable meanwhile.
type scheduleState struct {
	uc      LibraryScheduleUseCase
	running atomic.Bool
	journal scheduleJournal
}

// withSchedule sets the use case of subjects from the schedule.
func (s *LibraryService) withSchedule(uc LibraryScheduleUseCase) *LibraryService {
	s.schedule.uc = uc
	return s
}

// ScheduleSubjects proposes the subjects to create from the subject strings of the schedule and the recordings.
func (s *LibraryService) ScheduleSubjects(ctx context.Context) ScheduleSubjectsView {
	if s.schedule.uc == nil {
		s.log.Error(opScheduleSubjectsList, "problem", ScheduleProblemUnavailable, "cause", "not wired")
		return ScheduleSubjectsView{Groups: []ScheduleGroup{}, Problem: ScheduleProblemUnavailable}
	}
	p, err := s.schedule.uc.ScheduleSubjects(ctx)
	if err != nil {
		s.log.Warn(opScheduleSubjectsList, "problem", ScheduleProblemUnavailable)
		return ScheduleSubjectsView{Groups: []ScheduleGroup{}, Problem: ScheduleProblemUnavailable}
	}
	out := ScheduleSubjectsView{Token: p.Token, Groups: make([]ScheduleGroup, 0, len(p.Groups)), Omitted: p.Omitted}
	for _, g := range p.Groups {
		out.Groups = append(out.Groups, scheduleGroup(g))
	}
	s.log.Info(opScheduleSubjectsList, "groups", len(out.Groups), "omitted", out.Omitted)
	return out
}

// CreateFromSchedule applies the student's choices to the proposal of token (the one «Да»). It may take seconds; a
// second create or undo meanwhile answers busy.
func (s *LibraryService) CreateFromSchedule(ctx context.Context, token string, choices []ScheduleChoice) ScheduleCreateResult {
	empty, _ := scheduleCreateResult(library.ScheduleResult{})
	if s.schedule.uc == nil {
		s.log.Error(opScheduleSubjectsCreate, "problem", ScheduleProblemUnavailable, "cause", "not wired")
		empty.Problem = ScheduleProblemUnavailable
		return empty
	}
	cs, ok := scheduleChoices(choices)
	if !ok {
		s.log.Info(opScheduleSubjectsCreate, "problem", ScheduleProblemInvalid, "cause", "choice")
		empty.Problem = ScheduleProblemInvalid
		return empty
	}
	if !s.schedule.running.CompareAndSwap(false, true) {
		s.log.Info(opScheduleSubjectsCreate, "problem", ScheduleProblemBusy)
		empty.Problem = ScheduleProblemBusy
		return empty
	}
	defer s.schedule.running.Store(false)

	ctx, cancel := context.WithTimeout(ctx, scheduleCallTimeout)
	defer cancel()
	res, err := s.schedule.uc.CreateFromSchedule(ctx, token, cs)
	s.schedule.journal.record(token, res)

	out, unknown := scheduleCreateResult(res)
	out.Problem = createProblem(err)
	counts := []any{"created", len(out.Created), "aliased", len(out.Aliased), "skipped", len(out.Skipped)}
	if unknown > 0 {
		counts = append(counts, "unknown_reasons", unknown)
	}
	if out.Problem != "" {
		s.log.Warn(opScheduleSubjectsCreate, append(counts, "problem", out.Problem)...)
	} else {
		s.log.Info(opScheduleSubjectsCreate, counts...)
	}
	return out
}

// UndoFromSchedule is «Отменить всё» for token: it takes back the outcome the core recorded for that token in this run
// of the app; the page passes nothing else. A repeated undo is safe and passes the same recorded result. It may take
// seconds; a second create or undo meanwhile answers busy.
func (s *LibraryService) UndoFromSchedule(ctx context.Context, token string) ScheduleUndoResult {
	empty, _ := scheduleUndoResult(library.ScheduleUndo{})
	if s.schedule.uc == nil {
		s.log.Error(opScheduleSubjectsUndo, "problem", ScheduleProblemUnavailable, "cause", "not wired")
		empty.Problem = ScheduleProblemUnavailable
		return empty
	}
	if !s.schedule.running.CompareAndSwap(false, true) {
		s.log.Info(opScheduleSubjectsUndo, "problem", ScheduleProblemBusy)
		empty.Problem = ScheduleProblemBusy
		return empty
	}
	defer s.schedule.running.Store(false)

	// Looked up under the guard, so a create of the same token that was running has merged its outcome by now.
	res, ok := s.schedule.journal.lookup(token)
	if !ok {
		s.log.Info(opScheduleSubjectsUndo, "problem", ScheduleProblemInvalid, "cause", "unknown token")
		empty.Problem = ScheduleProblemInvalid
		return empty
	}

	ctx, cancel := context.WithTimeout(ctx, scheduleCallTimeout)
	defer cancel()
	undo, err := s.schedule.uc.UndoFromSchedule(ctx, token, res)

	out, unknown := scheduleUndoResult(undo)
	out.Problem = undoProblem(err)
	counts := []any{
		"removed", len(out.Removed), "kept", len(out.Kept), "unaliased", len(out.Unaliased), "kept_aliases", len(out.KeptAliases),
	}
	if unknown > 0 {
		counts = append(counts, "unknown_reasons", unknown)
	}
	if out.Problem != "" {
		s.log.Warn(opScheduleSubjectsUndo, append(counts, "problem", out.Problem)...)
	} else {
		s.log.Info(opScheduleSubjectsUndo, counts...)
	}
	return out
}

// createProblem maps an error of CreateFromSchedule; the stale check goes first, as a stale token says nothing else.
func createProblem(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, library.ErrStale):
		return ScheduleProblemStale
	case errors.Is(err, library.ErrNameTaken), errors.Is(err, schedulesubjects.ErrNameTaken):
		return ScheduleProblemNameTaken
	case errors.Is(err, library.ErrInvalidRequest), errors.Is(err, library.ErrInvalidName),
		errors.Is(err, schedulesubjects.ErrUnknownGroup), errors.Is(err, schedulesubjects.ErrUnknownMember),
		errors.Is(err, schedulesubjects.ErrInvalidChoice):
		return ScheduleProblemInvalid
	case errors.Is(err, library.ErrBusy):
		return ScheduleProblemBusy
	}
	return ScheduleProblemUnavailable
}

// undoProblem maps an error of UndoFromSchedule.
func undoProblem(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, library.ErrInvalidRequest):
		return ScheduleProblemInvalid
	case errors.Is(err, library.ErrBusy):
		return ScheduleProblemBusy
	}
	return ScheduleProblemUnavailable
}

// scheduleChoices maps the page's choices; an unknown action or too many choices is refused, never passed down.
func scheduleChoices(in []ScheduleChoice) ([]schedulesubjects.Choice, bool) {
	if len(in) > maxScheduleChoices {
		return nil, false
	}
	out := make([]schedulesubjects.Choice, 0, len(in))
	for _, c := range in {
		var action schedulesubjects.GroupAction
		switch c.Action {
		case ScheduleActionCreate:
			action = schedulesubjects.GroupCreate
		case ScheduleActionAliasOf:
			action = schedulesubjects.GroupAddToSubject
		case ScheduleActionSkip:
			action = schedulesubjects.GroupSkip
		default:
			return nil, false
		}
		if len(c.Members) > maxScheduleChoices {
			return nil, false
		}
		ch := schedulesubjects.Choice{Group: c.Group, Action: action, Name: c.Name}
		for _, m := range c.Members {
			var ma schedulesubjects.MemberAction
			switch m.Action {
			case ScheduleActionKeep:
				ma = schedulesubjects.MemberMerge
			case ScheduleActionSeparate:
				ma = schedulesubjects.MemberSeparately
			case ScheduleActionSkip:
				ma = schedulesubjects.MemberSkip
			default:
				return nil, false
			}
			ch.Members = append(ch.Members, schedulesubjects.MemberChoice{Member: m.Key, Action: ma, Name: m.Name})
		}
		out = append(out, ch)
	}
	return out, true
}

func scheduleGroup(g schedulesubjects.Group) ScheduleGroup {
	out := ScheduleGroup{
		Key: g.Key, Name: g.Name, Aliases: nonNilStrings(g.Aliases), Kinds: scheduleKinds(g.Kinds), Lessons: g.Lessons,
		Recordings: g.Recordings, Doubtful: g.Doubtful, Members: make([]ScheduleMember, 0, len(g.Members)),
		Action: ScheduleActionCreate,
	}
	if g.Subject != "" {
		out.Action, out.SubjectID, out.SubjectName = ScheduleActionAliasOf, string(g.Subject), g.Name
	}
	for _, m := range g.Members {
		out.Members = append(out.Members, ScheduleMember{
			Key: m.Key, Name: m.Name, Spellings: nonNilStrings(m.Spellings), Kinds: scheduleKinds(m.Kinds), Lessons: m.Lessons,
			Recordings: m.Recordings, Doubtful: m.Doubtful, Link: scheduleLink(m.Link),
		})
	}
	return out
}

// scheduleKinds lists the non-zero kinds: lecture, seminar, lab, unknown.
func scheduleKinds(k schedulesubjects.Kinds) []ScheduleKindCount {
	out := []ScheduleKindCount{}
	for _, kc := range []ScheduleKindCount{
		{ScheduleKindLecture, k.Lecture}, {ScheduleKindSeminar, k.Seminar}, {ScheduleKindLab, k.Lab}, {ScheduleKindUnknown, k.Unknown},
	} {
		if kc.Lessons > 0 {
			out = append(out, kc)
		}
	}
	return out
}

func scheduleLink(l schedulesubjects.Link) string {
	switch l {
	case schedulesubjects.LinkAbbreviation:
		return ScheduleLinkAbbreviation
	case schedulesubjects.LinkTypo:
		return ScheduleLinkTypo
	}
	return ScheduleLinkSame
}

// skipReason maps a reason of a skipped row; ok is false for a reason the page does not know. The fallback is
// name_taken: the only reason the use case gives for both kinds of row (a subject to create and spellings to add).
func skipReason(r library.ScheduleReason) (string, bool) {
	switch r {
	case library.ReasonNameTaken:
		return ScheduleReasonNameTaken, true
	case library.ReasonTooManyAliases:
		return ScheduleReasonTooManyAliases, true
	case library.ReasonSubjectGone:
		return ScheduleReasonSubjectGone, true
	}
	return ScheduleReasonNameTaken, false
}

// keptReason maps a reason of a subject the undo left. The fallback is has_files: it claims no notes of the student
// and only says the subject holds something, which is why it stays.
func keptReason(r library.ScheduleReason) (string, bool) {
	switch r {
	case library.ReasonHasFiles:
		return ScheduleReasonHasFiles, true
	case library.ReasonHasNotes:
		return ScheduleReasonHasNotes, true
	}
	return ScheduleReasonHasFiles, false
}

// keptAliasesReason maps a reason of spellings the undo left; stale is the only one the page knows.
func keptAliasesReason(r library.ScheduleReason) (string, bool) {
	return ScheduleReasonStale, r == library.ReasonStale
}

// scheduleCreateResult maps the outcome for the page; unknown counts the reasons that fell back.
func scheduleCreateResult(res library.ScheduleResult) (out ScheduleCreateResult, unknown int) {
	out = ScheduleCreateResult{
		Created: make([]ScheduleCreatedSubject, 0, len(res.Created)), Aliased: scheduleAliases(res.Aliased),
		Skipped: make([]ScheduleSkippedRow, 0, len(res.Skipped)),
	}
	for _, c := range res.Created {
		out.Created = append(out.Created, ScheduleCreatedSubject{ID: string(c.ID), Key: c.Key, Name: c.Name})
	}
	for _, sk := range res.Skipped {
		reason, ok := skipReason(sk.Reason)
		if !ok {
			unknown++
		}
		out.Skipped = append(out.Skipped, ScheduleSkippedRow{Key: sk.Key, SubjectID: string(sk.Subject), Reason: reason})
	}
	return out, unknown
}

// scheduleUndoResult maps the undo for the page; unknown counts the reasons that fell back.
func scheduleUndoResult(u library.ScheduleUndo) (out ScheduleUndoResult, unknown int) {
	out = ScheduleUndoResult{
		Removed: make([]string, 0, len(u.Removed)), Kept: make([]ScheduleKeptSubject, 0, len(u.Kept)),
		Unaliased: scheduleAliases(u.Unaliased), KeptAliases: make([]ScheduleKeptAliases, 0, len(u.KeptAliases)),
	}
	for _, id := range u.Removed {
		out.Removed = append(out.Removed, string(id))
	}
	for _, k := range u.Kept {
		reason, ok := keptReason(k.Reason)
		if !ok {
			unknown++
		}
		out.Kept = append(out.Kept, ScheduleKeptSubject{ID: string(k.ID), Name: k.Name, Reason: reason})
	}
	for _, k := range u.KeptAliases {
		reason, ok := keptAliasesReason(k.Reason)
		if !ok {
			unknown++
		}
		out.KeptAliases = append(out.KeptAliases, ScheduleKeptAliases{
			SubjectID: string(k.Subject), Aliases: nonNilStrings(k.Aliases), Reason: reason,
		})
	}
	return out, unknown
}

func scheduleAliases(in []library.ScheduleAliased) []ScheduleAliases {
	out := make([]ScheduleAliases, 0, len(in))
	for _, a := range in {
		out = append(out, ScheduleAliases{SubjectID: string(a.Subject), Aliases: nonNilStrings(a.Aliases)})
	}
	return out
}
