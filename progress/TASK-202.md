# TASK-202: screens vs concept-agent.html

## Audit
Compared by code with `docs/design/concept-agent.html` (templates tpl-today, tpl-tasks, tpl-subjects, scenes 7 and 9). Status: fixed / follow-up (needs data) / follow-up (layout).

### Предметы and the page of a subject
1. Structure: prototype is a two-column page (tree 220px + subject page); implemented is one flat list of rows with «Открыть» and a `·`-joined line of folders. Fixed: tree column (semester, subjects, folders of the open subject with counts) + subject page.
2. No subject cards on the landing. Fixed: per-semester grid of subject cards (name, folder chips, click opens).
3. Tree icons (chevron right/down) missing in the sprite. Fixed: `chevr` icon added.
4. Subject head: prototype has `semester` over-line, h1, sub-line; implemented had «Предметы» label and aliases line. Fixed: semester line, name, aliases as sub-line.
5. Folder sections: prototype `sec-h` heading with `· N` count and a grouped list of file rows with a doc icon. Fixed (rows with icon, type · size · note).
6. Quick actions: prototype has «Предмет» / «Папка» under the tree. Fixed.
7. «Ближайшее» (deadlines of the subject), teacher and the lesson schedule line in the head, «Лекции» as lessons with status chips, «новая» mark on a fresh folder, «Ещё действия» menu: follow-up (needs data: the library binding gives no deadlines, teacher, schedule or lessons per subject).
8. State: the open subject was page-local, «Назад» skipped it. Fixed: `Route.subject`.

### Задания
9. The opened card was page-local (`HomeworkPage.openId`). Fixed: `Route.assignment` is the single source; list opens and «Назад» go through the route.
10. Prototype list: day/date column, filter segmented control with counters (Активные / Ждут «Да» / Сданные), status chips, «Открыть» small buttons: follow-up (needs data: counters and states; list is the TASK-195 area, not restyled).

### Сегодня
11. Head over-line, ask line with chips, two columns (lessons / waiting + burning), «Готово за день»: present (TASK-126/193). No divergence found in structure.
12. Statusbar row «В очереди N задач / Звук обрабатывается только на этом компьютере»: follow-up (needs data: queue size is not in the today screen).
13. The head has two buttons («Обновить», «Все записи») that the prototype does not: kept (functions), noted.

### Журнал
14. Prototype groups lines by «Сейчас» / «Сегодня» / date with time column, kind chip and progress; implemented is one flat card with a filter row of selects. Follow-up (layout + data: day grouping needs the time of a line as a value, the core sends only text; processing lines «Сейчас» come from another binding).
15. Prototype filter is one segmented control with counters; implemented has segmented + three selects + checkbox: kept (more filters, no counters data).

### Панель агента
16. Compared with the `ap-head` / `ap-thread` / `ap-comp` structure: markup follows the prototype (TASK-124/121). No divergence found without running the app; the on-screen check is left to the owner.

### Настройки
17. Prototype: left vertical nav (`group snav`, icons, accent dot on the current item) next to the content; implemented: tabs row above the content. Follow-up (layout): needs the page heads to move out of each sub-page into one shared head; touches six pages.
18. «Изменения сохраняются сразу» over-line, `sec-h` + `foot` notes under groups: partly present. Follow-up (minor text).
