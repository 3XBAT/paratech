# Synthetic schedule (pack @settings)

`week.ics` is NOT stored here: the spec builds it at run time (`src/fixtures/schedule.ts`, `buildWeekIcs`) because one
lesson must start about two minutes after the import, and writes it to `<ctl>/fixtures/schedule/week.ics`.

What the generated week holds (all obviously fake, no owner data):

| Lesson | When | Teacher |
|---|---|---|
| Матанализ (лек.) | the first whole minute at least 2 min after the file is written | Тестов Тест Тестович |
| Литература (сем.) | one day later | Примерова Пример Примеровна |
| История (лек.) | two days later | Образцов Образец Образцович |

Times are UTC (`Z`) with `X-WR-TIMEZONE:Europe/Moscow`. The event text of «Матанализ» carries the synthetic call link
`https://telemost.yandex.ru/j/00000000000001` (it passes `ParseCallLink`, ADR-0032); the ICS importer ignores `LOCATION`
and the rest of `DESCRIPTION`, so the pack saves the link on «Сегодня» (row «Расписание: ссылка на пару»). The link is
never opened: it can only appear in `opened.jsonl` after «Подключиться» is pressed.
