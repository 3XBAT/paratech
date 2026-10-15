package homeworkdraft

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Limits of a version. They are those of the draft answer (domain/coursework/services/prompts), so every valid answer
// fits, and a body in 4-byte runes still fits the previous-draft field of the LLM context and an export proposal.
const (
	MaxTitleRunes = 200
	MaxBodyRunes  = 16000
)

// Origin tells how a version was made.
type Origin string

// Origins of a version.
const (
	OriginGenerated Origin = "generated" // the answer of a homework_draft job
	OriginEdited    Origin = "edited"    // written or changed by the student
)

// Valid reports whether o is a known origin.
func (o Origin) Valid() bool { return o == OriginGenerated || o == OriginEdited }

// Revision is one version of the draft, an entity of the aggregate: it lives only inside a Draft and never changes
// once added.
type Revision struct {
	number int
	origin Origin
	title  string
	body   string

	// Generated versions only.
	instruction   string       // the student's instructions of the generation; may be empty
	promptVersion int          // the prompts version of the generation, at least 1
	job           shared.JobID // the job that generated it; one version per job

	createdAt time.Time
}

// Number returns the version number, from 1.
func (r Revision) Number() int { return r.number }

// Origin returns how the version was made.
func (r Revision) Origin() Origin { return r.origin }

// Title returns the one-line title.
func (r Revision) Title() string { return r.title }

// Body returns the homework text.
func (r Revision) Body() string { return r.body }

// Instruction returns the student's instructions of a generated version; empty for an edited one.
func (r Revision) Instruction() string { return r.instruction }

// PromptVersion returns the prompts version of a generated version; 0 for an edited one.
func (r Revision) PromptVersion() int { return r.promptVersion }

// Job returns the job that generated the version; empty for an edited one.
func (r Revision) Job() shared.JobID { return r.job }

// CreatedAt returns when the version was added.
func (r Revision) CreatedAt() time.Time { return r.createdAt }

// Markdown renders the version as the exported homework file (values.HomeworkMarkdown, the format of every export).
func (r Revision) Markdown() string { return values.HomeworkMarkdown(r.title, r.body) }

// validate checks a version and normalises its texts; the result is what the draft stores.
func (r Revision) validate() (Revision, error) {
	bad := func(format string, args ...any) (Revision, error) {
		return Revision{}, fmt.Errorf("%w: version %d: %s", ErrInvalidDraft, r.number, fmt.Sprintf(format, args...))
	}
	if r.number < 1 {
		return bad("number must be at least 1")
	}
	if r.createdAt.IsZero() {
		return bad("missing time")
	}
	var err error
	if r.title, r.body, err = checkContent(r.title, r.body); err != nil {
		return Revision{}, fmt.Errorf("%w: version %d: %w", ErrInvalidDraft, r.number, err)
	}

	switch r.origin {
	case OriginGenerated:
		if err := r.job.Validate(); err != nil {
			return Revision{}, fmt.Errorf("%w: version %d: %w", ErrInvalidDraft, r.number, err)
		}
		if r.promptVersion < 1 {
			return bad("generated without a prompt version")
		}
		if r.instruction, err = values.CleanInstruction(r.instruction); err != nil {
			return Revision{}, fmt.Errorf("%w: version %d: %w", ErrInvalidDraft, r.number, err)
		}
	case OriginEdited:
		if r.job != "" || r.promptVersion != 0 || r.instruction != "" {
			return bad("an edited version has no job, prompt version or instruction")
		}
	default:
		return bad("unknown origin")
	}
	return r, nil
}

// checkContent trims and checks a title (one line of 1 to MaxTitleRunes characters) and a body (1 to MaxBodyRunes
// characters, line breaks and tabs allowed).
func checkContent(title, body string) (string, string, error) {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	switch {
	case title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > MaxTitleRunes ||
		strings.ContainsFunc(title, notInLine):
		return "", "", fmt.Errorf("%w: title must be one line of 1-%d characters without control characters", ErrInvalidContent, MaxTitleRunes)
	case body == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > MaxBodyRunes ||
		strings.ContainsFunc(body, badControl):
		return "", "", fmt.Errorf("%w: body must be 1-%d characters without control characters", ErrInvalidContent, MaxBodyRunes)
	}
	return title, body, nil
}

func badControl(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }

// notInLine also rejects the line and paragraph separators, which break a line in many renderers.
func notInLine(r rune) bool { return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 }
