// A synthetic ICS schedule for the layout pack (TASK-376): obviously fake subjects with LONG Russian names, lessons
// today and on other days, one lesson starting soon. Times are UTC ("Z") and the calendar says X-WR-TIMEZONE, as
// docs in adapters/icsschedule/doc.go describe; only the first bytes of a name matter to a person, the long tail
// stresses the titles of the lists. No commas or semicolons in a name: they would need ICS escaping.

export interface LayoutSubjects {
  analysis: string;
  circuits: string;
  language: string;
  sport: string;
}

export const layoutSubjects: LayoutSubjects = {
  analysis: "Математический анализ и аналитическая геометрия повышенной сложности",
  circuits: "Теоретические основы электротехники и электроники для инженерных специальностей",
  language: "Иностранный язык в профессиональной деятельности: деловая переписка и переговоры",
  sport: "Физическая культура",
};

const pad = (n: number) => String(n).padStart(2, "0");

/** 20261104T101500Z */
export function icsUtc(d: Date): string {
  return `${d.getUTCFullYear()}${pad(d.getUTCMonth() + 1)}${pad(d.getUTCDate())}T${pad(d.getUTCHours())}${pad(d.getUTCMinutes())}00Z`;
}

const minute = 60_000;
const hour = 60 * minute;
const day = 24 * hour;

interface Lesson {
  uid: string;
  title: string;
  start: Date;
  minutes: number;
  teacher: string;
  rrule?: string;
}

/** The lessons of the pack relative to `now`: one in 25 minutes, one earlier and one later today, series and singles later. */
export function layoutLessons(now: Date): Lesson[] {
  const at = (ms: number) => new Date(Math.floor((now.getTime() + ms) / minute) * minute);
  const s = layoutSubjects;
  return [
    { uid: "layout-soon", title: `${s.analysis} (лек.)`, start: at(25 * minute), minutes: 90, teacher: "Иванов Иван Иванович" },
    { uid: "layout-earlier", title: `${s.circuits} (сем.)`, start: at(-3 * hour), minutes: 80, teacher: "Петрова Анна Сергеевна" },
    { uid: "layout-later", title: `${s.language} (лек.)`, start: at(4 * hour), minutes: 90, teacher: "Сидоров Пётр Петрович" },
    { uid: "layout-series-a", title: `${s.analysis} (сем.)`, start: at(day), minutes: 90, teacher: "Иванов Иван Иванович", rrule: "FREQ=WEEKLY;COUNT=8" },
    { uid: "layout-series-b", title: `${s.circuits} (лек.)`, start: at(2 * day), minutes: 90, teacher: "Петрова Анна Сергеевна", rrule: "FREQ=WEEKLY;COUNT=6" },
    { uid: "layout-single", title: `${s.language} (сем.)`, start: at(3 * day), minutes: 90, teacher: "Сидоров Пётр Петрович" },
    { uid: "layout-sport", title: `${s.sport} (сем.)`, start: at(5 * day), minutes: 60, teacher: "Кузнецов Олег Иванович" },
  ];
}

/** The text of the file: CRLF lines, UTF-8, one VCALENDAR. */
export function buildLayoutIcs(now: Date): string {
  const lines = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//paratech e2e//layout//RU",
    "X-WR-CALNAME:Проверка раскладки",
    "X-WR-TIMEZONE:Europe/Moscow",
  ];
  for (const l of layoutLessons(now)) {
    lines.push(
      "BEGIN:VEVENT",
      `UID:${l.uid}@e2e.invalid`,
      `SUMMARY:${l.title}`,
      `DTSTART:${icsUtc(l.start)}`,
      `DTEND:${icsUtc(new Date(l.start.getTime() + l.minutes * minute))}`,
      `DESCRIPTION:Преподаватель: ${l.teacher}`,
    );
    if (l.rrule) {
      lines.push(`RRULE:${l.rrule}`);
    }
    lines.push("END:VEVENT");
  }
  lines.push("END:VCALENDAR");
  return lines.join("\r\n") + "\r\n";
}
