// «@» mentions of the agent panel (TASK-247): the fragment under the caret, the chosen pills, the keys of the list, the
// debounced search and the pills of the thread. Pure, no React. The names of objects are untrusted plain text: they are
// passed through untouched and the page draws them as text nodes only. A pill always shows the name stored at mention
// time (ADR-0028), never a name looked up again.

import type { MentionCandidate, MentionView, MentionsView } from "./bindings/agent.js";
import type { PageId } from "./nav.js";

/** The most mentions in one request (values.MaxMentions; the core checks again). */
export const maxMentions = 8;

export const mentionTexts = {
  hint: "Напишите @, чтобы указать предмет, пару, материал или задание",
  loading: "Ищу…",
  empty: "Ничего не найдено",
  unavailable: "Поиск недоступен: помощник не подключён",
  failed: "Не удалось выполнить поиск",
  full: `Не больше ${maxMentions} упоминаний в одной просьбе`,
  retry: "Повторить",
  picked: "уже указано",
  pillsLabel: "Упоминания",
  listLabel: "Что указать",
} as const;

export const removeMentionLabel = (label: string) => `Убрать упоминание ${label}`;

const kindCaptions: Record<string, string> = {
  subject: "Предмет",
  lesson: "Пара",
  material: "Материал",
  assignment: "Задание",
};

const unknownKindCaption = "Объект";

/** «Предмет», «Пара», «Материал», «Задание»; an unknown kind reads as «Объект». */
export function mentionKindCaption(kind: string): string {
  return Object.prototype.hasOwnProperty.call(kindCaptions, kind) ? kindCaptions[kind] : unknownKindCaption;
}

// ---- The fragment under the caret ----

export interface MentionFragment {
  /** Index of the «@». */
  start: number;
  /** The caret: the query is the text between the «@» and here. */
  end: number;
  query: string;
}

const isSpace = (c: string) => /\s/.test(c);

/**
 * The «@query» the caret is in: an «@» at the start of the text or after a whitespace, with no whitespace between it and
 * the caret. An «@» inside a word («a@b») is no mention. Null when the caret is not in such a fragment.
 */
export function activeFragment(text: string, caret: number): MentionFragment | null {
  const end = Math.min(Math.max(caret, 0), text.length);
  for (let i = end - 1; i >= 0; i--) {
    const c = text[i];
    if (c === "@") {
      return i === 0 || isSpace(text[i - 1]) ? { start: i, end, query: text.slice(i + 1, end) } : null;
    }
    if (isSpace(c)) {
      return null;
    }
  }
  return null;
}

/** The text without the fragment (the mention becomes a pill, not text) and where the caret goes. */
export function removeFragment(text: string, f: MentionFragment): { text: string; caret: number } {
  let end = f.end;
  // «a @x b» becomes «a b», not «a  b».
  const afterSpace = f.start === 0 || isSpace(text[f.start - 1]);
  if (afterSpace && end < text.length && (text[end] === " " || text[end] === "\t")) {
    end += 1;
  }
  return { text: text.slice(0, f.start) + text.slice(end), caret: f.start };
}

// ---- The chosen pills ----

export interface AddResult {
  pills: MentionView[];
  outcome: "added" | "duplicate" | "full";
}

/** Adds a candidate: the same object is not added twice, and not more than eight. Only kind, id and label are kept. */
export function addMention(pills: readonly MentionView[], c: { kind: string; id: string; label: string }): AddResult {
  if (pills.some((p) => p.kind === c.kind && p.id === c.id)) {
    return { pills: [...pills], outcome: "duplicate" };
  }
  if (pills.length >= maxMentions) {
    return { pills: [...pills], outcome: "full" };
  }
  return { pills: [...pills, { kind: c.kind, id: c.id, label: c.label }], outcome: "added" };
}

/** Backspace with the caret at the very start of the field removes the last pill. */
export function backspaceRemovesPill(sel: { start: number; end: number }, pillCount: number): boolean {
  return pillCount > 0 && sel.start === 0 && sel.end === 0;
}

/** What a request is asked with: kind, id and label only. Absent when there is none. */
export function askMentions(pills: readonly MentionView[]): MentionView[] | undefined {
  return pills.length === 0 ? undefined : pills.map((p) => ({ kind: p.kind, id: p.id, label: p.label }));
}

