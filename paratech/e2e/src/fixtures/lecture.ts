// The lecture of one import of the recording pack (TASK-378): what is typed into «Импортировать запись» → «Дата и время» and
// what the scripted Hub says about it. Pure; the clock is an input.

export const Subject = "Литература";

const pad = (n: number) => String(n).padStart(2, "0");

/** «+03:00» of the machine's zone at the moment d. */
export function localOffset(d: Date): string {
  const off = -d.getTimezoneOffset();
  return `${off < 0 ? "-" : "+"}${pad(Math.floor(Math.abs(off) / 60))}:${pad(Math.abs(off) % 60)}`;
}

/** «ГГГГ-ММ-ДД» of the local date: the value of a native date input. */
export const dateInput = (d: Date): string => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

export interface Lecture {
  /** The title typed into «Название»: the row and the lesson screen show it. */
  title: string;
  /** «ГГГГ-ММ-ДД» and «ЧЧ:ММ» typed into the form: yesterday 10:00 of the machine's zone (a lecture cannot start in the future). */
  date: string;
  time: string;
  /** The deadline of the scripted assignment: RFC 3339 with the offset of the lecture start (prompts.ParseExtraction). */
  due: string;
  /** «дд.мм» of the deadline, for the screens that print the date. */
  dueDay: string;
  assignmentTitle: string;
  assignmentDescription: string;
  summaryHeading: string;
  summaryText: string;
  summaryPoint: string;
  keyPoint: string;
}

// The wording of the app (desktop/frontend/src/dueText.ts), copied here on purpose: the specs check the screen against it
// without Intl, so the result does not depend on the ICU data or the locale of the runner.
const WeekdaysShort = ["вс", "пн", "вт", "ср", "чт", "пт", "сб"];
const MonthsShort = ["янв.", "февр.", "мар.", "апр.", "мая", "июн.", "июл.", "авг.", "сент.", "окт.", "нояб.", "дек."];

/** «вт, 10 нояб.»: the day of a deadline `due` («ГГГГ-ММ-ДД…», the wall clock of the lecture's zone) as the app prints it. */
export function dueDayRu(due: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})T/.exec(due);
  if (!m) {
    throw new Error(`not an RFC 3339 deadline: ${JSON.stringify(due)}`);
  }
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  return `${WeekdaysShort[new Date(Date.UTC(y, mo - 1, d)).getUTCDay()]}, ${d} ${MonthsShort[mo - 1]}`;
}

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * What the screen prints for the deadline «до вт, 10 нояб., 20:00» (the year follows only when it is not the current one).
 * `relative` also accepts «завтра до 20:00», which the lists print if a run crosses midnight; the lesson tab never does.
 */
export function dueMatcher(due: string, relative = false): RegExp {
  const time = /T(\d{2}:\d{2})/.exec(due)?.[1] ?? "20:00";
  const plain = `до ${escapeRe(dueDayRu(due))}(?: \\d{4})?, ${escapeRe(time)}`;
  return new RegExp(relative ? `(?:${plain}|завтра до ${escapeRe(time)})` : plain);
}

/** The lecture of one import. `label` tells the imports apart in the list («основная», «отмена», «перезапуск»). */
export function lectureOf(label: string, now = new Date()): Lecture {
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 10, 0, 0);
  const dueDay = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 2, 20, 0, 0);
  return {
    title: `Человек в футляре · ${label}`,
    date: dateInput(start),
    time: "10:00",
    // the offset of the START: the app refuses a deadline with another one, so the wall-clock time is that of the lecture's zone
    due: `${dateInput(dueDay)}T20:00:00${localOffset(start)}`,
    dueDay: `${pad(dueDay.getDate())}.${pad(dueDay.getMonth() + 1)}`,
    assignmentTitle: `Прочитать рассказ «Человек в футляре» до конца (${label})`,
    assignmentDescription: "Страницы 1–12 из сборника. Записать три цитаты о Беликове.",
    summaryHeading: "Беликов и его футляр",
    summaryText: "Проверочный конспект из ответа тестового провайдера: человек в футляре боится любых перемен.",
    summaryPoint: "Беликов носит калоши и зонтик в любую погоду",
    keyPoint: "Домашнее задание: дочитать рассказ до конца",
  };
}
