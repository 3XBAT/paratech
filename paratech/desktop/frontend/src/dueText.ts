// How a deadline reads, in one place (TASK-343, TASK-344): «сегодня до 20:00», «завтра до 18:30», «до сб, 24 окт., 18:30»,
// «просрочено, пт, 23 окт., 20:00». Used by «Сегодня», the agent panel and «Задания». Kept apart from todayView so the
// assignment views can use it without an import cycle (todayView reaches them through the recordings view).

import { plural } from "./plural.js";

export const weekdaysShort = ["вс", "пн", "вт", "ср", "чт", "пт", "сб"] as const;
export const monthsShort = ["янв.", "февр.", "мар.", "апр.", "мая", "июн.", "июл.", "авг.", "сент.", "окт.", "нояб.", "дек."] as const;

export const pad = (n: number) => String(n).padStart(2, "0");

/** The moment of an RFC 3339 text, null for an empty or unreadable one. */
export function parseMoment(s: string): Date | null {
  if (s === "") {
    return null;
  }
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** «10:30» in the local time zone, "" when unknown. */
export function timeText(s: string): string {
  const d = parseMoment(s);
  return d === null ? "" : `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();

/** Whole local calendar days from `now` to `d` (negative for the past). */
export function daysBetween(d: Date, now: Date): number {
  return Math.round((dayStart(d) - dayStart(now)) / 86_400_000);
}

/** «сб, 24 окт.»: the day of a moment in the local zone; with `now` the year follows («24 окт. 2027») when it differs. */
export function dayText(d: Date, now?: Date): string {
  const year = now !== undefined && d.getFullYear() !== now.getFullYear() ? ` ${d.getFullYear()}` : "";
  return `${weekdaysShort[d.getDay()]}, ${d.getDate()} ${monthsShort[d.getMonth()]}${year}`;
}

/** «сб, 24 окт., 18:30»: the day and the time of a moment in the local zone (the year only as in dayText). */
export function dayAndTime(d: Date, now?: Date): string {
  return `${dayText(d, now)}, ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** «сегодня», «завтра», «через 2 дня», «через 5 дней»; a past date reads «просрочено». */
export function daysLeftText(n: number): string {
  if (n < 0) {
    return "просрочено";
  }
  if (n === 0) {
    return "сегодня";
  }
  if (n === 1) {
    return "завтра";
  }
  return `через ${n} ${plural(n, "день", "дня", "дней")}`;
}

/**
 * The deadline as one tidy phrase, always with the time (a date alone reads as «до конца дня»): «до сб, 24 окт., 18:30».
 * With `now`: a coming today reads «сегодня до 20:00», tomorrow «завтра до 18:30», a moment that has passed
 * «просрочено, пт, 23 окт., 20:00» (never «до …» for the past). With `done` the plain «до …» stays whatever the
 * moment (a finished assignment is not «просрочено»). The year shows only when it is not the year of `now`.
 * "" when unknown.
 */
export function dueText(due: string, now?: Date, done = false): string {
  const d = parseMoment(due);
  if (d === null) {
    return "";
  }
  if (now !== undefined && !done) {
    if (d.getTime() <= now.getTime()) {
      return `${daysLeftText(-1)}, ${dayAndTime(d, now)}`;
    }
    const days = daysBetween(d, now);
    if (days === 0) {
      return `сегодня до ${timeText(due)}`;
    }
    if (days === 1) {
      return `завтра до ${timeText(due)}`;
    }
  }
  return `до ${dayAndTime(d, now)}`;
}