// ---- Keys of the open list ----

export type PickerKey = "next" | "prev" | "pick" | "close" | "swallow" | "none";

export interface KeyLike {
  key: string;
  shiftKey?: boolean;
  ctrlKey?: boolean;
  altKey?: boolean;
  metaKey?: boolean;
  isComposing?: boolean;
  keyCode?: number;
}

/**
 * What a key does while the list is open. Enter with nothing to pick is swallowed (it must not send the request); Tab
 * with nothing to pick moves focus on. Nothing is intercepted while an IME composes.
 */
export function pickerKey(e: KeyLike, optionCount: number): PickerKey {
  if (e.isComposing === true || e.keyCode === 229 || e.ctrlKey === true || e.altKey === true || e.metaKey === true) {
    return "none";
  }
  switch (e.key) {
    case "ArrowDown":
      return optionCount > 0 ? "next" : "none";
    case "ArrowUp":
      return optionCount > 0 ? "prev" : "none";
    case "Enter":
      return e.shiftKey === true ? "none" : optionCount > 0 ? "pick" : "swallow";
    case "Tab":
      return e.shiftKey === true || optionCount === 0 ? "none" : "pick";
    case "Escape":
      return "close";
    default:
      return "none";
  }
}

/** The next active option, wrapping at both ends. */
export function moveActive(active: number, count: number, dir: "next" | "prev"): number {
  if (count <= 0) {
    return 0;
  }
  return dir === "next" ? (active + 1) % count : (active - 1 + count) % count;
}

/** The active option kept inside the list. */
export function clampActive(active: number, count: number): number {
  return count <= 0 ? 0 : Math.min(Math.max(active, 0), count - 1);
}

// ---- The search with its debounce ----

export type SearchStatus = "idle" | "loading" | "ready" | "unavailable" | "failed";

export interface SearchState {
  status: SearchStatus;
  /** The query the status and the candidates belong to. */
  query: string;
  candidates: MentionCandidate[];
}

export const idleSearch: SearchState = { status: "idle", query: "", candidates: [] };

/** The search is not cheap for the core: typing waits this long before a call. */
export const mentionDebounceMs = 250;

export interface SearchDeps {
  call(query: string): Promise<MentionsView>;
  setTimer(fn: () => void, ms: number): unknown;
  clearTimer(handle: unknown): void;
  onState(s: SearchState): void;
  delay?: number;
}

export interface MentionSearch {
  /** The query changed (or the list opened). The same query again is a no-op. */
  request(query: string): void;
  /** «Повторить» after a failure. */
  retry(): void;
  /** The list closed: nothing is shown or delivered any more. */
  cancel(): void;
}

/**
 * Debounced search: the first request after the list opened goes at once, the next ones wait `delay` after the last
 * keystroke. At most one call is in flight; a query that fires meanwhile runs right after it ends, with the newest text.
 * A result for a query that is no longer the wanted one is dropped.
 */
export function createMentionSearch(d: SearchDeps): MentionSearch {
  const delay = d.delay ?? mentionDebounceMs;
  // The newest query; null while the list is closed.
  let wanted: string | null = null;
  // The last delivered result: it answers `done.query` again without a call.
  let done: SearchState | null = null;
  let state: SearchState = idleSearch;
  let timer: unknown = null;
  let inFlight = false;
  // A call is due as soon as the one in flight ends.
  let queued = false;

  const emit = (s: SearchState) => {
    state = s;
    d.onState(s);
  };
  const stopTimer = () => {
    if (timer !== null) {
      d.clearTimer(timer);
      timer = null;
    }
  };

  const run = () => {
    stopTimer();
    if (wanted === null || done?.query === wanted) {
      return;
    }
    if (inFlight) {
      queued = true;
      return;
    }
    const q = wanted;
    inFlight = true;
    queued = false;
    let p: Promise<MentionsView>;
    try {
      p = d.call(q);
    } catch (err) {
      p = Promise.reject(err);
    }
    p.then(
      (v): SearchState =>
        v.problem === ""
          ? { status: "ready", query: q, candidates: v.candidates }
          : { status: v.problem === "unavailable" ? "unavailable" : "failed", query: q, candidates: [] },
      (): SearchState => ({ status: "failed", query: q, candidates: [] }),
    ).then((s) => {
      inFlight = false;
      if (wanted === q) {
        done = s;
        queued = false;
        emit(s);
      } else if (queued) {
        run();
      }
    });
  };

  return {
    request(query) {
      if (wanted === query && (timer !== null || inFlight || done?.query === query)) {
        return;
      }
      const first = wanted === null;
      wanted = query;
      if (done?.query === query) {
        // Back to the query that was already answered: show that answer, drop what was scheduled.
        stopTimer();
        queued = false;
        emit(done);
        return;
      }
      emit({ status: "loading", query, candidates: state.candidates });
      if (first) {
        run();
      } else {
        stopTimer();
        timer = d.setTimer(run, delay);
      }
    },
    retry() {
      if (wanted === null) {
        return;
      }
      done = null;
      emit({ status: "loading", query: wanted, candidates: [] });
      run();
    },
    cancel() {
      stopTimer();
      wanted = null;
      done = null;
      queued = false;
      emit(idleSearch);
    },
  };
}

