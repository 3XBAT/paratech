# Prompts: how to change them and check quality

Source: TASK-093. Code: `desktop/internal/domain/coursework/services/prompts` (instructions, schemas, parsers),
`contextselection` (what is sent), `schedule/services/nextlesson` (the next lesson of the subject).

## What a request holds
- Trusted instructions: constants of `prompts` (English, Russian phrasings quoted as examples) + the JSON Schema.
- Untrusted data between `<untrusted_data>` markers: the transcript and the lesson context (title, subject, kind,
  teacher, `lesson_start`, `next_lesson_start`). "К следующему занятию" resolves to `next_lesson_start`.
- The lesson kind (closed enum) also picks one constant sentence of the summary and lecture text instructions.

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
null). Only invented subjects and names, no contacts. Add a case for every bug found in real use.

Without a Hub (always in CI): `go -C paratech test ./desktop/tests -run PromptEval -count=1`.

Against a Hub (paid calls, about one per case; never in CI). PowerShell:

```
$env:PARATECH_EVAL_HUB_URL="https://hub.example"; $env:PARATECH_EVAL_HUB_KEY="<tester key>"
go -C paratech test ./desktop/tests -run TestPromptEvalHub -v -count=1
```

`PARATECH_EVAL_MODEL` sets the model alias (default `default`). The key is read from the variable only, never from the
Credential Manager. Output: a table per case (`found / missed / extra / due ok`) with the prompts version. The same
prompt is replayed by the Hub for free (the key is a digest of the request); a changed prompt is asked anew.
