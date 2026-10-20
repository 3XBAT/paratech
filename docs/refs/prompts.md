# Prompts: how to change them and check quality

Source: TASK-093. Code: `desktop/internal/domain/coursework/services/prompts` (instructions, schemas, parsers),
`contextselection` (what is sent), `schedule/services/nextlesson` (the next lesson of the subject).

## What a request holds
- Trusted instructions: constants of `prompts` (English, Russian phrasings quoted as examples) + the JSON Schema.
- Untrusted data between `<untrusted_data>` markers: the transcript and the lesson context (title, subject, kind,
  teacher, `lesson_start`, `next_lesson_start`, `next_lesson_kind`). "К следующему занятию" resolves to
  `next_lesson_start`; "к следующему семинару" only when `next_lesson_kind` is `seminar`, else `due` is null.
- The lesson kind (closed enum) also picks one constant sentence of the summary and lecture text instructions.

## Summary (TASK-101, prompts v5)
- Answer: `sections[]` (`heading` one line, `text` 1-3 paragraphs, `points[]` may be empty) and `key_points`. Limits:
  20 sections, heading 200, text 4000, 10 points of 300, all sections together 20000 characters (the same constants
  in `prompts` and `summary`).
- Volume: about one section per 10-15 minutes; the data carries `transcript_minutes` (end of the last segment). The
  desktop does not know the Hub output limit and assumes about 4096 tokens (as `lectureparts`), so the prompt keeps
  all sections under about 8000 characters. A cut-off answer is asked again shorter; a long lesson that still does
  not fit needs a larger `max_output` of the alias on the Hub.
- Stored by sections (migration 0020, `summaries.sections`); `body` keeps the plain text (headings with a colon,
  texts, points as `- `), used for search, copy and the draft context. Summaries made before stay plain text, shown
  as one section without heading. «Сделать конспект заново» gives the new format.

## Changing a prompt
1. Edit the constants in `prompts.go`, `lesson.go` or `lecturetext.go`. Keep the safety rules of the preamble
   (`docs/refs/prompt-injection.md`); never put user data into the instructions.
2. Bump `prompts.Version` (summary, extraction, draft) or `prompts.LectureTextVersion` (lecture text). The version is
   in the job keys and the log lines `post-processing answer accepted/rejected` (`prompts_version`,
   `lecture_text_version`), so runs can be compared.
3. A schema change must keep `schema_test.go` green: the schema and the parser limits are the same constants.
4. Request size: instructions + data at most 1 MiB (`values.MaxRequestPayloadSize`); keep rules short.

## Checking quality (eval)
Cases: `desktop/tests/testdata/prompteval/*.json`, synthetic lessons with the expected assignments (keywords, `due` or
null) and the expected summary sections in lesson order (`summary_sections`, keywords per section). Only invented
subjects and names, no contacts. Add a case for every bug found in real use.

Without a Hub (always in CI): `go -C paratech test ./desktop/tests -run PromptEval -count=1`.

Against a Hub (paid calls, about one per case; never in CI). PowerShell:

```
$env:PARATECH_EVAL_HUB_URL="https://hub.example"; $env:PARATECH_EVAL_HUB_KEY="<tester key>"
go -C paratech test ./desktop/tests -run TestPromptEvalHub -v -count=1
```

`PARATECH_EVAL_MODEL` sets the model alias (default `default`). The key is read from the variable only, never from the
Credential Manager. Output: a table per case (`found / missed / extra / due ok`) with the prompts version, and a
summary table (`found / missed / sections / key points / in order`). The same
prompt is replayed by the Hub for free (the key is a digest of the request); a changed prompt is asked anew.
