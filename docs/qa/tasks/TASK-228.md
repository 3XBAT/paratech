# TASK-228 — QA: assignment soft delete (core)

The core has no button yet (UI is TASK-231/232), so the check is through tests and the agent.

1. `go test ./desktop/internal/domain/coursework/assignment/... ./desktop/internal/cases/homeworkflow/... ./desktop/tests/...` passes.
2. In the agent panel, create an assignment (create_assignment). Delete it with the test helper or after the UI tasks land: it vanishes from «Задания», «Сегодня» and the subject page.
3. Ask the agent about the deleted assignment by its earlier ID: it answers that it does not know it (unknown_id); Undo of the earlier agent action is refused.
4. Reprocess the recording the assignment came from: the deleted assignment does not come back.
5. «Вернуть» (RestoreAssignment) brings it back everywhere.
6. Deleting an assignment with an unresolved delivery (pending, sending, unknown) is refused.
