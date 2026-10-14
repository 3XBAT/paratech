// Package homeworkflow is the use case of AI post-processing and homework: extracting assignments and deadlines from a
// transcript, generating, editing and refining drafts with the cloud LLM (PRD sections 9 and 10, AC-11, AC-13, AC-18).
//
// TASK-040 adds the Service with the action proposal pipeline (proposals.go): Propose parses untrusted model output and
// validates it against local state, Confirm records the user's approval, Execute hands a command of a confirmed
// proposal to the email, export or calendar port. TASK-042 adds the post-processing of a transcript (PostProcessor in
// postprocess.go: the summarize and extract_homework jobs, enqueued on the STT completion event) and the user's edits of
// the extracted assignments and deadlines (Service.EditAssignment), which reprocessing never overwrites. Drafts
// (TASK-043, with the draft repository and the homeworkdraft aggregate) extend the Service.
//
// The PostProcessor has no side-effect port at all: summaries, assignments and deadlines are local data. Putting a
// deadline into the calendar or sending homework goes through an action proposal and the user's confirmation.
//
// Rules that apply: ARCH-20..27. LLM output is untrusted (hard rule 3): the LLM proposes, the core validates against
// trusted local state, the user confirms, and only then does an adapter act. The side-effect ports accept only commands
// of a confirmed proposal (domain/coursework/proposal), never model text. See docs/refs/prompt-injection.md.
package homeworkflow
