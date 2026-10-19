# ADR-0018: Deleting recordings

- **Status:** Accepted
- **Date:** 2026-10-19
- **Source:** TASK-095; extends ADR-0004(a) item 2.

## Context
The user deletes one or several recordings with everything made from them: sound, transcript and its live parts,
summary, lecture text, jobs, and the found homework they choose. Jobs may be running, files may be busy, the app may
crash in the middle, and Google may hold deadline events of the deleted homework.

## Decision
- **Mark first.** `recording.MarkDeleting` stores a deletion mark and the chosen assignments in one transaction
  (`recordings.deleting_at`, `deleting_assignments`, migration 0017). A marked recording is hidden from the lists, never
  transcribed or assembled. A recording in progress is refused.
- **Jobs.** The unfinished jobs of the recording (by recording, by its transcripts, draft jobs of the chosen
  assignments) are cancelled through `jobrunner.Service.Cancel`; the use case looks again until none is unfinished,
  at most 20 s. Then the files go through the Storage Manager.
- **Purge in one transaction.** It checks again that no job is unfinished (BEGIN IMMEDIATE: nothing is claimed or
  enqueued meanwhile) and removes parts, summaries, lecture texts, transcripts, the chosen assignments with drafts,
  the finished jobs and the recording row (version guarded). This widens the exception of ADR-0004(a) item 2 from
  "a recording with its transcripts" to "a recording with what was made from it".
- **Homework.** Plain ones are offered checked; ones with a draft only when checked. Ones with email history are
  never deleted: `deliveries.assignment_id` is RESTRICT and reprocessing relies on it, so they stay without their
  recording (transcript link NULL) and the sent mail stays in history.
- **Google.** Each deleted assignment leaves a row in `deadline_event_removals` in the purge transaction. The calendar
  sync deletes the deadline event (404/410 = success) and then the row, in every run, before the lessons. A sync is
  requested after a deletion; no Google call runs in a transaction.
- **Crash.** The app resumes marked recordings at start, after the job recovery and before the recording recovery
  and the workers. A job that does not stop or a busy file leaves the recording marked for the next start.

## Consequences
- A deletion may block its call up to the wait while a running job stops.
- An assignment deleted while its deadline proposal is being executed may leave an event in Google.
- Export files of a deleted assignment that was never sent stay in `exports/`.
