package library

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"paratech/desktop/internal/domain/library/services/schedulesubjects"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Subjects from the schedule (ADR-0033 §4, §7): ScheduleSubjects proposes the package, CreateFromSchedule applies the
// student's one «Да», UndoFromSchedule is «Отменить всё». Schedule strings are untrusted text (ADR-0022): they are
// never logged and never put into an error; the log holds counts only.

// Operation names of the log.
const (
	opScheduleApplied = "library: subjects from the schedule created"
	opScheduleStopped = "library: subjects from the schedule stopped midway"
	opScheduleUndone  = "library: subjects from the schedule undone"
)

const (
	// maxScheduleRows bounds the distinct strings given to schedulesubjects.Propose: its doubtful matching is
	// quadratic in them (200 similar strings take about 0.2 s). A term has a few dozen subjects with a few spellings
	// each, so 200 leaves room; beyond it the most used rows are kept (lessons + recordings, then by string), the rest
	// is counted in Omitted.
	maxScheduleRows = 200

	// maxBaseGuesses bounds the proposals made to find the subjects a token was made for after a part of its package
	// was applied (scheduleBase), so a retry costs at most that many proposals; past it the token is stale, which is
	// safe: nothing was done twice and the student looks again.
	maxBaseGuesses = 64

	// scheduleTokenVersion starts the hashed content, so a change of what is hashed never matches an old token.
	scheduleTokenVersion = "schedule-subjects/1"
)

// ErrStale means the schedule strings or the subjects changed since the proposal was shown: the student looks again.
var ErrStale = errors.New("library: the schedule or the subjects changed")

// ScheduleReason tells why a row of the package was skipped or a subject kept by the undo.
type ScheduleReason string

// Reasons.
const (
	ReasonNameTaken      ScheduleReason = "name_taken"       // another subject has the name or an alias meanwhile
	ReasonTooManyAliases ScheduleReason = "too_many_aliases" // the subject has subject.MaxAliases aliases
	ReasonSubjectGone    ScheduleReason = "subject_gone"     // the subject to add the spellings to was removed
	ReasonHasFiles       ScheduleReason = "has_files"        // undo: the subject holds files, it stays
	ReasonHasNotes       ScheduleReason = "has_notes"        // undo: the subject has notes of the student, it stays
)

// ScheduleProposal is the package to show: the groups and the token CreateFromSchedule needs. Omitted is how many
// distinct strings were left out beyond maxScheduleRows (the least used).
type ScheduleProposal struct {
	Token   string
	Groups  []schedulesubjects.Group
	Omitted int
}

// ScheduleCreated is a subject of the package: Key is the group or member key its ID is derived from.
type ScheduleCreated struct {
	ID   shared.SubjectID
	Key  string
	Name string
}

// ScheduleAliased is the spellings added to an existing subject.
type ScheduleAliased struct {
	Subject shared.SubjectID
	Aliases []string
}

// ScheduleSkipped is a row not done: Key for a subject to create, Subject for spellings to add.
type ScheduleSkipped struct {
	Key     string
	Subject shared.SubjectID
	Reason  ScheduleReason
}

// ScheduleResult is what CreateFromSchedule did, in the order of the outcome; UndoFromSchedule takes it back.
type ScheduleResult struct {
	Created []ScheduleCreated
	Aliased []ScheduleAliased
	Skipped []ScheduleSkipped
}

// ScheduleKept is a subject of the package the undo left: it is no longer empty.
type ScheduleKept struct {
	ID     shared.SubjectID
	Name   string
	Reason ScheduleReason
}

// ScheduleUndo is what UndoFromSchedule did.
type ScheduleUndo struct {
	Removed   []shared.SubjectID
	Kept      []ScheduleKept
	Unaliased []ScheduleAliased
}

// ScheduleSubjects proposes the subjects to create from the free subject strings of the schedule and the recordings,
// with the token of this proposal and of the subjects it was made for. Nothing to propose is an empty list.
func (s *Service) ScheduleSubjects(ctx context.Context) (ScheduleProposal, error) {
	rows, omitted, list, err := s.scheduleState(ctx)
	if err != nil {
		return ScheduleProposal{}, err
	}
	entries := entriesOf(list)
	p := schedulesubjects.Propose(rows, entries)
	return ScheduleProposal{Token: scheduleToken(p, entries), Groups: p.Groups, Omitted: omitted}, nil
}

