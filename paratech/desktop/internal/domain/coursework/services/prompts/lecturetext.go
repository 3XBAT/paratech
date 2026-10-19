package prompts

import (
	"encoding/json"
	"fmt"

	"paratech/desktop/internal/domain/coursework/values"
)

// LectureTextVersion identifies the lecture_text prompt, data layout and schema (TASK-090), apart from Version so that
// a change of one does not redo the other. Bump it on any change of them.
const LectureTextVersion = 1

const lectureTextTask = `Task: turn the speech-recognition text of one part of a lesson into the full readable text of that part. The field segments holds the recognised segments of this part in order; each has an index i, its start time at (minutes:seconds from the start of the recording) and its text. context_before, if present, is the end of the previous part, only for continuity: do not output it and do not repeat its content. part and parts tell which part of the lesson this is; lesson_title and subject, if present, name the lesson.

This is not a summary. Write the text as it was said, in the speaker's order and in the language of the transcript:
- keep all the content: every explanation, definition, example, number, formula, question, answer, remark and digression; do not summarize, shorten, condense or leave anything out, and do not add anything that was not said;
- add punctuation and capital letters and split the text into paragraphs;
- remove filler words, false starts, stutters and immediate repetitions;
- fix an obvious recognition error only when the right word is clear from the context; otherwise keep the words as recognised;
- group the paragraphs into logical blocks; give each block a short heading of a few words that names its topic, and set from to the index i of the segment where the block starts.
The part may begin or end in the middle of a sentence or a topic: start with the first segment of this part, end with its last one, and write no introduction or conclusion of your own. If the part holds nothing but noise or silence, answer with an empty blocks list.

`

// Wording of the lesson kinds. The kind is chosen by the app (values.LessonKind), never read from the data, and it
// changes only how the text is arranged.
const (
	lectureKindNote = ` The kind of the lesson changes only how you arrange the text, never what it contains.

`
	kindLecture  = `The lesson is a lecture: keep the lecturer's explanations, definitions, examples and digressions as said.`
	kindSeminar  = `The lesson is a seminar: keep the questions, the answers and the discussion as said, and keep each problem and the steps of its solution as said.`
	kindPractice = `The lesson is a practical class: keep each problem or exercise, the steps of its solution and the results as said.`
	kindLab      = `The lesson is a laboratory class: keep the setup, the steps of the procedure, the measurements and the results as said.`
)

// LectureTextInstructions returns the trusted instructions of a lecture_text request for a lesson of kind kind: constant
// text with the answer schema. An unknown or invalid kind adds no wording.
func LectureTextInstructions(kind values.LessonKind) string {
	note := ""
	switch kind {
	case values.LessonKindLecture:
		note = kindLecture + lectureKindNote
	case values.LessonKindSeminar:
		note = kindSeminar + lectureKindNote
	case values.LessonKindPractice:
		note = kindPractice + lectureKindNote
	case values.LessonKindLab:
		note = kindLab + lectureKindNote
	}
	return preamble + lectureTextTask + note + answerRule + lectureTextSchema
}

// LectureSegment is one recognised segment of a part of the lecture text.
type LectureSegment struct {
	Index   int   // position in the transcript; the answer refers to it in from
	StartMs int64 // start in the recording
	Text    string
}

// LecturePart is what one lecture_text request is made of. Every text field is user data and goes into the untrusted
// data only; Kind selects trusted wording and is not sent as data.
type LecturePart struct {
	LessonTitle   string
	Subject       string
	Kind          values.LessonKind
	Part, Parts   int    // 1-based number of the part and the number of parts
	ContextBefore string // the end of the previous part, for continuity only; empty for the first part
	Segments      []LectureSegment
}

type lectureSegmentData struct {
	I    int    `json:"i"`
	At   string `json:"at"`
	Text string `json:"text"`
}

type lectureTextData struct {
	LessonTitle   string               `json:"lesson_title,omitempty"`
	Subject       string               `json:"subject,omitempty"`
	Part          int                  `json:"part"`
	Parts         int                  `json:"parts"`
	ContextBefore string               `json:"context_before,omitempty"`
	Segments      []lectureSegmentData `json:"segments"`
}

