# TASK-155: Note-only lectures, «Добавить конспект прошедшей пары», citation labels (UI)

- **Agent:** frontend
- **Label:** frontend
- **Depends on:** TASK-152, TASK-153, TASK-154
- **PRD sections / AC:** ADR-0027 «Knowledge base, answers, tickets»; TASK-118 answer card; TASK-134 tickets
- **Branch:** ralph/<date>-task-155

## Goal
The student finds and adds notes for lectures that were never recorded, and sees where a citation comes from.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{KnowledgeBasePage.tsx,KnowledgeParts.tsx,SubjectsPage.tsx,SubjectsParts.tsx,TicketsParts.tsx}`
- `paratech/desktop/frontend/src/{knowledgeView.ts,libraryView.ts}` (+ tests)
- the answer card component and its view (agent panel) for the source label only

## Out of scope
- Bulk import (TASK-160).

## Acceptance criteria
- [ ] A subject lists recorded and note-only lectures by date, «без даты» last, with a chip «ваш конспект».
- [ ] «Добавить конспект прошедшей пары»: pick a past lesson of the subject, a date, or «без даты»; then the TASK-154
      block opens for it.
- [ ] Search hits and answer citations of section `ownNote` read «ваш конспект · стр. 3» / «· раздел „…“» / «·
      фрагмент 2» and open the note at that place; a removed source shows «источник удалён».
- [ ] Ticket not-covered text from the binding is shown as given.
- [ ] Tests for labels, ordering and the add flow; `npm run test` and `npm run build` pass.
