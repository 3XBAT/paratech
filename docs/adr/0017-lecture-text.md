# ADR-0017: Lecture text in parts

- **Status:** Accepted
- **Date:** 2026-10-19
- **Source:** TASK-090 (owner's scope of 2026-10-19); ADR-0005, ADR-0010, TASK-086.

## Context
«Текст лекции» is the full readable text of a lesson: punctuation, paragraphs, short headings, filler removed, nothing
summarized. Its output is about as long as the transcript (a 3-hour lesson is 250–300 KB), far over one answer of the
model. The desktop sends `max_output_tokens = 0` (the alias maximum, unknown to the desktop).

## Decision
- **Parts** (`domain/coursework/services/lectureparts`): the segments are cut by size (7 KB of text) and time (6 min).
  With an assumed 4096 output tokens and Cyrillic at 2.5–3 characters per token, a part's answer is about a third of the
  limit. A 3-hour lesson is about 40 parts; beyond 64 both limits grow by the same factor.
- **Overlap is context only**: each part after the first gets the last ~25 s of the previous part as `context_before`,
  marked "do not output". The merge needs no LLM: blocks are joined in order, a first paragraph repeating the last one is
  dropped, a block with the same heading continues the previous one. The model names the start segment of a block
  (`from`); it is clamped into the part and kept non-decreasing.
- **Cost bound**: at most parts + 3 paid calls per attempt: one per part plus at most 3 format reminders per attempt
  (`LectureTextReminders`). LLM keys are derived from the job and the request, so the requests a rerun asks again
  (reminders included) are replayed by the Hub for free.
- **No condensing**: an answer whose paragraphs keep under 45% of the runes of a part of 600+ runes (distinct segment
  texts counted once) is asked again with a reminder to keep all content, from the same budget
  (`lectureparts.Part.Condensed`). An empty answer (noise) is accepted.
- **Resumable**: the aggregate `coursework/lecturetext` stores each finished part (table `lecture_texts`, migration
  0016). An attempt that stops continues from the next part; a new job with the same requests (a retry) keeps them. The
  text shown is replaced only when a run completes. Job timeout = 67 calls × the Hub request timeout + slack.
- **Lesson kind** (`values.LessonKind`) only selects a constant sentence of the trusted instructions; the title and
  subject (schedule text) go into the untrusted data only.
  *Amended by TASK-093*: kind, teacher, lesson start and the next lesson are sent as untrusted data too (kind is a
  closed enum chosen by the app); the run id ignores the lesson context, so a schedule re-import keeps stored parts.
- Hub: operation `lecture_text` with its own operation label in the quota ledger (quota is shared per key; Hub
  migration 0005).

## Consequences
- A long lesson takes tens of minutes of calls; each part holds a light worker for one step only (TASK-097).
- The part size is a guess about the alias limit: a truncated part is asked again to keep all content; if it still
  does not fit, the Hub owner raises `max_output` of the alias.
- The kind and subject come from the recording's lesson snapshot (TASK-088/089), else from the lesson; one place in
  `homeworkflow` load.
- Once a lecture text job of the current transcript completed (`lectureTextReady`), the recordings row offers the lecture
  text instead of the raw transcript. The transcript is never deleted: retries are made from it.

## Amendment: one part per job step (TASK-097)
- A 3-hour lesson held one of the two workers for over two hours, and drafts, emails and the calendar waited. Now one
  lecture_text job still makes the whole text, but each claim formats and stores only the first part not stored
  (`lecturetext.NextPart`). While parts are left the handler returns `job.ErrContinueLater` and the runner calls
  `job.Yield`: QUEUED, due now, the attempt given back, the progress kept. The claim order (`next_run_at`,
  `created_at`) runs the jobs that were due before it first. The step that stores the last part publishes the text and
  completes the job.
- One job means one part at a time, the same keys, the same «Повторить» and «Сделать текст лекции заново», and the
  deletion cancels it like any job. A crash or a failed step repeats only the part not stored yet.
- Attempts count failed steps of the whole job. The reminder budget (`LectureTextReminders`) and the timeout are per
  step: at most 1 + 3 paid calls per step, so a lecture whose every part needs all reminders costs up to 4 × parts.
- Between steps the job is QUEUED with its checkpoint "N/M"; views that should show it as in progress read the
  checkpoint.