// BuildLectureText builds the request of one part: the constant LectureTextInstructions(p.Kind) and the part as one
// JSON object between DataOpen and DataClose. The result is deterministic, so a retry of the same part sends the same
// request. Errors wrap ErrNoContext (no segments or a bad part number) or values.ErrInvalidLLMRequest.
func BuildLectureText(p LecturePart, modelAlias, idempotencyKey string) (values.LLMRequest, error) {
	if len(p.Segments) == 0 || p.Parts < 1 || p.Part < 1 || p.Part > p.Parts {
		return values.LLMRequest{}, fmt.Errorf("%w: part %d of %d with %d segments", ErrNoContext, p.Part, p.Parts, len(p.Segments))
	}
	data := lectureTextData{
		LessonTitle: p.LessonTitle, Subject: p.Subject, Part: p.Part, Parts: p.Parts, ContextBefore: p.ContextBefore,
		Segments: make([]lectureSegmentData, len(p.Segments)),
	}
	for i, s := range p.Segments {
		data.Segments[i] = lectureSegmentData{I: s.Index, At: clock(s.StartMs), Text: s.Text}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return values.LLMRequest{}, fmt.Errorf("%w: encode the part: %w", ErrNoContext, err)
	}
	return values.NewLLMRequest(values.OperationLectureText, modelAlias, LectureTextInstructions(p.Kind),
		DataOpen+"\n"+string(b)+"\n"+DataClose, idempotencyKey)
}

// clock renders a time in the recording as minutes:seconds; the minutes go past 59 in a long lesson.
func clock(ms int64) string {
	s := max(ms, 0) / 1000
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// LectureBlock is one block of a lecture_text answer: model-written data, never an instruction.
type LectureBlock struct {
	heading    string
	from       int
	paragraphs []string
}

// Heading returns the one-line heading.
func (b LectureBlock) Heading() string { return b.heading }

// From returns the segment index the model says the block starts at; the caller checks it against the part.
func (b LectureBlock) From() int { return b.from }

// Paragraphs returns the paragraphs.
func (b LectureBlock) Paragraphs() []string { return append([]string(nil), b.paragraphs...) }

// LectureText is the validated answer of a lecture_text request; it may hold no block (a part without speech).
type LectureText struct {
	blocks []LectureBlock
}

// Blocks returns the blocks in the order of the answer.
func (t LectureText) Blocks() []LectureBlock { return append([]LectureBlock(nil), t.blocks...) }

type lectureTextWire struct {
	Blocks *[]lectureBlockWire `json:"blocks"`
}

type lectureBlockWire struct {
	Heading    *string   `json:"heading"`
	From       *int      `json:"from"`
	Paragraphs *[]string `json:"paragraphs"`
}

// ParseLectureText checks the answer of a lecture_text request: at most MaxLectureBlocks blocks, each with a one-line
// heading of 1 to MaxLectureHeadingRunes, an integer from of at least 0 and 1 to MaxLectureParagraphs paragraphs of 1
// to MaxLectureParagraphRunes. Whether from lies inside the part is left to the caller (it is clamped, not refused).
// Errors wrap ErrMalformedOutput, and ErrTruncatedOutput for an object that never ends.
func ParseLectureText(out values.UntrustedOutput) (LectureText, error) {
	var w lectureTextWire
	if err := decode(out, &w, "blocks", "heading", "from", "paragraphs"); err != nil {
		return LectureText{}, err
	}
	if w.Blocks == nil {
		return LectureText{}, malformed("blocks is required")
	}
	if len(*w.Blocks) > MaxLectureBlocks {
		return LectureText{}, malformed("more than %d blocks", MaxLectureBlocks)
	}

	blocks := make([]LectureBlock, 0, len(*w.Blocks))
	for i, bw := range *w.Blocks {
		b, err := checkLectureBlock(bw)
		if err != nil {
			return LectureText{}, fmt.Errorf("blocks[%d]: %w", i, err)
		}
		blocks = append(blocks, b)
	}
	return LectureText{blocks: blocks}, nil
}

func checkLectureBlock(w lectureBlockWire) (LectureBlock, error) {
	if w.Heading == nil || w.From == nil || w.Paragraphs == nil {
		return LectureBlock{}, malformed("heading, from and paragraphs are required")
	}
	heading, err := checkLine("heading", *w.Heading, MaxLectureHeadingRunes)
	if err != nil {
		return LectureBlock{}, err
	}
	if *w.From < 0 {
		return LectureBlock{}, malformed("from must not be negative")
	}
	if n := len(*w.Paragraphs); n == 0 || n > MaxLectureParagraphs {
		return LectureBlock{}, malformed("paragraphs must hold 1-%d items", MaxLectureParagraphs)
	}
	paragraphs := make([]string, 0, len(*w.Paragraphs))
	for i, p := range *w.Paragraphs {
		text, err := checkText(fmt.Sprintf("paragraphs[%d]", i), p, MaxLectureParagraphRunes, false)
		if err != nil {
			return LectureBlock{}, err
		}
		paragraphs = append(paragraphs, text)
	}
	return LectureBlock{heading: heading, from: *w.From, paragraphs: paragraphs}, nil
}
