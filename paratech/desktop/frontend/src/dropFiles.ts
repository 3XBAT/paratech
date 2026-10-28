// A file dropped on the window goes to the library. The drop is handled in Go (Wails native drop on an element with
// data-file-drop-target): Go imports the files and emits "library:dropped" with the outcome. The page never sees or
// sends a path; it shows the outcome. File names are text nodes only.

import { genericError, libraryErrorText } from "./bindings/library.js";
import { isMaterialDrag } from "./libraryInboxView.js";

export const droppedEvent = "library:dropped";

/** The attribute Wails looks for on the element under the cursor; a drop elsewhere is ignored. */
export const dropTargetAttr = "data-file-drop-target";

const maxNameLength = 120;

export const dropTexts = {
  overlay: "Отпустите файл, чтобы добавить его в библиотеку",
  added: "добавлен во «Входящие»",
  existing: "уже есть в библиотеке",
  nothing: "Нечего добавлять.",
  close: "Закрыть",
} as const;

/** One line of the report of a drop. */
export interface DropLine {
  ok: boolean;
  text: string;
}

/** The report of a drop and whether anything is in the library now. */
export interface DropOutcome {
  lines: DropLine[];
  imported: boolean;
}

function record(v: unknown): Record<string, unknown> | null {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

function shortName(name: unknown): string {
  if (typeof name !== "string") {
    return "";
  }
  return name.length > maxNameLength ? `${name.slice(0, maxNameLength)}…` : name;
}

/** Turns the payload of "library:dropped" into report lines. Refusal texts pass only when known, else a generic one. */
export function parseDrop(data: unknown): DropOutcome {
  // Wails may deliver the payload alone or in a list of one.
  const payload = record(Array.isArray(data) && data.length === 1 ? data[0] : data);
  if (payload === null) {
    return { lines: [{ ok: false, text: genericError }], imported: false };
  }
  if (typeof payload.error === "string" && payload.error !== "") {
    return { lines: [{ ok: false, text: libraryErrorText(payload.error) }], imported: false };
  }
  const files = Array.isArray(payload.files) ? payload.files : [];
  if (files.length === 0) {
    return { lines: [{ ok: false, text: dropTexts.nothing }], imported: false };
  }

  const lines: DropLine[] = [];
  let imported = false;
  for (const raw of files) {
    const f = record(raw) ?? {};
    const name = shortName(f.name);
    if (typeof f.error === "string" && f.error !== "") {
      const text = libraryErrorText(f.error);
      lines.push({ ok: false, text: name === "" ? text : `${name}: ${text}` });
    } else if (record(f.material) !== null) {
      imported = true;
      const what = f.existing === true ? dropTexts.existing : dropTexts.added;
      lines.push({ ok: true, text: name === "" ? what : `${name}: ${what}` });
    } else {
      lines.push({ ok: false, text: name === "" ? genericError : `${name}: ${genericError}` });
    }
  }
  return { lines, imported };
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

/** The part of a DOM drag event the window handlers use. */
export interface DragLike {
  dataTransfer: { types?: ArrayLike<string> } | null;
  preventDefault(): void;
}

/**
 * The drag handlers of the whole window (DropZone adds them as listeners). A drag of files is always cancelled, or the
 * WebView navigates to the dropped file. The drag of a row of «Входящие» (TASK-339) is not a file: it is left alone, so the
 * cards of the subjects take it, and it never shows the overlay. A drag that carries both (never made by the page) is
 * cancelled all the same, the safe side, but leaves the overlay and the depth counter untouched.
 */
export function windowDragHandlers(setOver: (over: boolean) => void) {
  let depth = 0;
  const cancels = (e: DragLike) => !isMaterialDrag(e.dataTransfer) || hasFiles(e.dataTransfer);
  const tracks = (e: DragLike) => !isMaterialDrag(e.dataTransfer);
  return {
    enter(e: DragLike) {
      if (hasFiles(e.dataTransfer)) {
        e.preventDefault();
        if (tracks(e)) {
          depth = nextDepth(depth, "enter");
          setOver(true);
        }
      }
    },
    dragover(e: DragLike) {
      if (cancels(e)) {
        e.preventDefault();
      }
    },
    leave(e: DragLike) {
      if (tracks(e)) {
        depth = nextDepth(depth, "leave");
        setOver(depth > 0);
      }
    },
    drop(e: DragLike) {
      if (cancels(e)) {
        // Only so the WebView does not navigate to the file; the real drop is handled in Go ("library:dropped").
        e.preventDefault();
      }
      if (tracks(e)) {
        depth = nextDepth(depth, "reset");
        setOver(false);
      }
    },
    /** The outcome of a native drop came: no drag is going on. */
    reset() {
      depth = nextDepth(depth, "reset");
      setOver(false);
    },
  };
}

/** Subscribes to the outcome of a native drop. Without a Wails runtime (a browser, a test) it does nothing. */
export async function subscribeDropped(
  on: (name: string, handler: (data: unknown) => void) => Promise<() => void>,
  onDropped: (outcome: DropOutcome) => void,
): Promise<() => void> {
  try {
    return await on(droppedEvent, (data) => onDropped(parseDrop(data)));
  } catch {
    return () => undefined;
  }
}
