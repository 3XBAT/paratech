# TASK-234 — QA: the agent asks the subject of an assignment

Needs the Hub with catalog 8 / prompt v8 deployed first, then the new desktop build.

1. In a chat without a subject write «Запиши: решить номер 5 к пятнице». The agent shows the question «К какому предмету относится задание?» with your subjects and «Без предмета» last. No option is selected and the send button is disabled.
2. Pick a subject and send: the assignment is added under that subject.
3. Repeat and pick «Без предмета»: the assignment is added without a subject.
4. Repeat and press «Пропустить — сделай как считаешь»: the assignment is added without a subject (not under the first one).
5. In a chat of a subject name the assignment in the request («по матанализу…») or write it inside the subject's chat: no question, the assignment goes to that subject.
6. Other questions of the agent still have the first option preselected.
7. A request stopped before the update: «Продолжить»/«Повторить» end with the honest «не удалось»; a new request works.