// CreateFromSchedule applies the student's choices to the proposal of token. The proposal is made again here from the
// stored strings and subjects, never taken from the caller; one of another token is ErrStale. Each subject is its own
// transaction with the ID derived from the token and its key, so a retry with the same token creates nothing twice and
// does the rest (the subjects and spellings this package made already are taken out to find its proposal again). A
// row whose name or alias another subject took meanwhile is skipped (name_taken), spellings for a subject at its alias
// limit too (too_many_aliases). A failure midway returns what was done with the error; the done part stays.
// Errors: ErrInvalidRequest (a bad token or choice), ErrStale, ErrNameTaken and ErrInvalidName (a rename),
// ErrBusy, ErrUnavailable.
func (s *Service) CreateFromSchedule(ctx context.Context, token string, choices []schedulesubjects.Choice) (ScheduleResult, error) {
	if !validScheduleToken(token) {
		return ScheduleResult{}, ErrInvalidRequest
	}
	rows, _, list, err := s.scheduleState(ctx)
	if err != nil {
		return ScheduleResult{}, err
	}
	p, err := scheduleBase(ctx, token, rows, list)
	if err != nil {
		return ScheduleResult{}, err
	}
	out, err := schedulesubjects.Apply(p, choices)
	if err != nil {
		return ScheduleResult{}, applyError(err)
	}

	now := s.clock.Now()
	var res ScheduleResult
	for _, c := range out.Creates {
		if err = s.createScheduled(ctx, token, c, now, &res); err != nil {
			break
		}
	}
	for _, a := range out.Additions {
		if err != nil {
			break
		}
		err = s.addScheduled(ctx, a, now, &res)
	}

	counts := []any{
		slog.Int("created", len(res.Created)), slog.Int("aliased", len(res.Aliased)), slog.Int("skipped", len(res.Skipped)),
	}
	if err != nil {
		s.log.Warn(opScheduleStopped, counts...)
		return res, err
	}
	s.log.Info(opScheduleApplied, counts...)
	return res, nil
}

