# Glossary of UI words

Source: ADR-0034 §6. One word per thing in every screen, card, popup and error. Code comments may use English.

| Use | Means | Do not use |
|---|---|---|
| пара | an event of the schedule; «Пары дня», «Пары» of a subject | занятие (except «тип пары»: лекция, семинар, лабораторная) |
| запись | the audio of a pair, the recording | — |
| Конспект | what paratech made from the recording | Кратко, Краткое содержание, Конспект paratech |
| Мой конспект | the student's own files and text for a pair (owner's term) | Мои заметки |
| Текст лекции | the cleaned full text of the lecture | — |
| Расшифровка | the speech-to-text result; the step «Расшифровка» | Распознавание (речи); «модуль расшифровки», not «компонент распознавания» |
| задание | an assignment; section «Задания» | ДЗ, домашка, Домашние задания (the system folder «ДЗ» keeps its name) |
| работа под ключ | a turnkey work (file) | свободная работа |
| Фоновая обработка | jobs of the queue | Задачи (as a section title) |
| Ждёт интернет | waiting for network | Ждёт сети, нет сети |
| Ждёт пополнения минут | waiting for quota | Ждёт лимита, лимит исчерпан, Минуты обработки закончились (as a status) |
| Ждёт зарядки | waiting for AC power | Ждёт питания, ждёт питания от сети |
| Повторить | retry after an error (a text button) | Попробовать снова, Запустить снова, a bare round icon |
| Нужно ваше решение | things waiting for the student's confirmation | Ждёт вашего «Да» |
| Сроки на неделе | deadlines in the next 7 days | Горит на 7 дней |

Rules:
- Buttons are verbs: «Добавить предмет», «Новая папка», «Написать черновик», «Разобрать список».
- Confirmation of an external or irreversible action: «Да, <глагол>» / «Не <глагол>» («Да, отправить письмо» /
  «Не отправлять»); dialogs: verb + «Отмена».
- Impersonal voice everywhere: no «понял», «могу», «сделай как считаешь», «Проверяем…», «Повторим…».
- A refusal says why: «Нельзя удалить: …», «Нельзя отменить: …».
