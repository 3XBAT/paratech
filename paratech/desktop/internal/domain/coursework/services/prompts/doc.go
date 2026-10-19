// Package prompts is the prompt contract of the LLM operations summary, extract_homework_deadlines, homework_draft
// and lecture_text (PRD sections 9.1 and 13.3, AC-18, TASK-090): the trusted instructions, the JSON Schema of each answer and the
// strict checks that turn the untrusted answer into typed values. It is pure: no I/O, no clock, no ids.
//
// Separation of instructions and data. Instructions are constants of this package and never contain user data.
// The selected context (domain/coursework/services/contextselection) goes into LLMRequest.UntrustedData only, as one
// JSON object between the lines DataOpen and DataClose; encoding/json escapes '<' and '>', so a transcript cannot
// forge the closing marker. The instructions tell the model that this block is data, never instructions. The
// student's own instructions of a homework draft (TASK-094) follow in a section of their own between StudentOpen and
// StudentClose, one JSON object too: the model follows them about the homework text only, never about the answer
// format or actions, and nothing in the data can open such a section.
//
// Answers. The Hub contract has no schema field (ADR-0005): the schema is part of the instructions and the answer is
// checked here. A code fence or a sentence around the object is removed first (TASK-086), a second object or an array
// is not; an object that never ends is reported as truncated (ErrTruncatedOutput). ParseSummary, ParseExtraction and
// ParseDraft then accept exactly one JSON object of the schema: no unknown,
// differently-cased or duplicate keys, all fields present, correct types, no trailing data, bounded sizes and item
// counts, no control or bidi-override characters, deadlines as RFC 3339 times near the lesson date. Error messages
// name fields, never model text. A draft holds a title and a body only; recipients, paths and calendars are never part
// of an answer (docs/refs/prompt-injection.md rule 2), and extracted deadlines are proposals, not actions.
//
// lecture_text is sent per part of the transcript (BuildLectureText, LectureTextVersion): the segments with their
// indexes and start times, the end of the previous part as context and the lesson title and subject, all as untrusted
// data. The lesson kind only selects a constant sentence of the instructions (LectureTextInstructions).
package prompts
