package lecturetext

import (
	"fmt"
	"time"
	"unicode"

	"paratech/desktop/internal/domain/shared"
)

// Limits of the aggregate.
const (
	// MaxParts bounds the parts of one run; the planner (domain/coursework/services/lectureparts) never makes more.
	MaxParts = 64

	// MaxBlocks bounds the blocks of the text and, separately, of the finished parts of a run.
	MaxBlocks = 4096

	// MaxTextBytes bounds the text of the blocks and, separately, of the finished parts of a run: far above a 3-hour
	// lesson (about 300 KB), so a valid run always fits.
	MaxTextBytes = 4 << 20

	// MaxRunLen bounds the run id (a digest of the requests of the run).
	MaxRunLen = 128
)

// ID identifies a lecture text. It is the id of the transcript it was made from (IDOf).
type ID string

// IDOf returns the id of the lecture text of transcript t.
func IDOf(t shared.TranscriptID) ID { return ID(t) }

// String returns the identifier as a plain string.
func (id ID) String() string { return string(id) }

// Validate applies the rules of a transcript id, which a lecture text id is.
func (id ID) Validate() error { return shared.TranscriptID(id).Validate() }

// LectureText is the lecture text of one transcript and the run that makes it. Fields change only through the
// methods below.
type LectureText struct {
	version int64 // persisted version for optimistic concurrency; 0 = never stored (ADR-0004)
	id      ID

	transcript shared.TranscriptID // empty after the transcript was deleted (ON DELETE SET NULL)
	lesson     shared.LessonID     // optional

	// The text shown: empty until a run completed. promptVersion is the lecture text prompts version that made it.
	blocks        []Block
	promptVersion int

	// The run in progress: its id (a digest of its requests), its number of parts and the parts finished so far.
	run      string
	runParts int
	parts    [][]Block

	createdAt time.Time
	updatedAt time.Time
}

// Spec is what the creator of a lecture text provides to New.
type Spec struct {
	Transcript shared.TranscriptID
	Lesson     shared.LessonID // optional
}

// New creates the lecture text of a transcript with no text and no run, never stored (Version() == 0). Its id is
// IDOf(spec.Transcript).
func New(spec Spec, now time.Time) (*LectureText, error) {
	if err := spec.Transcript.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidLectureText, err)
	}
	l := &LectureText{
		id: IDOf(spec.Transcript), transcript: spec.Transcript, lesson: spec.Lesson, createdAt: now, updatedAt: now,
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	return l, nil
}

