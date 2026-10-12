// Package values holds value objects shared by several aggregates of the coursework context.
//
// It currently holds the LLM request and result. They exist because the LLMClient port of the homeworkflow
// use case must be expressed in domain types (ARCH-26, ARCH-36: no shared/gen types cross a port).
// LLMRequest is validated at construction; the model answer is typed as UntrustedOutput (hard rule 3) so it
// cannot reach a plain string parameter by accident. The operation catalog and the real prompts arrive with TASK-041.
package values
