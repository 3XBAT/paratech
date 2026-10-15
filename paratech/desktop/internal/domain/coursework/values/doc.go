// Package values holds value objects shared by several aggregates of the coursework context.
//
// It currently holds the LLM request and result. They exist because the LLMClient port of the homeworkflow
// use case must be expressed in domain types (ARCH-26, ARCH-36: no shared/gen types cross a port).
// LLMRequest is validated at construction; the model answer is typed as UntrustedOutput (hard rule 3) so it
// cannot reach a plain string parameter by accident. The prompts, the answer schemas and the
// answer parsers of each operation live in domain/coursework/services/prompts. It also holds the job types and the
// job payload of the post-processing of a transcript (PostProcessRequest) and of a homework draft generation
// (DraftRequest).
package values
