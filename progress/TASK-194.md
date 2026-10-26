# TASK-194 (PAUSED)

## Done
- Implementation committed (401ec26f): TurnkeyWriting/TurnkeySection DTOs carry presentation/lab data
  (presentation, template, questions, talkMaterialId, talkResult, lab, manual, placeholders; kind, part, bullets, columns).
  Files: input/wails/turnkey.go, turnkey_test.go. No TS change (hand-written parsers; TASK-195 adds them).
- Verified by implementer: go test -race wails pkg, GOOS=windows go vet ./desktop/..., lint on wails pkg, architecture test.
- Changed existing test: TestTurnkeyWritingJSON (expected JSON gets the new keys; old values unchanged).
- Decision to confirm: new fields always present in JSON (no omitempty); false/""/[] for ordinary works.

## Left
- reviewer + security-auditor (opus) in parallel, fix findings.
- tasks.json status, docs/qa/tasks/TASK-194.md, final progress file, commit.
- fetch, bring in master, push branch (explicit refspec: upstream is origin/master), PR, audit-pr.sh.

## Next step
Run reviewer and security-auditor on the branch diff.