// State is the stored form of a lecture text, for Restore and the repositories.
type State struct {
	Version       int64
	ID            ID
	Transcript    shared.TranscriptID // empty: the transcript was deleted
	Lesson        shared.LessonID     // empty: none
	Blocks        []BlockState
	PromptVersion int // 0 without blocks
	Run           string
	RunParts      int
	Parts         [][]BlockState
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Restore rebuilds a stored lecture text without side effects; it only checks that the state is consistent.
func Restore(st State) (*LectureText, error) {
	if st.Version < 1 {
		return nil, fmt.Errorf("%w: stored version %d", ErrInvalidLectureText, st.Version)
	}
	if st.Transcript != "" && IDOf(st.Transcript) != st.ID {
		return nil, fmt.Errorf("%w: id differs from the transcript", ErrInvalidLectureText)
	}
	blocks, err := restoreBlocks(st.Blocks)
	if err != nil {
		return nil, err
	}
	if len(st.Parts) > MaxParts {
		return nil, fmt.Errorf("%w: more than %d parts", ErrInvalidLectureText, MaxParts)
	}
	parts := make([][]Block, 0, len(st.Parts))
	for _, p := range st.Parts {
		bs, err := restoreBlocks(p)
		if err != nil {
			return nil, err
		}
		parts = append(parts, bs)
	}
	l := &LectureText{
		version: st.Version, id: st.ID, transcript: st.Transcript, lesson: st.Lesson, blocks: blocks,
		promptVersion: st.PromptVersion, run: st.Run, runParts: st.RunParts, parts: parts,
		createdAt: st.CreatedAt, updatedAt: st.UpdatedAt,
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	return l, nil
}

func restoreBlocks(states []BlockState) ([]Block, error) {
	if len(states) > MaxBlocks {
		return nil, fmt.Errorf("%w: more than %d blocks", ErrInvalidLectureText, MaxBlocks)
	}
	out := make([]Block, 0, len(states))
	for i, s := range states {
		b, err := NewBlock(s.Heading, s.StartMs, s.Paragraphs)
		if err != nil {
			return nil, fmt.Errorf("%w: block %d: %w", ErrInvalidLectureText, i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

func (l *LectureText) validate() error {
	if err := l.id.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidLectureText, err)
	}
	if l.lesson != "" {
		if err := l.lesson.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidLectureText, err)
		}
	}
	if l.createdAt.IsZero() || l.updatedAt.IsZero() {
		return fmt.Errorf("%w: missing time", ErrInvalidLectureText)
	}
	if (len(l.blocks) > 0) != (l.promptVersion >= 1) || l.promptVersion < 0 {
		return fmt.Errorf("%w: prompt version %d with %d blocks", ErrInvalidLectureText, l.promptVersion, len(l.blocks))
	}
	if err := checkBlocks(l.blocks); err != nil {
		return err
	}

	if l.run == "" {
		if l.runParts != 0 || len(l.parts) != 0 {
			return fmt.Errorf("%w: parts without a run", ErrInvalidLectureText)
		}
		return nil
	}
	if !validRun(l.run) {
		return fmt.Errorf("%w: run id", ErrInvalidLectureText)
	}
	if l.runParts < 1 || l.runParts > MaxParts || len(l.parts) > l.runParts {
		return fmt.Errorf("%w: %d of %d parts", ErrInvalidLectureText, len(l.parts), l.runParts)
	}
	return checkBlocks(flatten(l.parts))
}

// checkBlocks bounds the blocks and requires their starts not to go back in time.
func checkBlocks(blocks []Block) error {
	if len(blocks) > MaxBlocks {
		return fmt.Errorf("%w: more than %d blocks", ErrInvalidLectureText, MaxBlocks)
	}
	size := 0
	for i, b := range blocks {
		if b.heading == "" {
			return fmt.Errorf("%w: block %d is empty", ErrInvalidLectureText, i)
		}
		if i > 0 && b.startMs < blocks[i-1].startMs {
			return fmt.Errorf("%w: block %d starts before block %d", ErrInvalidLectureText, i, i-1)
		}
		size += b.size()
	}
	if size > MaxTextBytes {
		return fmt.Errorf("%w: text is larger than %d bytes", ErrInvalidLectureText, MaxTextBytes)
	}
	return nil
}

func flatten(parts [][]Block) []Block {
	var out []Block
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// validRun accepts 1 to MaxRunLen printable ASCII characters without spaces.
func validRun(s string) bool {
	if s == "" || len(s) > MaxRunLen {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' ' {
			return false
		}
	}
	return true
}

// NextPart returns the index of the first part of run (of parts parts) that is not finished: 0 when another run, or
// none, is in progress. It equals parts when every part is finished.
func (l *LectureText) NextPart(run string, parts int) int {
	if l.run != run || l.runParts != parts {
		return 0
	}
	return len(l.parts)
}

// AddPart stores the blocks of part index of run, which has parts parts. A part of another run starts that run
// afresh: the parts of the run before are dropped (its requests changed, so its answers no longer fit). The part must
// be the next one (NextPart), else ErrPartOutOfOrder. The blocks of a part may be empty (nothing was said in it); the
// starts may not go back in time, also across parts.
func (l *LectureText) AddPart(run string, parts, index int, blocks []Block, now time.Time) error {
	if !validRun(run) || parts < 1 || parts > MaxParts || index < 0 || index >= parts {
		return fmt.Errorf("%w: part %d of %d", ErrInvalidLectureText, index, parts)
	}
	for i, b := range blocks {
		if b.heading == "" {
			return fmt.Errorf("%w: block %d is empty", ErrInvalidLectureText, i)
		}
	}

	next := l.NextPart(run, parts)
	if index != next {
		return fmt.Errorf("%w: part %d, the next one is %d", ErrPartOutOfOrder, index, next)
	}
	kept := l.parts
	if next == 0 {
		kept = nil
	}
	candidate := append(append([][]Block(nil), kept...), append([]Block(nil), blocks...))
	if err := checkBlocks(flatten(candidate)); err != nil {
		return err
	}
	l.run, l.runParts, l.parts, l.updatedAt = run, parts, candidate, now
	return nil
}

// Complete reports whether the run has all its parts, so its blocks can be merged and published.
func (l *LectureText) Complete() bool { return l.run != "" && len(l.parts) == l.runParts }

// Parts returns a copy of the finished parts of the run, in order.
func (l *LectureText) Parts() [][]Block {
	out := make([][]Block, len(l.parts))
	for i, p := range l.parts {
		out[i] = append([]Block(nil), p...)
	}
	return out
}

// Publish makes blocks, merged from the parts of the complete run run, the text shown, replacing the one before, and
// ends the run. blocks may not be empty. Errors: another or an incomplete run wraps ErrRunIncomplete.
func (l *LectureText) Publish(run string, blocks []Block, promptVersion int, now time.Time) error {
	if l.run != run || !l.Complete() {
		return fmt.Errorf("%w: %d of %d parts", ErrRunIncomplete, len(l.parts), l.runParts)
	}
	if len(blocks) == 0 || promptVersion < 1 {
		return fmt.Errorf("%w: no blocks or prompt version %d", ErrInvalidLectureText, promptVersion)
	}
	blocks = append([]Block(nil), blocks...)
	if err := checkBlocks(blocks); err != nil {
		return err
	}
	l.blocks, l.promptVersion = blocks, promptVersion
	l.run, l.runParts, l.parts, l.updatedAt = "", 0, nil, now
	return nil
}

// ID returns the lecture text identifier.
func (l *LectureText) ID() ID { return l.id }

// TranscriptID returns the transcript, empty if it was deleted.
func (l *LectureText) TranscriptID() shared.TranscriptID { return l.transcript }

// LessonID returns the lesson, empty if unknown.
func (l *LectureText) LessonID() shared.LessonID { return l.lesson }

// Ready reports whether a run completed, so Blocks holds the text.
func (l *LectureText) Ready() bool { return len(l.blocks) > 0 }

// Blocks returns a copy of the blocks of the text, empty until a run completed.
func (l *LectureText) Blocks() []Block { return append([]Block(nil), l.blocks...) }

// PromptVersion returns the prompts version that made the blocks; 0 without blocks.
func (l *LectureText) PromptVersion() int { return l.promptVersion }

// Run returns the id of the run in progress, its number of parts and how many are finished; "" without a run.
func (l *LectureText) Run() (run string, parts, done int) { return l.run, l.runParts, len(l.parts) }

// CreatedAt returns the creation time.
func (l *LectureText) CreatedAt() time.Time { return l.createdAt }

// UpdatedAt returns the time of the last change.
func (l *LectureText) UpdatedAt() time.Time { return l.updatedAt }

// State returns the stored form, for the repositories.
func (l *LectureText) State() State {
	st := State{
		Version: l.version, ID: l.id, Transcript: l.transcript, Lesson: l.lesson, PromptVersion: l.promptVersion,
		Run: l.run, RunParts: l.runParts, CreatedAt: l.createdAt, UpdatedAt: l.updatedAt,
		Blocks: blockStates(l.blocks),
	}
	for _, p := range l.parts {
		st.Parts = append(st.Parts, blockStates(p))
	}
	return st
}

func blockStates(blocks []Block) []BlockState {
	out := make([]BlockState, len(blocks))
	for i, b := range blocks {
		out[i] = b.State()
	}
	return out
}

// Version returns the version this instance was last loaded or saved with: 0 for an aggregate that was never
// stored, otherwise at least 1 (optimistic concurrency, ADR-0004).
func (l *LectureText) Version() int64 { return l.version }

// Persisted records the version the store assigned after a successful Save. It is called by the repository adapter
// only, after the write succeeded (ADR-0004).
func (l *LectureText) Persisted(version int64) { l.version = version }
