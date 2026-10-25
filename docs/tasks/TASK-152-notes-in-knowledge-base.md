# TASK-152: Notes in «База знаний»: lectures without a recording and search

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-149, TASK-150
- **PRD sections / AC:** ADR-0027 «Knowledge base, answers, tickets»; ADR-0019, ADR-0024
- **Branch:** ralph/<date>-task-152

## Goal
A subject in «База знаний» lists note-only lectures next to recorded ones, and search finds the student's notes.

## Allowed to change
- `paratech/desktop/internal/domain/coursework/services/knowledgebase/**`
- `paratech/desktop/internal/cases/knowledgebase/**`
- `paratech/desktop/internal/adapters/sqlite/{notes.go,queries/notes.sql,sqlcgen/**}` (read queries only)
- `paratech/desktop/internal/input/wails/knowledgebase.go` (+ tests), `paratech/desktop/frontend/src/bindings/knowledgeBase.ts`
- `paratech/desktop/internal/app/**` (wiring)

## Out of scope
- Answers and tickets (TASK-153), UI (TASK-155).

## Acceptance criteria
- [ ] A KB lesson has a key: recording, or a note-only lecture (`lesson:<id>`, `day:<subject>:<date>`, `note:<id>`
      for undated). Notes anchored to a recording, or to a lesson that has a recording, join that lesson.
- [ ] `Document` gets note fragments (note, material, locator, text); search hits of section `ownNote` with
      provenance; limits of ADR-0019 apply (3 per section, 50 in all).
- [ ] Subject export (Markdown) includes «Мой конспект» per lecture, marked as the student's text.
- [ ] A removed note or material is left out; a broken part is skipped and logged (as ADR-0019).
- [ ] Domain tests: grouping, keys, ordering by date with undated last; use case tests on fakes.
- [ ] `go vet`, `go test -race` of the touched packages and the architecture test pass.
