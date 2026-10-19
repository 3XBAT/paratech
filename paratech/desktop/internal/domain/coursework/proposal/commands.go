package proposal

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// The commands below are what side-effect ports accept. Their fields are unexported and there is no constructor
// from raw data: the only way to get one is the method of the same name on a CONFIRMED proposal. A port that takes a
// command therefore cannot be fed model output directly (AC-18). Adapters reject the zero value (IsZero).

// EmailCommand sends the exported homework of an assignment to the trusted recipient (PRD section 10, AC-14).
type EmailCommand struct {
	key        string
	assignment shared.AssignmentID
	recipient  Recipient
	attachment shared.ArtifactRef
	digest     Digest
	confirmed  time.Time
	subject    string // empty: the mailer's default subject
}

// ExportCommand writes homework text to the export file of an assignment (PRD section 10 step 6).
type ExportCommand struct {
	key        string
	assignment shared.AssignmentID
	target     shared.ArtifactRef
	content    string
}

// CalendarCommand puts the deadline of an assignment into the app's own calendar (PRD section 11.3). The calendar is
// the adapter's dedicated one; the event is keyed by the assignment, so a repeated upsert updates the same event.
type CalendarCommand struct {
	key        string
	assignment shared.AssignmentID
	title      string
	due        time.Time
	lesson     values.LessonLabel // what the event names of the lesson; zero: nothing
}

// idempotencyKey is stable for one proposal: executing it again after a crash or a retry repeats the same request,
// which the adapter deduplicates (hard rule 7).
func (p *Proposal) idempotencyKey() string { return "proposal:" + p.id.String() }

func (p *Proposal) ready(kind Kind) error {
	if p.status != StatusConfirmed {
		return fmt.Errorf("%w: status %s", ErrNotConfirmed, p.status)
	}
	if p.kind != kind {
		return fmt.Errorf("%w: %s, not %s", ErrWrongKind, p.kind, kind)
	}
	return nil
}

// EmailCommand returns the command of a CONFIRMED send_homework_email proposal.
func (p *Proposal) EmailCommand() (EmailCommand, error) {
	if err := p.ready(KindSendHomework); err != nil {
		return EmailCommand{}, err
	}
	ref, err := ExportRefOf(p.assignment, p.format)
	if err != nil {
		return EmailCommand{}, err
	}
	return EmailCommand{key: p.idempotencyKey(), assignment: p.assignment, recipient: p.recipient, attachment: ref, digest: p.attachment,
		confirmed: p.confirmedAt}, nil
}

// ExportCommand returns the command of a CONFIRMED export_homework proposal.
func (p *Proposal) ExportCommand() (ExportCommand, error) {
	if err := p.ready(KindExportHomework); err != nil {
		return ExportCommand{}, err
	}
	ref, err := ExportRef(p.assignment)
	if err != nil {
		return ExportCommand{}, err
	}
	return ExportCommand{
		key: p.idempotencyKey(), assignment: p.assignment, target: ref,
		content: values.HomeworkMarkdown(p.assignmentTitle, p.body),
	}, nil
}

// CalendarCommand returns the command of a CONFIRMED create_deadline_event proposal.
func (p *Proposal) CalendarCommand() (CalendarCommand, error) {
	if err := p.ready(KindDeadlineEvent); err != nil {
		return CalendarCommand{}, err
	}
	return CalendarCommand{key: p.idempotencyKey(), assignment: p.assignment, title: p.title, due: p.due}, nil
}

// WithSubject returns c with the email subject the core built from trusted local state
// (values.HomeworkEmailSubject). The subject must be one line of 1 to values.MaxEmailSubjectRunes characters without
// control or format characters, so it cannot add a mail header; otherwise, or for the zero command, the error wraps
// ErrInvalidProposal.
func (c EmailCommand) WithSubject(subject string) (EmailCommand, error) {
	switch {
	case c.IsZero():
		return EmailCommand{}, fmt.Errorf("%w: zero email command", ErrInvalidProposal)
	case strings.TrimSpace(subject) == "" || !utf8.ValidString(subject) ||
		utf8.RuneCountInString(subject) > values.MaxEmailSubjectRunes || strings.ContainsFunc(subject, notInSubject):
		return EmailCommand{}, fmt.Errorf("%w: email subject must be one line of 1-%d characters", ErrInvalidProposal,
			values.MaxEmailSubjectRunes)
	}
	c.subject = subject
	return c, nil
}

func notInSubject(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
}

// Subject returns the email subject; empty when the core set none and the mailer uses its default.
func (c EmailCommand) Subject() string { return c.subject }

// IdempotencyKey returns the key that makes a repeated send safe.
func (c EmailCommand) IdempotencyKey() string { return c.key }

// AssignmentID returns the assignment whose homework is sent.
func (c EmailCommand) AssignmentID() shared.AssignmentID { return c.assignment }

// Recipient returns the trusted recipient.
func (c EmailCommand) Recipient() Recipient { return c.recipient }

// Attachment returns the exported homework file.
func (c EmailCommand) Attachment() shared.ArtifactRef { return c.attachment }

// AttachmentDigest returns the content the user confirmed; the mailer sends the attachment only if it matches.
func (c EmailCommand) AttachmentDigest() Digest { return c.digest }

// ConfirmedAt returns when the user confirmed the sending; the delivery records it (AC-14).
func (c EmailCommand) ConfirmedAt() time.Time { return c.confirmed }

// IsZero reports whether c is the zero value, which no adapter may act on.
func (c EmailCommand) IsZero() bool { return c == EmailCommand{} }

// IdempotencyKey returns the key that makes a repeated export safe.
func (c ExportCommand) IdempotencyKey() string { return c.key }

// AssignmentID returns the assignment whose homework is exported.
func (c ExportCommand) AssignmentID() shared.AssignmentID { return c.assignment }

// Target returns the export file, derived by the core.
func (c ExportCommand) Target() shared.ArtifactRef { return c.target }

// Content returns the file to write: the trusted assignment title as the heading and the model's body
// (values.HomeworkMarkdown, the format of the draft export too).
func (c ExportCommand) Content() string { return c.content }

// IsZero reports whether c is the zero value, which no adapter may act on.
func (c ExportCommand) IsZero() bool { return c == ExportCommand{} }

// WithLesson returns c with what the event says about the lesson of the assignment: subject, kind, teacher and
// date, resolved by the core (values.NewLessonLabel). A transcript hint stays marked by its source. The zero command
// stays zero, so no adapter acts on it.
func (c CalendarCommand) WithLesson(l values.LessonLabel) CalendarCommand {
	if c.IsZero() {
		return CalendarCommand{}
	}
	c.lesson = l
	return c
}

// Lesson returns what the event names of the lesson; zero when nothing.
func (c CalendarCommand) Lesson() values.LessonLabel { return c.lesson }

// IdempotencyKey returns the key that makes a repeated upsert safe.
func (c CalendarCommand) IdempotencyKey() string { return c.key }

// AssignmentID returns the assignment the event belongs to; the adapter derives the stable event id from it.
func (c CalendarCommand) AssignmentID() shared.AssignmentID { return c.assignment }

// Title returns the event title.
func (c CalendarCommand) Title() string { return c.title }

// Due returns the deadline, in UTC.
func (c CalendarCommand) Due() time.Time { return c.due }

// IsZero reports whether c is the zero value, which no adapter may act on.
func (c CalendarCommand) IsZero() bool { return c == CalendarCommand{} }
