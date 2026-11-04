// File-name slugs for screenshots and traces: ASCII only, Cyrillic transliterated. Pure.

const translit: Record<string, string> = {
  а: "a", б: "b", в: "v", г: "g", д: "d", е: "e", ё: "e", ж: "zh", з: "z", и: "i", й: "y", к: "k", л: "l", м: "m",
  н: "n", о: "o", п: "p", р: "r", с: "s", т: "t", у: "u", ф: "f", х: "h", ц: "c", ч: "ch", ш: "sh", щ: "sch",
  ъ: "", ы: "y", ь: "", э: "e", ю: "yu", я: "ya",
};

const maxSlug = 48;

/** "Открыть «Настройки»" -> "otkryt-nastroyki"; "" -> "step". */
export function slugify(name: string, fallback = "step"): string {
  let out = "";
  for (const ch of name.toLowerCase()) {
    out += translit[ch] ?? ch;
  }
  const slug = out
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, maxSlug)
    .replace(/-+$/g, "");
  return slug === "" ? fallback : slug;
}

/** "007-otkryt-nastroyki": the counter keeps names unique and in order. */
export function stepSlug(name: string, counter: number): string {
  return `${String(counter).padStart(3, "0")}-${slugify(name)}`;
}
