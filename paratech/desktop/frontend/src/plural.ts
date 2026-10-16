/** The Russian plural form for n: one (1, 21), few (2–4, 22–24) or many (0, 5–20, 25…). */
export function plural(n: number, one: string, few: string, many: string): string {
  const abs = Math.abs(Math.trunc(n));
  const tens = abs % 100;
  const units = abs % 10;
  if (tens >= 11 && tens <= 14) {
    return many;
  }
  if (units === 1) {
    return one;
  }
  return units >= 2 && units <= 4 ? few : many;
}
