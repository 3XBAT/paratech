// Small file-system readers for the specs (pure apart from reading the disk).
import { readdirSync } from "node:fs";
import path from "node:path";

/** Paths of the regular files below `dir` (recursive) whose base name matches `pattern`, sorted; a missing dir has none. */
export function findFiles(dir: string, pattern: RegExp): string[] {
  const out: string[] = [];
  const walk = (d: string): void => {
    let entries;
    try {
      entries = readdirSync(d, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      const full = path.join(d, e.name);
      if (e.isDirectory()) {
        walk(full);
      } else if (e.isFile() && pattern.test(e.name)) {
        out.push(full);
      }
    }
  };
  walk(dir);
  return out.sort();
}
