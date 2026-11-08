// The scripted answers of the fake provider for the recording pack (TASK-378). One imported lecture makes THREE Hub calls
// (desktop/internal/cases/homeworkflow/postprocess.go enqueues summarize, extract_homework and lecture_text per transcript;
// the lecture text of a short recording is one part), and the fake picks a rule by a substring of the last user message:
//   lecture text   `"segments":[`          only this request has a segment list (prompts.BuildLectureText)
//   summary        `"transcript_minutes"`  only the summary payload has the length (prompts.summaryData; rounded up, so >= 1)
//   extraction     `"transcript":"`        the summary also has it, but its rule comes first and is consumed first
// Every reply is the strict JSON of its operation (prompts.ParseSummary / ParseExtraction / ParseLectureText); the app checks
// it like model output and stores it as data. An unscripted call fails the scenario (src/steps).
import type { Rule } from "../../../src/fakellm/index.js";

export const LectureMatch = '"segments":[';
export const SummaryMatch = '"transcript_minutes"';
export const ExtractMatch = '"transcript":"';

export interface LectureScript {
  summaryHeading: string;
  summaryText: string;
  summaryPoint: string;
  keyPoint: string;
  assignmentTitle: string;
  assignmentDescription: string;
  /** RFC 3339 with the UTC offset of the lesson start (ParseExtraction refuses another offset). */
  due: string;
}

const filler = [
  "Это заранее заготовленный ответ тестового провайдера, он не связан со словами записи.",
  "Он нужен только для того, чтобы проверить путь от записи до экрана пары.",
  "Текст лекции собирается по частям, и каждая часть должна сохранить почти весь смысл расшифровки.",
  "Поэтому ответ не короче половины расшифровки: иначе приложение попросит составить текст заново.",
  "Эти предложения повторяются, чтобы набрать нужный объём, и ничего не утверждают о литературе.",
];

/** About `runes` characters of harmless Russian text in paragraphs of at most ~500 characters. */
export function fillerParagraphs(runes: number): string[] {
  const out: string[] = [];
  let total = 0;
  for (let i = 0; total < runes; i++) {
    const p = [0, 1, 2, 3, 4].map((k) => filler[(i + k) % filler.length]).join(" ");
    out.push(p);
    total += [...p].length;
  }
  return out;
}

/** Larger than any transcript of the fixture: the answer must keep at least 45 % of it (lectureparts.MinKeptPercent). */
export const LectureRunes = 2400;

export function lectureRules(s: LectureScript): Rule[] {
  return [
    {
      kind: "plain",
      match: LectureMatch,
      reply: JSON.stringify({ blocks: [{ heading: "Начало записи", from: 0, paragraphs: fillerParagraphs(LectureRunes) }] }),
    },
    {
      kind: "plain",
      match: SummaryMatch,
      reply: JSON.stringify({
        sections: [{ heading: s.summaryHeading, text: s.summaryText, points: [s.summaryPoint] }],
        key_points: [s.keyPoint],
      }),
    },
    {
      kind: "plain",
      match: ExtractMatch,
      reply: JSON.stringify({
        assignments: [{ title: s.assignmentTitle, description: s.assignmentDescription, due: s.due }],
        subject_hint: "",
        teacher_hint: "",
      }),
    },
  ];
}
