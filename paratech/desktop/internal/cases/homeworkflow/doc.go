// Package homeworkflow is the use case of AI post-processing and homework: extracting assignments and deadlines from a
// transcript, generating, editing and refining drafts with the cloud LLM (PRD sections 9 and 10, AC-11, AC-13, AC-18).
//
// TASK-003 contains only the ports (ports.go). The Service arrives with TASK-042 (post-processing) and TASK-043
// (drafts); the draft repository is added by TASK-043 together with the homeworkdraft aggregate.
//
// Rules that apply: ARCH-20..27. LLM output is untrusted (hard rule 3): the LLM proposes, the core validates against
// trusted local state, policy or the user confirms, and only then does an adapter act. No port here performs side
// effects on behalf of the model.
package homeworkflow
