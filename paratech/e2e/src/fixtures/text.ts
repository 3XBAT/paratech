// Pure readers of what the screens say (no Playwright): used by the specs, unit-tested.

/** Collapses every kind of space (thin, no-break, tabs, line breaks) to one plain space and trims. */
export function collapse(text: string): string {
  return text.replace(/[\s   ]+/g, " ").trim();
}

/** Digits of a rendered amount such as «400 000» (groups of three, any space) -> 400000; NaN when it is not a number. */
export function amountOf(text: string): number {
  const digits = text.replace(/[\s   ]/g, "");
  return /^\d+$/.test(digits) ? Number(digits) : Number.NaN;
}

/**
 * What is wrong with a message meant for a student: empty, no Russian words, raw JS or transport leftovers
 * (undefined, null, NaN, Error:, a panic), a hash dump, a file path or a source position. Empty list = readable.
 */
export function readableRussian(text: string): string[] {
  const problems: string[] = [];
  if (text.trim() === "") {
    problems.push("текст пустой");
  }
  if (!/[А-Яа-яЁё]{3,}/.test(text)) {
    problems.push("в тексте нет русских слов");
  }
  if (/\bundefined\b|\bnull\b|\bNaN\b|\[object |\bError:|TypeError|panic|goroutine/.test(text)) {
    problems.push("в тексте служебные остатки (undefined/null/NaN/Error:/panic)");
  }
  if (/[0-9a-f]{32,}/i.test(text)) {
    problems.push("в тексте дамп хеша (32+ шестнадцатеричных знаков)");
  }
  if (/[A-Za-z]:\\|\/Users\/|\.go:\d+/.test(text)) {
    problems.push("в тексте путь файла или исходник");
  }
  return problems;
}