// ---- What the list shows ----

export type PopupKind = "full" | "loading" | "unavailable" | "failed" | "empty" | "options";

export interface PopupOption {
  key: string;
  kind: string;
  id: string;
  label: string;
  caption: string;
  hint: string;
  /** The object is already among the pills. */
  picked: boolean;
}

export interface PopupModel {
  kind: PopupKind;
  /** The status line; "" when options are shown. */
  text: string;
  options: PopupOption[];
  /** «Повторить» is offered. */
  retry: boolean;
}

/** The list for the search state and the pills already chosen. Every state has its own line; none is a dead end. */
export function popupModel(search: SearchState, picked: readonly MentionView[]): PopupModel {
  const status = (kind: PopupKind, text: string, retry = false): PopupModel => ({ kind, text, options: [], retry });
  if (picked.length >= maxMentions) {
    return status("full", mentionTexts.full);
  }
  if (search.status === "unavailable") {
    return status("unavailable", mentionTexts.unavailable);
  }
  if (search.status === "failed") {
    return status("failed", mentionTexts.failed, true);
  }
  const seen = new Set<string>();
  const options: PopupOption[] = [];
  for (const c of search.candidates) {
    const key = `${c.kind}:${c.id}`;
    if (!seen.has(key)) {
      seen.add(key);
      options.push({
        key,
        kind: c.kind,
        id: c.id,
        label: c.label,
        caption: mentionKindCaption(c.kind),
        hint: c.hint,
        picked: picked.some((p) => p.kind === c.kind && p.id === c.id),
      });
    }
  }
  if (options.length > 0) {
    // Results of the previous query stay while the next one is being fetched.
    return { kind: "options", text: "", options, retry: false };
  }
  return search.status === "ready" ? status("empty", mentionTexts.empty) : status("loading", mentionTexts.loading);
}

// ---- Pills of the thread ----

export interface MentionPill {
  key: string;
  kind: string;
  id: string;
  /** The stored name, untouched; the kind when the stored name is empty. Plain text only. */
  label: string;
  caption: string;
  /** «Открыть: Предмет Физика». */
  aria: string;
}

/** The pills of a request: always the name stored at mention time, whatever happened to the object since. */
export function mentionPills(list: readonly MentionView[] | undefined): MentionPill[] {
  return (list ?? []).map((m, i) => {
    const caption = mentionKindCaption(m.kind);
    const label = m.label.trim() !== "" ? m.label : caption;
    return { key: `${m.kind}:${m.id}:${i}`, kind: m.kind, id: m.id, label, caption, aria: `Открыть: ${caption} ${label}` };
  });
}

/** Where a pill leads, in terms of the routes the window already has. */
export type MentionOpening = { type: "subject"; id: string } | { type: "assignment"; id: string } | { type: "page"; page: PageId };

/**
 * No route opens a lesson or a material by id: «Пара» leads to «База знаний», «Материал» to «Предметы». A deleted object
 * just meets the normal state of that page. An unknown kind or an empty id leads nowhere.
 */
export function mentionOpening(kind: string, id: string): MentionOpening | null {
  if (id === "") {
    return null;
  }
  switch (kind) {
    case "subject":
      return { type: "subject", id };
    case "assignment":
      return { type: "assignment", id };
    case "lesson":
      return { type: "page", page: "knowledge" };
    case "material":
      return { type: "page", page: "subjects" };
    default:
      return null;
  }
}