// createScheduled creates the subject c with all six system folders, as CreateSubject; one that exists under its
// derived ID was created by this package before and counts as created.
func (s *Service) createScheduled(ctx context.Context, token string, c schedulesubjects.Create, now time.Time, res *ScheduleResult) error {
	id := schedulesubjects.DeriveSubjectID(token, c.Key)
	created := func(name string) error {
		res.Created = append(res.Created, ScheduleCreated{ID: id, Key: c.Key, Name: name})
		return nil
	}
	skipped := func(r ScheduleReason) error {
		res.Skipped = append(res.Skipped, ScheduleSkipped{Key: c.Key, Reason: r})
		return nil
	}

	switch sub, err := s.subjects.Get(ctx, id); {
	case err == nil:
		return created(sub.Name().String())
	case !errors.Is(err, shared.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	sub, err := subject.New(subject.Spec{ID: id, Name: c.Name, Aliases: c.Aliases, Semester: values.SemesterOf(now)}, now)
	if err != nil {
		return domainError(err)
	}
	ix, err := s.index(ctx)
	if err != nil {
		return err
	}
	if ix.TakenByOther(sub.Name().String(), id) || slices.ContainsFunc(sub.Aliases(), func(a subject.Name) bool {
		return ix.TakenByOther(a.String(), id)
	}) {
		return skipped(ReasonNameTaken)
	}

	err = s.subjects.Save(ctx, sub)
	if err == nil {
		return created(sub.Name().String())
	}
	if !errors.Is(err, shared.ErrDuplicate) && !errors.Is(err, shared.ErrVersionConflict) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	// Another call of the same package created it meanwhile, or another subject took a spelling.
	switch got, gerr := s.subjects.Get(ctx, id); {
	case gerr == nil:
		return created(got.Name().String())
	case !errors.Is(gerr, shared.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrUnavailable, gerr)
	case errors.Is(err, shared.ErrDuplicate):
		return skipped(ReasonNameTaken)
	}
	return ErrBusy
}

// addScheduled adds the spellings of a to its subject in one transaction, with the checks of AddAlias; all of them or
// none, so a retry finds the subject as before or as done.
func (s *Service) addScheduled(ctx context.Context, a schedulesubjects.Addition, now time.Time, res *ScheduleResult) error {
	_, err := s.changeSubject(ctx, a.Subject, func(sub *subject.Subject) (bool, error) {
		ix, err := s.index(ctx)
		if err != nil {
			return false, err
		}
		changed := false
		for _, alias := range a.Aliases {
			if ix.TakenByOther(alias, a.Subject) {
				return false, ErrNameTaken
			}
			c, err := sub.AddAlias(alias, now)
			if err != nil {
				return false, domainError(err)
			}
			changed = changed || c
		}
		return changed, nil
	})

	reason := ScheduleReason("")
	switch {
	case err == nil:
		res.Aliased = append(res.Aliased, ScheduleAliased{Subject: a.Subject, Aliases: slices.Clone(a.Aliases)})
		return nil
	case errors.Is(err, ErrNameTaken):
		reason = ReasonNameTaken
	case errors.Is(err, ErrTooMany):
		reason = ReasonTooManyAliases
	case errors.Is(err, ErrSubjectNotFound):
		reason = ReasonSubjectGone
	default:
		return err
	}
	res.Skipped = append(res.Skipped, ScheduleSkipped{Subject: a.Subject, Reason: reason})
	return nil
}

// UndoFromSchedule takes back the result of CreateFromSchedule with token («Отменить всё»). A subject is removed only
// when its ID is the one derived from the token and its key and it is still empty (the checks of RemoveSubject); one
// with notes or files stays and is listed in Kept. The added spellings are removed again; they must be spellings the
// proposal of the token gave to that subject, so the proposal is found again (ErrStale when the schedule or the
// subjects changed meanwhile). Removed and gone subjects both count as removed, so a retry is safe. Errors:
// ErrInvalidRequest (a bad token, an ID or spelling not of this package), ErrStale, ErrBusy, ErrUnavailable.
func (s *Service) UndoFromSchedule(ctx context.Context, token string, res ScheduleResult) (ScheduleUndo, error) {
	if !validScheduleToken(token) || len(res.Created) > maxScheduleRows || len(res.Aliased) > maxScheduleRows {
		return ScheduleUndo{}, ErrInvalidRequest
	}
	var ids []shared.SubjectID
	for _, c := range res.Created {
		// A key is the key of a name: at most MaxNameRunes runes of up to 4 bytes.
		if c.Key == "" || len(c.Key) > 4*subject.MaxNameRunes || c.ID != schedulesubjects.DeriveSubjectID(token, c.Key) {
			return ScheduleUndo{}, ErrInvalidRequest
		}
		if !slices.Contains(ids, c.ID) {
			ids = append(ids, c.ID)
		}
	}
	if err := s.checkScheduleAliases(ctx, token, res.Aliased); err != nil {
		return ScheduleUndo{}, err
	}

	var undo ScheduleUndo
	err := s.undoScheduled(ctx, ids, res.Aliased, &undo)
	counts := []any{
		slog.Int("removed", len(undo.Removed)), slog.Int("kept", len(undo.Kept)), slog.Int("unaliased", len(undo.Unaliased)),
	}
	if err != nil {
		s.log.Warn(opScheduleStopped, counts...)
		return undo, err
	}
	s.log.Info(opScheduleUndone, counts...)
	return undo, nil
}

func (s *Service) undoScheduled(ctx context.Context, ids []shared.SubjectID, aliased []ScheduleAliased, undo *ScheduleUndo) error {
	for _, id := range ids {
		sub, err := s.subject(ctx, id)
		switch {
		case errors.Is(err, ErrSubjectNotFound):
			undo.Removed = append(undo.Removed, id)
			continue
		case err != nil:
			return err
		}
		switch err := s.RemoveSubject(ctx, id); {
		case err == nil, errors.Is(err, ErrSubjectNotFound):
			undo.Removed = append(undo.Removed, id)
		case errors.Is(err, ErrHasNotes): // before ErrNotEmpty, which it wraps
			undo.Kept = append(undo.Kept, ScheduleKept{ID: id, Name: sub.Name().String(), Reason: ReasonHasNotes})
		case errors.Is(err, ErrNotEmpty):
			undo.Kept = append(undo.Kept, ScheduleKept{ID: id, Name: sub.Name().String(), Reason: ReasonHasFiles})
		default:
			return err
		}
	}

	for _, a := range aliased {
		now := s.clock.Now()
		_, err := s.changeSubject(ctx, a.Subject, func(sub *subject.Subject) (bool, error) {
			changed := false
			for _, alias := range a.Aliases {
				changed = sub.RemoveAlias(alias, now) || changed
			}
			return changed, nil
		})
		if err != nil && !errors.Is(err, ErrSubjectNotFound) {
			return err
		}
		undo.Unaliased = append(undo.Unaliased, ScheduleAliased{Subject: a.Subject, Aliases: slices.Clone(a.Aliases)})
	}
	return nil
}

// checkScheduleAliases checks that every spelling to remove is one the proposal of token gave to that subject: a
// caller cannot remove the student's own aliases through the undo.
func (s *Service) checkScheduleAliases(ctx context.Context, token string, aliased []ScheduleAliased) error {
	if len(aliased) == 0 {
		return nil
	}
	total := 0
	for _, a := range aliased {
		total += len(a.Aliases)
		if a.Subject.Validate() != nil || len(a.Aliases) == 0 || total > maxScheduleRows {
			return ErrInvalidRequest
		}
	}
	rows, _, list, err := s.scheduleState(ctx)
	if err != nil {
		return err
	}
	p, err := scheduleBase(ctx, token, rows, list)
	if err != nil {
		return err
	}
	given := map[shared.SubjectID]map[string]bool{}
	for _, g := range p.Groups {
		if g.Subject == "" {
			continue
		}
		if given[g.Subject] == nil {
			given[g.Subject] = map[string]bool{}
		}
		for _, m := range g.Members {
			for _, sp := range m.Spellings {
				given[g.Subject][values.Key(sp)] = true
			}
		}
	}
	for _, a := range aliased {
		for _, alias := range a.Aliases {
			if !given[a.Subject][values.Key(alias)] {
				return ErrInvalidRequest
			}
		}
	}
	return nil
}

// scheduleState reads the strings (capped, see maxScheduleRows) and the subjects.
func (s *Service) scheduleState(ctx context.Context) ([]schedulesubjects.Row, int, []*subject.Subject, error) {
	all, err := s.strings.ListScheduleSubjectStrings(ctx)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	list, err := s.subjects.List(ctx)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	rows, omitted := capScheduleRows(all)
	return rows, omitted, list, nil
}

// capScheduleRows keeps the maxScheduleRows most used rows (lessons + recordings, then by string), deterministic for
// one store content.
func capScheduleRows(rows []schedulesubjects.Row) ([]schedulesubjects.Row, int) {
	if len(rows) <= maxScheduleRows {
		return rows, 0
	}
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b schedulesubjects.Row) int {
		if c := cmp.Compare(b.Lessons.Total()+b.Recordings, a.Lessons.Total()+a.Recordings); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return sorted[:maxScheduleRows], len(rows) - maxScheduleRows
}

// scheduleBase makes the proposal token was made for. That is the proposal of the current subjects, unless a part of
// the package was applied: then the subjects with an ID derived from the token are taken out, and the spellings the
// package added, which are the last aliases of their subjects and spellings of the rows; which of those trailing
// aliases are the student's own is not stored, so the splits are tried (the most recently changed subjects first, at
// most maxBaseGuesses) until one gives the token. None gives ErrStale.
func scheduleBase(ctx context.Context, token string, rows []schedulesubjects.Row, list []*subject.Subject) (schedulesubjects.Proposal, error) {
	entries := entriesOf(list)
	if p := schedulesubjects.Propose(rows, entries); scheduleToken(p, entries) == token {
		return p, nil
	}

	// The key of a row is the key of its member in any proposal (the key without the lesson-kind mark), so a proposal
	// of the row alone gives it cheaply.
	derived := map[shared.SubjectID]bool{}
	rowKeys := map[string]bool{}
	for _, r := range rows {
		rowKeys[values.Key(r.Name)] = true
		for _, g := range schedulesubjects.Propose([]schedulesubjects.Row{r}, nil).Groups {
			derived[schedulesubjects.DeriveSubjectID(token, g.Key)] = true
		}
	}

	type candidate struct {
		at, trailing int
		updated      time.Time
		id           shared.SubjectID
	}
	var base []subjectmatch.Entry
	var cands []candidate
	updated := map[shared.SubjectID]time.Time{}
	for _, sub := range list {
		updated[sub.ID()] = sub.UpdatedAt()
	}
	for _, e := range entries {
		if derived[e.ID] {
			continue
		}
		n := 0
		for i := len(e.Aliases) - 1; i >= 0 && rowKeys[values.Key(e.Aliases[i])]; i-- {
			n++
		}
		if n > 0 {
			cands = append(cands, candidate{at: len(base), trailing: n, updated: updated[e.ID], id: e.ID})
		}
		base = append(base, e)
	}
	slices.SortFunc(cands, func(a, b candidate) int {
		if c := b.updated.Compare(a.updated); c != 0 {
			return c
		}
		return cmp.Compare(a.id, b.id)
	})

	strip := make([]int, len(cands)) // a mixed-radix counter: the first candidate changes fastest
	guess := make([]subjectmatch.Entry, len(base))
	for range maxBaseGuesses {
		if err := ctx.Err(); err != nil {
			return schedulesubjects.Proposal{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		copy(guess, base)
		for i, c := range cands {
			a := base[c.at].Aliases
			guess[c.at].Aliases = a[:len(a)-strip[i]]
		}
		if p := schedulesubjects.Propose(rows, guess); scheduleToken(p, guess) == token {
			return p, nil
		}
		if !nextStrip(strip, func(i int) int { return cands[i].trailing }) {
			break
		}
	}
	return schedulesubjects.Proposal{}, ErrStale
}

// nextStrip advances the counter, digit i up to limit(i); false once every split was tried.
func nextStrip(strip []int, limit func(i int) int) bool {
	for i := range strip {
		if strip[i] < limit(i) {
			strip[i]++
			return true
		}
		strip[i] = 0
	}
	return false
}

// entriesOf is the subjects as subjectmatch entries, by ID.
func entriesOf(list []*subject.Subject) []subjectmatch.Entry {
	out := make([]subjectmatch.Entry, 0, len(list))
	for _, sub := range list {
		e := subjectmatch.Entry{ID: sub.ID(), Name: sub.Name().String()}
		for _, a := range sub.Aliases() {
			e.Aliases = append(e.Aliases, a.String())
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b subjectmatch.Entry) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// scheduleToken is the sha256 of the proposal and of the subjects it was made for, each field length-prefixed (as the
// schedule import plan's fingerprint).
func scheduleToken(p schedulesubjects.Proposal, entries []subjectmatch.Entry) string {
	h := sha256.New()
	field := func(s string) { _, _ = fmt.Fprintf(h, "%d:%s", len(s), s) } // a hash never fails to write
	num := func(n int) { field(strconv.Itoa(n)) }
	list := func(ss []string) {
		num(len(ss))
		for _, s := range ss {
			field(s)
		}
	}
	kinds := func(k schedulesubjects.Kinds) {
		num(k.Lecture)
		num(k.Seminar)
		num(k.Lab)
		num(k.Unknown)
	}
	flag := func(b bool) { field(strconv.FormatBool(b)) }

	field(scheduleTokenVersion)
	for _, g := range p.Groups {
		field("G")
		field(g.Key)
		field(g.Name)
		field(string(g.Subject))
		list(g.Aliases)
		kinds(g.Kinds)
		num(g.Lessons)
		num(g.Recordings)
		flag(g.Doubtful)
		for _, m := range g.Members {
			field("M")
			field(m.Key)
			field(m.Name)
			list(m.Spellings)
			kinds(m.Kinds)
			num(m.Lessons)
			num(m.Recordings)
			num(int(m.Link))
			flag(m.Doubtful)
		}
	}
	for _, e := range entries {
		field("S")
		field(string(e.ID))
		field(e.Name)
		list(e.Aliases)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// validScheduleToken reports whether token looks like one of scheduleToken: 64 lowercase hex digits.
func validScheduleToken(token string) bool {
	if len(token) != 2*sha256.Size {
		return false
	}
	for _, r := range token {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// applyError maps an error of schedulesubjects.Apply; none holds a schedule string.
func applyError(err error) error {
	switch {
	case errors.Is(err, schedulesubjects.ErrNameTaken):
		return fmt.Errorf("%w: %w", ErrNameTaken, err)
	case errors.Is(err, subject.ErrInvalidName):
		return domainError(err)
	}
	return fmt.Errorf("%w: %w", ErrInvalidRequest, err)
}
