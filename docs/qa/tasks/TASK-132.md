# TASK-132: check steps (owner)

Needs a confirmed turnkey work (TASK-129) and the agent emitting a `work` card.

1. Confirmed work: the panel shows «Написать работу». For a presentation it does not.
2. Press it: progress «Раздел N из M» appears and updates by itself; no finished file is shown while it runs.
3. When the plan is ready: «План готов, утвердите» with the editor. Edit a title, add and remove a section, move one; try 13 sections or 40 words (message appears). «Утвердить план» starts the sections.
4. After completion: the .docx line, sections under spoilers, sources marked «проверить источник» where the model named them, the line «Сгенерировано — прочитайте перед сдачей». Text with `<b>` or links stays plain text.
5. «Доработать»: pick a section and a note, the section is written again. «Изменить план»: «Написать заново» is disabled until the plan is changed.
6. Not built yet (needs Go bindings): «Открыть в Word», «Остановить», «Отправить преподавателю».
