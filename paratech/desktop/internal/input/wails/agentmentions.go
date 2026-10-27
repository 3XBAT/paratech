package wails

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/agent/values"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// opAgentMentions is the log name of the «@» search.
const opAgentMentions = "ui: agent mention candidates"

// Limits of the «@» search (ADR-0028 «References, not copies»).
const (
	MaxMentionCandidates  = 20
	maxMentionQueryRunes  = 100
	maxMentionLabelRunes  = 120 // within values.MaxMentionLabelRunes, so a candidate can go back in an Ask
	maxMentionHintRunes   = 120
	maxMentionReads       = 500 // objects read per source
	maxMentionGroups      = 100 // subjects whose materials or lectures are read
	maxMentionLessonCheck = 40  // existence checks of lessons per call
)

// Labels of a candidate whose own name is empty after cleaning, and of the inbox in a hint.
const (
	mentionSubjectLabel    = "Предмет"
	mentionLessonLabel     = "Пара"
	mentionMaterialLabel   = "Файл"
	mentionAssignmentLabel = "Задание"
	mentionInboxName       = "Входящие"
)

// AgentMentionLibrary is the library as the «@» search reads it; satisfied by library.Service.
type AgentMentionLibrary interface {
	Subjects(ctx context.Context) ([]*subject.Subject, error)
	Materials(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error)
}

// AgentMentionLectures is the knowledge base as the «@» search reads it; satisfied by knowledgebase.Service.
type AgentMentionLectures interface {
	Subjects(ctx context.Context) ([]kb.Subject, error)
	Lectures(ctx context.Context, key string) ([]kb.Lesson, error)
}

// AgentMentionAssignments lists the assignments that are not deleted; satisfied by homeworkflow.Service.
type AgentMentionAssignments interface {
	ListAssignments(ctx context.Context) ([]homeworkflow.AssignmentView, error)
}

// AgentMentionExists tells whether a mentioned object still exists, the check of agentturn.Mentioned.
type AgentMentionExists interface {
	Exists(ctx context.Context, ref values.Ref) (bool, error)
}

// AgentMentionSources are what the «@» search reads; Local gives the student's zone of the dates in the hints.
type AgentMentionSources struct {
	Library     AgentMentionLibrary
	Lectures    AgentMentionLectures
	Assignments AgentMentionAssignments
	Exists      AgentMentionExists
	Local       AgentClock
}

func (m AgentMentionSources) complete() bool {
	return m.Library != nil && m.Lectures != nil && m.Assignments != nil && m.Exists != nil && m.Local != nil
}

// AgentMentionCandidate is an object the student can mention with «@»: kind is subject, lesson, material or
// assignment; label is its name and hint its subject and date, plain text of one line. No file path or content.
type AgentMentionCandidate struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// AgentMentionsView is the answer of MentionCandidates: Problem is "", unavailable or failed.
type AgentMentionsView struct {
	Problem    string                  `json:"problem"`
	Candidates []AgentMentionCandidate `json:"candidates"`
}

// mentionCandidate is a candidate with the time of its last activity, for the order.
type mentionCandidate struct {
	view AgentMentionCandidate
	ref  values.Ref
	at   time.Time
}

// MentionCandidates finds at most MaxMentionCandidates objects whose name contains query, case-insensitive: library
// subjects, lessons with a recording or notes, active materials and assignments not deleted, the most recently active
// first. An empty query lists the most recent ones. It reads only the local database.
func (s *AgentService) MentionCandidates(ctx context.Context, query string) AgentMentionsView {
	if !s.mentions.complete() {
		return AgentMentionsView{Problem: AgentProblemUnavailable, Candidates: []AgentMentionCandidate{}}
	}
	out, err := s.mentionCandidates(ctx, mentionQuery(query))
	if err != nil {
		s.log.Warn(opAgentMentions, "err", err)
		return AgentMentionsView{Problem: AgentProblemFailed, Candidates: []AgentMentionCandidate{}}
	}
	return AgentMentionsView{Candidates: out}
}

