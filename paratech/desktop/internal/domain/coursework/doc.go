// Package coursework is the bounded context of homework: assignments extracted from lessons, drafts generated
// with the cloud LLM and the minimal context sent to it (PRD sections 9 and 10, AC-13, AC-18).
// Aggregates: domain/coursework/assignment (with its deadline), domain/coursework/summary (one per transcript),
// domain/coursework/proposal (the side-effect boundary, PRD section 13.3), domain/coursework/homeworkdraft (the draft
// versions of one assignment), domain/coursework/lecturetext (the full readable text of one transcript, TASK-090).
// Value objects shared by aggregates live in domain/coursework/values. Domain services:
// domain/coursework/services/contextselection (the minimal context of an LLM operation, PRD section 9.1),
// domain/coursework/services/prompts (instructions, answer schemas and strict answer parsing),
// domain/coursework/services/lectureparts (the parts of the lecture text and their merge) and
// domain/coursework/services/summaryexport (the summary and the lecture text as text or Markdown, a whole subject as
// Markdown) and domain/coursework/services/knowledgebase (subjects, lessons and the search of the knowledge base).
//
// Layout of a context (docs/architecture.md section 3.1):
//
//	coursework/doc.go                  this file: purpose and glossary
//	coursework/<aggregate>/            one package per aggregate
//	coursework/values/                 value objects shared by several aggregates of the context
//	coursework/services/<service>/     domain services and policies
//
// Rules that apply: ARCH-10..19. The context imports only std, itself and domain/shared.
//
// # Glossary
//
//   - Assignment: a homework item with an optional deadline; the local copy is the system of record. An assignment the
//     user edited is never overwritten by reprocessing.
//   - Deadline: the due time of an assignment; not a separate record.
//   - Summary: the LLM summary of one lesson transcript, with key points.
//   - Post-processing: the summarize, extract_homework and lecture_text jobs that run after a transcript is stored.
//   - Lecture text: the full text of a lesson as said, with punctuation, paragraphs and short headings; not a summary.
//   - Draft: an LLM-generated homework text that the user reviews, edits and refines before sending.
//   - Draft version: one numbered, unchangeable state of a draft, generated or edited; a change starts from the latest.
//   - LLM request: the operation, the trusted instructions and the untrusted user data sent to the Hub.
//   - Untrusted data: transcripts and LLM output; it never triggers a side effect by itself (hard rule 3, PRD section 13.3).
//   - Action proposal: a structured action the LLM suggests (send homework, export it, add a deadline event). It is
//     validated against trusted local state and confirmed by the user before any adapter acts (AC-14, AC-18).
//   - Trusted target: a recipient, file or calendar chosen by the core from local state, never named by the model.
//   - Command: the validated, confirmed form of a proposal; the only input side-effect ports accept.
//   - Minimal context: only the local data an operation needs is sent to the LLM (PRD section 9.1).
package coursework
