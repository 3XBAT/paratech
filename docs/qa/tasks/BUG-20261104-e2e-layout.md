# QA: BUG-20261104-e2e-layout (owner's check)

Start the app cold (default window, then drag it to its smallest size). Open the agent panel (Ctrl+K) and drag it to its widest.

1. «Сегодня» with a schedule that has lessons: the first thing you see is the day. In a lesson row «Добавить ссылку» is fully visible under the status, its text lines up with the lesson title; widen the window: it sits at the right of the row.
2. «Сегодня», a deadline with a long subject: hover the subject, the full text shows in a tooltip.
3. «Предметы»: hover a long subject name, a folder, and the chevron next to a subject: each shows a tooltip with the full name («Развернуть: …» / «Свернуть: …» for the chevron).
4. Subject -> «Билеты к экзамену»: the path «Предметы / <предмет> / Билеты к экзамену» keeps «Билеты к экзамену» readable with the widest panel; the long subject in the middle is cut with «…»; no empty gap after the «/». Hover a cut crumb: the full name.
5. «Задания» -> open an assignment with a long title: the path line ends with «…» and never makes the page scroll sideways. No error is visible; the block «Сделать под ключ» shows «Начать разбор».
6. Developer check (optional): the webview console has no `422` on opening an assignment and no 404 for `favicon.ico` / `wails/custom.js` at start.
