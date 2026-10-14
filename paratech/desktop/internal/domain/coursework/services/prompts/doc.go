// Package prompts is the prompt contract of the LLM operations summary, extract_homework_deadlines and
// homework_draft (PRD sections 9.1 and 13.3, AC-18): the trusted instructions, the JSON Schema of each answer and the
// strict checks that turn the untrusted answer into typed values. It is pure: no I/O, no clock, no ids.
//
// Separation of instructions and data. Instructions are constants of this package and never contain user data.
// The selected context (domain/coursework/services/contextselection) goes into LLMRequest.UntrustedData only, as one
// JSON object between the lines DataOpen and DataClose; encoding/json escapes '<' and '>', so a transcript cannot
// forge the closing marker. The instructions tell the model that this block is data, never instructions.
//
// Answers. The Hub contract has no schema field (ADR-0005): the schema is part of the instructions and the answer is
// checked here. ParseSummary, ParseExtraction and ParseDraft accept exactly one JSON object of the schema: no unknown,
// differently-cased or duplicate keys, all fields present, correct types, no trailing data, bounded sizes and item
// counts, no control or bidi-override characters, deadlines as RFC 3339 times near the lesson date. Error messages
// name fields, never model text. A draft holds a title and a body only; recipients, paths and calendars are never part
// of an answer (docs/refs/prompt-injection.md rule 2), and extracted deadlines are proposals, not actions.
package prompts
