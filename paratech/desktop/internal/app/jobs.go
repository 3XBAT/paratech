package app

import (
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/cases/transcribe"
	"paratech/desktop/internal/domain/jobs/job"
)

// Attempt limits of the job types.
const (
	// sttTimeout covers a CPU-only transcription of the longest recording on a slow laptop.
	sttTimeout = 6 * time.Hour

	// llmJobTimeout leaves room for a Hub call that ends at its own request timeout and a retry inside the attempt.
	llmJobTimeout = 5 * hubclient.DefaultRequestTimeout

	// sendEmailTimeout is above twice the SMTP session timeout: the smtp adapter's deadline covers the session setup
	// and then the transaction separately. A configured smtp.Config.Timeout must keep this rule.
	sendEmailTimeout = 2*smtp.DefaultTimeout + time.Minute
)

// modelAlias is the Hub model alias of every LLM request.
const modelAlias = "default"

// jobHandlers are the use cases that run jobs.
type jobHandlers struct {
	stt         jobrunner.Handler
	postProcess jobrunner.Handler // summarize and extract_homework
	drafts      jobrunner.Handler
	delivery    deliveryJobs
}

// deliveryJobs runs and reconciles the email delivery job.
type deliveryJobs interface {
	jobrunner.Handler
	jobrunner.Reconciler
}

// registrations binds every job type to its handler and policies (PRD sections 7.2-7.4, hard rule 7).
func registrations(h jobHandlers) []jobrunner.Registration {
	llm := func(t job.Type, handler jobrunner.Handler) jobrunner.Registration {
		// Light: they wait on the Hub, not the CPU. Requeue: the LLM call is idempotent by its key.
		return jobrunner.Registration{Type: t, Handler: handler, Recovery: jobrunner.RecoveryRequeue, Timeout: llmJobTimeout}
	}
	return []jobrunner.Registration{
		// Local CPU work without side effects: waits for AC power and is rerun after an interruption.
		{Type: transcribe.JobType, Handler: h.stt, Heavy: true, Recovery: jobrunner.RecoveryRequeue, Timeout: sttTimeout},
		llm(homeworkflow.SummarizeJobType, h.postProcess),
		llm(homeworkflow.ExtractHomeworkJobType, h.postProcess),
		llm(homeworkflow.HomeworkDraftJobType, h.drafts),
		// An external side effect: never Heavy (a flapping power source must not loop it), never rerun blindly.
		{
			Type: senddelivery.JobType, Handler: h.delivery, Reconciler: h.delivery, Recovery: jobrunner.RecoveryReconcile,
			Uncancellable: true, Timeout: sendEmailTimeout,
		},
	}
}

// notifyTypes are the job types whose finish the user is told about (PRD section 15); email delivery has its own UI.
func notifyTypes() []job.Type {
	return []job.Type{
		transcribe.JobType, homeworkflow.SummarizeJobType, homeworkflow.ExtractHomeworkJobType, homeworkflow.HomeworkDraftJobType,
	}
}
