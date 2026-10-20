// Knowledge base bindings (TASK-091): calls into the Go core (internal/input/wails/knowledgebase.go). The core groups
// the lessons by subject, searches and exports; the page shows what it gets. Every text is imported, typed or model
// text: plain text only.

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  subjects: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Subjects",
  lessons: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Lessons",
  lesson: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Lesson",
  search: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Search",
  exportSubject: "paratech/desktop/internal/input/wails.KnowledgeBaseService.ExportSubject",
  setLessonInfo: "paratech/desktop/internal/input/wails.KnowledgeBaseService.SetLessonInfo",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  invalidId: "Неверный идентификатор записи.",
  notFound: "Запись не найдена: обновите список.",
  subjectNotFound: "В этом предмете больше нет занятий: обновите список.",
  query: "Введите для поиска от 2 до 100 символов.",
  lessonInfo: "Слишком длинное название предмета или неизвестный тип занятия.",
  notFinished: "Запись ещё идёт: поправить её можно после окончания.",
  busy: "Запись сейчас меняется. Попробуйте ещё раз.",
  unavailable: "Не удалось прочитать базу знаний. Попробуйте ещё раз.",
} as const;
