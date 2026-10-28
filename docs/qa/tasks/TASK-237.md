# TASK-237 — QA

The subject page has no teacher fields and the choice card has no UI yet (TASK-238), so there is little to click.
Until TASK-238, «Отправить преподавателю» (task or work) is refused with «Нет почты преподавателя…» unless the
teacher was set through the bindings; the settings address is no longer used for these two kinds.

1. In `paratech/`: `go test -race ./desktop/internal/domain/coursework/... ./desktop/internal/cases/homeworkflow/... ./desktop/internal/app/... ./desktop/internal/input/wails/... ./desktop/tests/...` passes (role by lesson kind, one teacher for all, choose_teacher, STALE, injection, e-mail validators cross-check).
2. `npm --prefix paratech/desktop/frontend run test` and `run build` pass.
3. After TASK-238 (owner): set a lecturer and a practice teacher with different addresses on a subject; «Отправить преподавателю» on a lecture task shows the lecturer in «Кому», on a seminar task the practice teacher; on a task without a lesson kind the card asks «Кому отправить?» and «Да» stays disabled until a choice; a subject with no teacher e-mail shows «Нет почты преподавателя…» with «Указать почту».
