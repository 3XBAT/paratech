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
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/jobs/job"
)

// Attempt limits of the job types.
const (
	// sttTimeout covers a CPU-only transcription of the longest recording on a slow laptop.
	sttTimeout = 6 * time.Hour

	// llmJobTimeout leaves room for a Hub call that ends at its own request timeout and a retry inside the attempt.
	// A homework draft makes one call.
	llmJobTimeout = 5 * hubclient.DefaultRequestTimeout

	// postProcessJobTimeout covers the worst case of a summarize or extract_homework run: AnswerCalls calls, each
	// with the room of one call.
	postProcessJobTimeout = homeworkflow.AnswerCalls * llmJobTimeout

	// lectureTextJobTimeout covers the worst case of one lecture_text step (one part, TASK-097): MaxLectureTextCalls
	// (the part and its reminders) one after the other, each ending at the Hub client's request timeout, plus the room
	// of one call for the local work. A step that still times out is retried from the same part.
	lectureTextJobTimeout = homeworkflow.MaxLectureTextCalls*hubclient.DefaultRequestTimeout + llmJobTimeout

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
	postProcess jobrunner.Handler // summarize, extract_homework and lecture_text
	drafts      jobrunner.Handler
	works       jobrunner.Handler // the writing of a turnkey work (TASK-131)
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

// hiddenJobTypes are the job types of background service work that involves no user; «Записи и задачи» leaves them
// out (TASK-074). The calendar sync runs after every trigger and its result is shown in the Google section instead.
// The work on a library material belongs to a request of the agent and is shown in its panel (TASK-122), as does the
// writing of a turnkey work (TASK-131, its progress card).
// STT, summaries and homework belong to a lesson: the activity use case shows them in the row of their recording
// (TASK-076); drafts and sending belong to an action of the user and stay listed.
func hiddenJobTypes() []job.Type {
	return []job.Type{
		schedulesync.CalendarSyncJobType, homeworkflow.MaterialSummaryJobType, homeworkflow.MaterialHomeworkJobType,
		turnkey.WritingJobType,
	}
}

// registrations binds every job type to its handler and policies (PRD sections 7.2-7.4, hard rule 7). Without a
// speech recognition engine the stt type is left out: the runner claims and recovers only registered types, so STT
// jobs wait QUEUED (their keys stay usable) until the speech component is activated (sttRegistration).
func registrations(h jobHandlers) []jobrunner.Registration {
	llm := func(t job.Type, handler jobrunner.Handler, timeout time.Duration) jobrunner.Registration {
		// Light: they wait on the Hub, not the CPU. Requeue: the LLM call is idempotent by its key.
		return jobrunner.Registration{Type: t, Handler: handler, Recovery: jobrunner.RecoveryRequeue, Timeout: timeout}
	}
	var regs []jobrunner.Registration
	if h.stt != nil {
		regs = append(regs, sttRegistration(h.stt))
	}
	return append(regs,
		llm(homeworkflow.SummarizeJobType, h.postProcess, postProcessJobTimeout),
		llm(homeworkflow.ExtractHomeworkJobType, h.postProcess, postProcessJobTimeout),
		llm(homeworkflow.LectureTextJobType, h.postProcess, lectureTextJobTimeout),
		llm(homeworkflow.MaterialSummaryJobType, h.postProcess, postProcessJobTimeout),
		llm(homeworkflow.MaterialHomeworkJobType, h.postProcess, postProcessJobTimeout),
		llm(homeworkflow.HomeworkDraftJobType, h.drafts, llmJobTimeout),
		// One step is one call (the outline or a section) or the local file; a step that times out is repeated
		// under the same key (a free replay).
		llm(turnkey.WritingJobType, h.works, llmJobTimeout),
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
