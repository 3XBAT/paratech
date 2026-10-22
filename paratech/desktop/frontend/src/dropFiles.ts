// A file dropped on the window goes to the library. WebView2 gives the page no full path of a dropped file, so the
// path comes from the native drop of the shell as the Wails event "files:dropped" (payload: a list of paths). The shell
// does not emit it yet (see the report of TASK-124); until then the DOM drop only says to use «Добавить файл».
// Paths are untrusted and never shown: the page prints fixed Russian texts from the binding only.

import { genericError, type LibraryApi, libraryErrorText } from "./bindings/library.js";

export const droppedEvent = "files:dropped";

/** At most this many files are taken from one drop. */
export const maxDropped = 20;

export const dropTexts = {
  overlay: "Отпустите файл, чтобы добавить его в библиотеку",
  noPath: "Перетащенный файл определить не удаётся — используйте «Добавить файл».",
  added: "Файл добавлен во «Входящие».",
  existing: "Этот файл уже есть в библиотеке.",
  nothing: "Нечего добавлять.",
  tooMany: `Взяты первые ${maxDropped} файлов, остальные не добавлены.`,
  close: "Закрыть",
} as const;

/** One line of the report of a drop. */
export interface DropLine {
  ok: boolean;
  text: string;
}

/** The paths a Wails event carries: a list, a list in a list, or one path; anything else is no path. */
export function parseDroppedPaths(data: unknown): string[] {
  if (typeof data === "string") {
    return data === "" ? [] : [data];
  }
  if (!Array.isArray(data)) {
    return [];
  }
  if (data.length === 1 && Array.isArray(data[0])) {
    return parseDroppedPaths(data[0]);
  }
  return data.filter((p): p is string => typeof p === "string" && p !== "");
}

/**
 * The paths of the files of a DOM drop. A browser hides them; a File that still carries a string `path` (some embedded
 * webviews) is taken, otherwise the list is empty.
 */
export function pathsFromTransfer(transfer: { files?: ArrayLike<unknown> } | null | undefined): string[] {
  const files = transfer?.files;
  const out: string[] = [];
  for (let i = 0; files && i < files.length; i++) {
    const p = (files[i] as { path?: unknown } | null)?.path;
    if (typeof p === "string" && p !== "") {
      out.push(p);
    }
  }
  return out;
}

/** True when the drag carries files (not text or a link). */
export function hasFiles(transfer: { types?: ArrayLike<string> } | null | undefined): boolean {
  const types = transfer?.types;
  for (let i = 0; types && i < types.length; i++) {
    if (types[i] === "Files") {
      return true;
    }
  }
  return false;
}

/** The depth counter of a drag over the window: dragenter and dragleave also fire for every child element. */
export function nextDepth(depth: number, event: "enter" | "leave" | "reset"): number {
  if (event === "reset") {
    return 0;
  }
  return event === "enter" ? depth + 1 : Math.max(0, depth - 1);
}

/**
 * Builds onFilesDropped: imports each path through the binding one after another and reports a fixed Russian line per
 * file (never the path or the file name). A failed file does not stop the next. onDone runs after the last one.
 */
export function createFilesDropped(
  api: Pick<LibraryApi, "importDroppedFile">,
  report: (lines: DropLine[]) => void,
  onDone?: () => void,
): (paths: string[]) => Promise<void> {
  return async (paths) => {
    const taken = paths.filter((p) => typeof p === "string" && p !== "");
    if (taken.length === 0) {
      report([{ ok: false, text: dropTexts.noPath }]);
      return;
    }
    const lines: DropLine[] = [];
    for (const path of taken.slice(0, maxDropped)) {
      try {
        const res = await api.importDroppedFile(path);
        if (res.cancelled || res.material === null) {
          lines.push({ ok: false, text: dropTexts.nothing });
        } else {
          lines.push({ ok: true, text: res.existing ? dropTexts.existing : dropTexts.added });
        }
      } catch (err) {
        lines.push({ ok: false, text: libraryErrorText(err) || genericError });
      }
    }
    if (taken.length > maxDropped) {
      lines.push({ ok: false, text: dropTexts.tooMany });
    }
    report(lines);
    onDone?.();
  };
}

/** Subscribes to the native drop event. Without a Wails runtime (a browser, a test) it does nothing and says so. */
export async function subscribeDropped(
  on: (name: string, handler: (data: unknown) => void) => Promise<() => void>,
  onFilesDropped: (paths: string[]) => void,
): Promise<() => void> {
  try {
    return await on(droppedEvent, (data) => onFilesDropped(parseDroppedPaths(data)));
  } catch {
    return () => undefined;
  }
}
