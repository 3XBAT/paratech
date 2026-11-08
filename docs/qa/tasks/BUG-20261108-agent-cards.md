# BUG-20261108-agent-cards: check steps

Start from a cold app with a Hub key, some recorded and summarized lessons, an open agent panel on «Сегодня».

1. Ask the agent «что было на паре по <предмет>». The student sees the card «Ответ по материалам»: first a short «Загрузка ответа…», then the text of the answer (one paragraph per statement) and below it the sources (rows with place and lesson title).
2. The card does NOT disappear when the model's final text arrives: the card, the sources and the model's reply are all on screen. Hover a source row: it highlights; click: the lesson page opens on the cited tab.
3. A long answer: a statement over ~600 characters is cut at a word with «Показать полностью»; the button expands and «Свернуть» collapses it. A long word never makes the panel scroll sideways.
4. A statement the lessons do not support starts with the muted «Не из материалов пар:».
5. Ask «предложи удалить запись <название>»: the card «Нужно подтверждение» with «Да»/«Нет» stays under the model's reply. Press «Нет»; nothing is deleted.
6. Ask «добавь предмет <имя>»: the line with «Отменить» stays after the reply; «Отменить» removes the subject (the open «Предметы» tree may still show it until re-entered: known D5).
7. A question the lessons cannot answer: one muted line «В материалах пар ответа нет…» instead of a card with sources.
8. Switch to another chat and back: the cards of the finished request are still there. After an app restart they are not (known, follow-up).
