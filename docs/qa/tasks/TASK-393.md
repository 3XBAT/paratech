# TASK-393: check steps (owner)

Start from the cold app, «Записи».

1. Import (or record) a long lecture. While it is recognised, the row shows «Расшифровка · N %», «≈ N мин осталось» and, at the end of that line, a small cross. Hover or focus shows «Отменить расшифровку».
2. Click the cross. A window «Отменить расшифровку?» names the recording and says the finished part is not kept. Focus is on «Не отменять»; Esc keeps the recognition running. Tick «Больше не спрашивать», press «Да, отменить».
3. The row says «Расшифровка отменена» (muted, no red) with «Запись сохранена.», stays under «Идут», and has the word button «Расшифровать снова». `whisper-cli.exe` is gone from Task Manager.
4. Restart the app: the row is still cancelled (it now sits in «Завершённые», the button is there too).
5. Press «Расшифровать снова»: the row goes «В очереди», then «Расшифровка», and runs to the summary.
6. Cancel once more: no question this time (the box was ticked).
7. A transcription that failed for good (all attempts used) shows «Не удалось расшифровать запись.» and the same button «Расшифровать снова».
8. No speech model installed: «Расшифровать снова» answers «Модель расшифровки не установлена.» with «Открыть модели речи».
9. «Сегодня»: a cancelled recording shows calmly «Расшифровка отменена», not «Проблема с записью».

Known gap: there is no setting to bring the question back after «Больше не спрашивать» (follow-up).