func (s *AgentService) mentionCandidates(ctx context.Context, q string) ([]AgentMentionCandidate, error) {
	src := s.mentions
	loc := src.Local.Now().Location()

	subjects, err := src.Library.Subjects(ctx)
	if err != nil {
		return nil, err
	}
	subjects = subjects[:min(len(subjects), maxMentionReads)]
	names := make(map[shared.SubjectID]string, len(subjects))
	var found []mentionCandidate
	for _, sub := range subjects {
		names[sub.ID()] = sub.Name().String()
		if mentionSubjectMatches(sub, q) {
			found = appendMention(found, values.KindSubject, string(sub.ID()), sub.Name().String(), "", sub.UpdatedAt())
		}
	}

	materials, err := s.mentionMaterials(ctx, subjects, names, q, loc)
	if err != nil {
		return nil, err
	}
	lessons, err := s.mentionLessons(ctx, q, loc)
	if err != nil {
		return nil, err
	}
	assignments, err := s.mentionAssignments(ctx, q, loc)
	if err != nil {
		return nil, err
	}
	found = append(append(append(found, materials...), lessons...), assignments...)

	slices.SortStableFunc(found, func(a, b mentionCandidate) int {
		if c := b.at.Compare(a.at); c != 0 {
			return c
		}
		if c := cmp.Compare(a.view.Kind, b.view.Kind); c != 0 {
			return c
		}
		return cmp.Compare(a.view.ID, b.view.ID)
	})
	return s.existing(ctx, found)
}

