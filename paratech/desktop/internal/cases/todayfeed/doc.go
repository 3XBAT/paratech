// Package todayfeed is the use case of «Сегодня» (TASK-125, ADR-0021 «Posts after a lesson», design §2.7):
//
//   - Poster turns the completion of a post-processing job (the summary is stored, the homework was looked for) into a
//     post after the lesson. The app calls it from the jobrunner handler of these jobs (no case imports another,
//     ARCH-20). No LLM: the post only records facts, and a repeated event changes nothing (one post per recording).
//   - Screen reads the data of the screen: the lessons of the day, the posts, «Ждёт вашего «Да»» (proposals waiting
//     for the student), «Горит на 7 дней», «Готово за день» and the hint chips, and the posts of one subject thread.
//
// «Да, в календарь» of a post only names the assignments for the existing create_deadline_event proposal; nothing
// here creates, confirms or executes a proposal (hard rule 3).
//
// Rules that apply: ARCH-20..27. The post is domain/agent/post, the rules of the screen domain/agent/services/today.
package todayfeed
