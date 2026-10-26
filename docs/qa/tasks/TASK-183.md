# TASK-183 — check steps

Prerequisite: the Hub runs a build with TASK-181 (master), then the desktop with TASK-183.

1. Open the agent panel, ask «что такое горутина», wait for the answer.
2. Ask «кто такой рыбаков», wait for the answer.
3. Press «Повторить» twice on the second request.
4. Expected: every answer is only about the second question (Рыбаков), not mixed with goroutines; no error about an invalid turn.
5. With an old Hub (before TASK-181) the agent would fail with invalid_argument: do not use it with this build.
