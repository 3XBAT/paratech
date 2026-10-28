# TASK-237 — send to the teacher of the subject (core)

## Done
- Domain `coursework/proposal/teachers.go` (own types, ARCH-11): `TeacherRole`, `RoleOf(lessonKind)`, `Teacher`, `Teachers`, `ChooseRecipient(teachers, kind)`, `ChosenRecipient(teachers, role, shownAddress)`, `RecipientView`. Reasons `no_subject`, `no_teacher_email` (REJECTED); `choose_teacher` is a view reason, never stored; `ReasonNoRecipient` kept so old rows restore. Types print redacted (fmt, slog).
- The aggregate decides: `Facts` carries `Teachers`, `Lesson`, `ChosenRole` (`Facts.Recipient` removed); `Validate`/`Recheck` call `ChooseRecipient`. A send awaiting a choice is VALIDATED without a recipient; `Confirm`/`ConfirmOnce` refuse it (`ErrChooseTeacher`); `ConfirmChoice(role, shown, now)` pins the shown address and confirms; `Recheck` with the role runs `ChosenRecipient` (changed or never-offered address: STALE). `Restore` allows no recipient only for VALIDATED/DECLINED. No schema change.
- `homeworkflow`: `RecipientSource` = `Teachers(ctx, subject)` + `SubjectNamed(ctx, name)` (ADR-0024 name resolution); subject = work's own, else the assignment's; lesson kind from the assignment (work: its assignment). `ConfirmSendTo`/`ConfirmSendWorkTo` take the chosen role+address.
- `app/settingsbridge.go`: `subjectTeachers` over `library.Service`; the settings address is never read for these sends.
- Wails: `SendPreparation`/`AgentConfirmCard` fields `recipient`, `recipientName`, `role`, `reason`, `subjectId`, `options[{role,name,email}]` (`input/wails/homeworkrecipient.go`); `ConfirmSend(..., role, address)` and `Confirm(..., role, address)` got two trailing args; `ErrSendChooseTeacher`. TS: `bindings/recipient.ts` (new, shared parsing), homework.ts, agentConfirm.ts.
- Tests: kind-to-role table, one-teacher-for-all, choose_teacher incl. one address, no_teacher_email, no_subject, «Да» with/without role, STALE (pinned and chosen role, unoffered address), no_assignment, injection (address in teacher hint/description/CorrectLesson), no settings fallback, cross-check of `proposal.NewRecipient` vs `subject.NewTeacher` e-mail rules (~40 addresses, no drift found).

## Decisions to confirm
- B-6 counts roles, not addresses: one filled role serves every kind; a known kind with its own role empty while two roles are filled (even with the same address) is `no_teacher_email`. "All filled roles share one address, no question" applies to an unknown kind only.
- Existing binding signatures were extended (not new names) because `TestFrontendCallsBoundNames` needs every binding called by name.
- `recipient.ts` added beside the two named binding files to avoid duplicated parsing.
- Settings «адрес для ДЗ» is now unused by sends but still editable: retire/hide later.

## Interim state until TASK-238
No UI for teacher fields or the choice: every send is refused with `no_teacher_email` (text points to the subject page) unless a teacher was set; `choose_teacher` cannot be confirmed from today's UI (the dialog passes no choice, «Да» returns `ErrSendChooseTeacher`). Safe, but run TASK-238 next.

## Follow-ups
- `input/agent/propose.go` `rejected` maps `no_subject`/`no_teacher_email` to `RefuseFailed`; map to `RefuseNoRecipient` (outside this task's files). `cases/homeworkflow/agentproposals.go` comment still says the recipient comes from settings.
- Reviewer lows: `ErrInvalidRecipient` from `ConfirmChoice` maps to `ErrSendStale` text (better `ErrSendChooseTeacher`); a failed library/works read now fails `Get` also for history cards (degrade to no names); choose card with 0-1 options after teachers change is stuck except «Нет»; `bindings/recipient.ts` duplicates `teacherRoles` of `library.ts`; `subjectTeachers.Teachers` lists all subjects to find one; add tests for Restore of CONFIRMED without recipient and ConfirmChoice with empty address.
- Reviewer M1 (doubted, deferred): the chosen role is not persisted; if the first execute fails before `Recheck`, a later execute without a role accepts any address that is among the current options. Needs a forged role/address pair from the student's own webview, and the address is still one of the subject's own teachers after the student's «Да»; no hard-rule breach (auditor agrees). Fix if wanted: pass teachers into `ConfirmChoice` or persist the role.
- `Proposal.Recipient` still prints its address under `%v` (pre-existing); nothing logs it.

## Changed existing tests
All used the settings recipient, `Facts.Recipient` or the old signatures; none weakened: domain `proposal_test.go`, `worksend_test.go`; homeworkflow `fakes_test.go`, `proposals_test.go`, `screen_test.go`, `agentproposals_test.go`, `worksend_test.go` (the card now reads the work, so a failing works read makes `Get` an error); wails `agentconfirm*_test.go`, `homework*_test.go` (extra args, new JSON names, `ErrSendChooseTeacher` in the Go/TS error list); app `proposals_test.go`, `homework_test.go` (expected e-mail subject «География: домашнее задание»), `worksend_test.go`, `exports_test.go`, `missing_parts_test.go`, `registrations_test.go`, `settingsbridge_test.go`; `desktop/tests` `proposals_sqlite_test.go`, `delivery_smtp_test.go`, `agent_propose_test.go`, `userconfirmation_test.go` (more user-only methods); fixtures only: `adapters/sqlite/proposals_test.go`, `cases/senddelivery/fakes_test.go`; frontend `homework.test.mjs`, `agentconfirm.test.mjs`.

## Security
Auditor: no high/medium (role and address come only from the student's click; unoffered address goes STALE; no settings fallback; no e-mail in logs, agent or Hub). Reviewer: approve, M1 doubted (above), M2 is the interim state (above).
