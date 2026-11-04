// The synthetic week of the settings pack (TASK-377): three subjects, one lesson that starts about two minutes after the
// import and a synthetic call link in the text of the first. Built at run time (the lesson time depends on the clock),
// written by the spec to <ctl>/fixtures/schedule/week.ics. Obviously fake teachers; nothing of the owner.
//
// Times are UTC ("Z") and the calendar says X-WR-TIMEZONE:Europe/Moscow, as docs of adapters/icsschedule describe: the
// preview shows the lessons in that zone (an expected «HH:MM» is computed here with Intl, not with the zone of the machine),
// «Сегодня» shows them in the zone of the machine (the expected time is computed with the local getHours()).
//
// The call link: ParseCallLink (domain/schedule/values/calllink.go, ADR-0032) takes http(s), a lowercase a-z0-9.- host
// with a dot and no port, and an RFC 3986 path; telemost.yandex.ru/j/<14 digits> passes (pinned by a unit test of the
// same rules). The ICS importer IGNORES LOCATION and the rest of DESCRIPTION (icsschedule/doc.go), so the link stays
// out of the app until the spec adds it through «Сегодня»; it is written to opened.jsonl only when something opens it.

export const CallLinkUrl = "https://telemost.yandex.ru/j/00000000000001";
export const ScheduleZone = "Europe/Moscow";

export const WeekSubjects = { analysis: "Матанализ", literature: "Литература", history: "История" } as const;

const minute = 60_000;
const day = 24 * 60 * minute;

export interface WeekLesson {
  uid: string;
  /** The SUMMARY: the subject and the kind in brackets, as students write it. */
  title: string;
  subject: string;
  /** What the preview names the kind: «лекция» / «семинар» (scheduleView.kindLabel). */
  kindLabel: string;
  teacher: string;
  start: Date;
  end: Date;
  /** The lesson that starts about two minutes after `now`. */
  soon: boolean;
  /** The call link written into the event text, if any. */
  link?: string;
}

const pad = (n: number) => String(n).padStart(2, "0");

/** 20261104T101500Z */
export function icsUtc(d: Date): string {
  return `${d.getUTCFullYear()}${pad(d.getUTCMonth() + 1)}${pad(d.getUTCDate())}T${pad(d.getUTCHours())}${pad(d.getUTCMinutes())}00Z`;
}

/**
 * The first whole minute that is at least two minutes after `now`: a lesson 2:00-3:00 away. The app shows minutes, and a
 * start rounded DOWN could be only a minute away by the time the file is read (the popup comes at the start).
 */
export function startInTwoMinutes(now: Date): Date {
  return new Date(Math.ceil((now.getTime() + 2 * minute) / minute) * minute);
}

/** The lessons for a moment `now`: Матанализ in about two minutes, Литература a day later, История two days later. */
export function weekLessons(now: Date): WeekLesson[] {
  const soon = startInTwoMinutes(now);
  const lesson = (l: Omit<WeekLesson, "end"> & { minutes: number }): WeekLesson => {
    const { minutes, ...rest } = l;
    return { ...rest, end: new Date(l.start.getTime() + minutes * minute) };
  };
  return [
    lesson({
      uid: "settings-analysis",
      title: `${WeekSubjects.analysis} (лек.)`,
      subject: WeekSubjects.analysis,
      kindLabel: "лекция",
      teacher: "Тестов Тест Тестович",
      start: soon,
      minutes: 90,
      soon: true,
      link: CallLinkUrl,
    }),
    lesson({
      uid: "settings-literature",
      title: `${WeekSubjects.literature} (сем.)`,
      subject: WeekSubjects.literature,
      kindLabel: "семинар",
      teacher: "Примерова Пример Примеровна",
      start: new Date(soon.getTime() + day),
      minutes: 80,
      soon: false,
    }),
    lesson({
      uid: "settings-history",
      title: `${WeekSubjects.history} (лек.)`,
      subject: WeekSubjects.history,
      kindLabel: "лекция",
      teacher: "Образцов Образец Образцович",
      start: new Date(soon.getTime() + 2 * day),
      minutes: 80,
      soon: false,
    }),
  ];
}

/** The text of the file: CRLF lines, UTF-8, one VCALENDAR; the event text of a lesson with a link carries it (escaped `\n`). */
export function buildWeekIcs(now: Date): string {
  const lines = ["BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//paratech e2e//settings//RU", "X-WR-CALNAME:Неделя для проверки", `X-WR-TIMEZONE:${ScheduleZone}`];
  for (const l of weekLessons(now)) {
    const description = `Преподаватель: ${l.teacher}` + (l.link ? `\\nСсылка на пару: ${l.link}` : "");
    lines.push(
      "BEGIN:VEVENT",
      `UID:${l.uid}@e2e.invalid`,
      `SUMMARY:${l.title}`,
      `DTSTART:${icsUtc(l.start)}`,
      `DTEND:${icsUtc(l.end)}`,
      `DESCRIPTION:${description}`,
    );
    if (l.link) {
      lines.push(`LOCATION:${l.link}`);
    }
    lines.push("END:VEVENT");
  }
  lines.push("END:VCALENDAR");
  return lines.join("\r\n") + "\r\n";
}

// --- what the screens say about a lesson ---

/** «ДД.ММ.ГГГГ ЧЧ:ММ» in a zone, the form of the preview (LessonText.start). */
export function wallClock(d: Date, zone = ScheduleZone): { date: string; time: string; day: string } {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone: zone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(d);
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  const date = `${get("day")}.${get("month")}.${get("year")}`;
  return { date, time: `${get("hour")}:${get("minute")}`, day: `${get("year")}-${get("month")}-${get("day")}` };
}

/** «10:15–11:45» as the preview shows the time of a lesson that starts and ends on one day of the zone. */
export function previewTime(l: Pick<WeekLesson, "start" | "end">, zone = ScheduleZone): string {
  const s = wallClock(l.start, zone);
  const e = wallClock(l.end, zone);
  return s.date === e.date ? `${s.time}–${e.time}` : `${s.time} – ${e.date} ${e.time}`;
}

/** «Сегодня» / «Завтра» / "" (a later day): the day header of the preview, judged in the zone of the file. */
export function previewDayLabel(l: Pick<WeekLesson, "start">, now: Date, zone = ScheduleZone): string {
  const today = wallClock(now, zone).day;
  const tomorrow = wallClock(new Date(now.getTime() + day), zone).day;
  const at = wallClock(l.start, zone).day;
  return at === today ? "Сегодня" : at === tomorrow ? "Завтра" : "";
}

/** «ЧЧ:ММ» in the zone of this machine, the form of «Сегодня» (dueText.timeText uses getHours). */
export function localTime(d: Date): string {
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Whether the lesson is on today's date of this machine, i.e. «Сегодня» lists it (judged at `now`). */
export function isLocalToday(d: Date, now: Date): boolean {
  return d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate();
}
