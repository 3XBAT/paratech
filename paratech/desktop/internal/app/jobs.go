package app

import (
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/schedulesync"
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

	// calendarSyncTimeout covers one run of up to 1000 lessons with a few Calendar API requests each; a stuck run is
	// retried, and every write is idempotent.
	calendarSyncTimeout = 30 * time.Minute
)

// modelAlias is the Hub model alias of every LLM request.
const modelAlias = "default"

// jobHandlers are the use cases that run jobs.
type jobHandlers struct {
	stt         jobrunner.Handler // nil without a usable speech recognition engine
	postProcess jobrunner.Handler // summarize and extract_homework
	drafts      jobrunner.Handler
	delivery    deliveryJobs
	calendar    jobrunner.Handler // the calendar sync of the schedule
}

// deliveryJobs runs and reconciles the email delivery job.
type deliveryJobs interface {
	jobrunner.Handler
	jobrunner.Reconciler
}

// sttRegistration is the stt job type: local CPU work without side effects, so it waits for AC power and is rerun
// after an interruption. The speech activator registers it once the speech component verified (jobrunner Activate).
func sttRegistration(h jobrunner.Handler) jobrunner.Registration {
	return jobrunner.Registration{
		Type: transcribe.JobType, Handler: h, Heavy: true, Recovery: jobrunner.RecoveryRequeue, Timeout: sttTimeout,
	}
}

// registrations binds every job type to its handler and policies (PRD sections 7.2-7.4, hard rule 7). Without a
// speech recognition engine the stt type is left out: the runner claims and recovers only registered types, so STT
// jobs wait QUEUED (their keys stay usable) until the speech component is activated (sttRegistration).
func registrations(h jobHandlers) []jobrunner.Registration {
	llm := func(t job.Type, handler jobrunner.Handler) jobrunner.Registration {
		// Light: they wait on the Hub, not the CPU. Requeue: the LLM call is idempotent by its key.
		return jobrunner.Registration{Type: t, Handler: handler, Recovery: jobrunner.RecoveryRequeue, Timeout: llmJobTimeout}
	}
	var regs []jobrunner.Registration
	if h.stt != nil {
		regs = append(regs, sttRegistration(h.stt))
	}
	return append(regs,
		llm(homeworkflow.SummarizeJobType, h.postProcess),
		llm(homeworkflow.ExtractHomeworkJobType, h.postProcess),
		llm(homeworkflow.HomeworkDraftJobType, h.drafts),
		// An external side effect: never Heavy (a flapping power source must not loop it), never rerun blindly.
		jobrunner.Registration{
			Type: senddelivery.JobType, Handler: h.delivery, Reconciler: h.delivery, Recovery: jobrunner.RecoveryReconcile,
			Uncancellable: true, Timeout: sendEmailTimeout,
		},
		// An external side effect, but idempotent: upserts and deletes by stable event ids (PRD section 14 case 8), so
		// an interrupted run is requeued and writes again. Never Heavy: it waits on Google, not the CPU.
		jobrunner.Registration{
			Type: schedulesync.CalendarSyncJobType, Handler: h.calendar, Recovery: jobrunner.RecoveryRequeue,
			Timeout: calendarSyncTimeout,
		},
	)
}