// existing keeps the first MaxMentionCandidates candidates; a lesson is checked against the schedule first, as the
// knowledge base still names a deleted lesson its recordings were made in.
func (s *AgentService) existing(ctx context.Context, found []mentionCandidate) ([]AgentMentionCandidate, error) {
	out := make([]AgentMentionCandidate, 0, MaxMentionCandidates)
	checks := 0
	for _, c := range found {
		if len(out) == MaxMentionCandidates {
			break
		}
		if c.ref.Kind() == values.KindLesson {
			if checks == maxMentionLessonCheck {
				continue
			}
			checks++
			ok, err := s.mentions.Exists.Exists(ctx, c.ref)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		out = append(out, c.view)
	}
	return out, nil
}

// mentionMaterials lists the active materials of the subjects and of «Входящие» whose name contains q.
func (s *AgentService) mentionMaterials(ctx context.Context, subjects []*subject.Subject, names map[shared.SubjectID]string, q string, loc *time.Location) ([]mentionCandidate, error) {
	groups := []shared.SubjectID{""}
	for _, sub := range subjects[:min(len(subjects), maxMentionGroups)] {
		groups = append(groups, sub.ID())
	}
	var out []mentionCandidate
	read := 0
	for _, g := range groups {
		if read >= maxMentionReads {
			break
		}
		list, err := s.mentions.Library.Materials(ctx, g)
		if err != nil {
			return nil, err
		}
		for _, m := range list[:min(len(list), maxMentionReads-read)] {
			read++
			name := fileName(m.Name())
			if !m.Active() || !mentionMatches(name, q) {
				continue
			}
			where := mentionInboxName
			if !m.InInbox() {
				where = names[m.Subject()]
			}
			out = appendMention(out, values.KindMaterial, string(m.ID()), name, mentionHint(where, "", m.UpdatedAt(), loc), m.UpdatedAt())
		}
	}
	return out, nil
}

// mentionLessons lists the schedule lessons with a recording or notes whose title contains q, each once (its newest
// lecture).
func (s *AgentService) mentionLessons(ctx context.Context, q string, loc *time.Location) ([]mentionCandidate, error) {
	groups, err := s.mentions.Lectures.Subjects(ctx)
	if err != nil {
		return nil, err
	}
	newest := map[shared.LessonID]mentionCandidate{}
	read := 0
	for _, g := range groups[:min(len(groups), maxMentionGroups)] {
		if read >= maxMentionReads {
			break
		}
		list, err := s.mentions.Lectures.Lectures(ctx, g.Key)
		if err != nil {
			return nil, err
		}
		for _, l := range list[:min(len(list), maxMentionReads-read)] {
			read++
			if l.ScheduleLesson == "" || !mentionMatches(l.Title, q) {
				continue
			}
			at := l.StartedAt
			if at.IsZero() && len(l.Notes) > 0 {
				at = l.Notes[0].CreatedAt
			}
			if prev, ok := newest[l.ScheduleLesson]; ok && !at.After(prev.at) {
				continue
			}
			c := appendMention(nil, values.KindLesson, string(l.ScheduleLesson), l.Title, mentionHint(lectureSubject(l), "", l.StartedAt, loc), at)
			if len(c) == 1 {
				newest[l.ScheduleLesson] = c[0]
			}
		}
	}
	out := make([]mentionCandidate, 0, len(newest))
	for _, c := range newest {
		out = append(out, c) // the caller orders them
	}
	return out, nil
}

// mentionAssignments lists the assignments not deleted whose title contains q; the hint has the deadline, else the
// date of the lesson.
func (s *AgentService) mentionAssignments(ctx context.Context, q string, loc *time.Location) ([]mentionCandidate, error) {
	list, err := s.mentions.Assignments.ListAssignments(ctx)
	if err != nil {
		return nil, err
	}
	var out []mentionCandidate
	for _, a := range list[:min(len(list), maxMentionReads)] {
		if a.Deleted || !mentionMatches(a.Title, q) {
			continue
		}
		hint := mentionHint(a.Subject, "до ", a.Due, loc)
		if a.Due.IsZero() {
			hint = mentionHint(a.Subject, "", a.LessonDate, loc)
		}
		out = appendMention(out, values.KindAssignment, string(a.ID), a.Title, hint, a.UpdatedAt)
	}
	return out, nil
}

// appendMention adds a candidate with a cleaned label and hint; one whose id or label would not pass as a mention of
// an Ask is left out.
func appendMention(out []mentionCandidate, kind values.IDKind, id, label, hint string, at time.Time) []mentionCandidate {
	ref, err := values.NewRef(kind, id)
	if err != nil {
		return out
	}
	label = mentionText(label, maxMentionLabelRunes)
	if label == "" {
		label = mentionFallback(kind)
	}
	if _, err := values.NewMention(ref, label); err != nil {
		return out
	}
	return append(out, mentionCandidate{
		view: AgentMentionCandidate{Kind: string(kind), ID: id, Label: label, Hint: mentionText(hint, maxMentionHintRunes)},
		ref:  ref, at: at,
	})
}

func mentionFallback(kind values.IDKind) string {
	switch kind {
	case values.KindSubject:
		return mentionSubjectLabel
	case values.KindLesson:
		return mentionLessonLabel
	case values.KindMaterial:
		return mentionMaterialLabel
	}
	return mentionAssignmentLabel
}

// mentionQuery is the query folded for matching: cleaned, cut to maxMentionQueryRunes, lower case.
func mentionQuery(q string) string {
	return strings.ToLower(mentionText(q, maxMentionQueryRunes))
}

// mentionMatches reports whether the cleaned name contains q, case-insensitive; an empty q matches every name.
func mentionMatches(name, q string) bool {
	return strings.Contains(strings.ToLower(mentionText(name, maxMentionLabelRunes)), q)
}

// mentionSubjectMatches matches the name of a subject or one of its aliases («матан»).
func mentionSubjectMatches(sub *subject.Subject, q string) bool {
	if mentionMatches(sub.Name().String(), q) {
		return true
	}
	return q != "" && slices.ContainsFunc(sub.Aliases(), func(a subject.Name) bool { return mentionMatches(a.String(), q) })
}

// mentionHint is «subject · date» in the student's zone; either part may be missing.
func mentionHint(subjectName, datePrefix string, at time.Time, loc *time.Location) string {
	var parts []string
	if s := mentionText(subjectName, maxMentionHintRunes); s != "" {
		parts = append(parts, s)
	}
	if !at.IsZero() {
		parts = append(parts, datePrefix+at.In(loc).Format("02.01.2006"))
	}
	return strings.Join(parts, " · ")
}

// lectureSubject is the subject a lecture is shown under.
func lectureSubject(l kb.Lesson) string {
	switch {
	case l.Library.ID != "":
		return l.Library.Name
	case l.Subject != "":
		return l.Subject
	}
	return kb.NoSubjectName
}

// fileName keeps the last segment of a material name, so no folder of the student's disk can reach the page.
func fileName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[i+1:]
	}
	return name
}

// mentionText makes untrusted text one line of at most limit runes: spaces, line breaks and control characters
// become one space, format characters (bidi overrides, zero-width) and invalid bytes are dropped.
func mentionText(s string, limit int) string {
	var b strings.Builder
	n, space := 0, false
	for _, r := range strings.ToValidUTF8(s, "") {
		switch {
		case unicode.IsSpace(r) || unicode.IsControl(r):
			space = n > 0
			continue
		case r == utf8.RuneError || !unicode.IsGraphic(r):
			continue
		}
		if space {
			if n+1 >= limit {
				break
			}
			b.WriteByte(' ')
			n++
			space = false
		}
		if n >= limit {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
