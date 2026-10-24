// View logic of the writing cards of a turnkey work (TASK-132): which card a work shows, the progress line, the plan
// editor and its checks, polling. The core decides statuses and steps (ARCH-44); here they are only put into words in
// an impersonal voice. Topic, titles, paragraphs, notes and sources are model or student text: pass them through
// untouched and render them as text nodes only.

import { notFoundText, turnkeyErrorText } from "./bindings/turnkey.js";
import type { OutlineItem, TurnkeyApi, TurnkeyWork, WorkSection, WorkWriting } from "./bindings/turnkey.js";

// The limits of the domain (domain/coursework/work/writing.go); titles and notes are counted in runes, as there.
export const limits = {
  minSections: 1,
  maxSections: 12,
  minWords: 50,
  maxWords: 3000,
  totalWordsMax: 15000,
  titleMax: 150,
  noteMax: 1000,
} as const;

/** Length in runes (code points), not UTF-16 units. */
export function runeLen(s: string): number {
  return [...s].length;
}

export const workTexts = {
  caption: "Написание работы",
  startHint: "Требования подтверждены. Работу можно написать целиком: сначала составляется план.",
  start: "Написать работу",
  planning: "Составляется план",
  approveCaption: "План готов, утвердите",
  approveHint: "Разделы можно переименовать, переставить, добавить или убрать.",
  approve: "Утвердить план",
  savePlan: "Сохранить план",
  blockedText: "Не удалось написать раздел. Можно сгенерировать заново.",
  blockedOutlineText: "Не удалось составить план. Можно составить заново.",
  noteLabel: "Дополнительные указания",
  regenerate: "Сгенерировать заново",
  doneCaption: "Работа готова",
  generated: "Сгенерировано — прочитайте перед сдачей",
  verify: "проверить источник",
  revise: "Доработать",
  reviseSection: "Что дорабатывать",
  allSections: "Весь текст",
  editPlan: "Изменить план",
  rewrite: "Написать заново",
  cancel: "Отмена",
  addSection: "Добавить раздел",
  titleLabel: "Название раздела",
  wordsLabel: "Объём, слов",
  moveUp: "Выше",
  moveDown: "Ниже",
  remove: "Убрать раздел",
  sources: "Источники",
  fallbackName: "Работа",
  fileKind: ".docx",
} as const;

export const checkTexts = {
  count: `Нужно от ${limits.minSections} до ${limits.maxSections} разделов.`,
  title: "У каждого раздела должно быть название.",
  words: `Объём раздела — целое число слов от ${limits.minWords} до ${limits.maxWords}.`,
  total: `Общий объём — не больше ${limits.totalWordsMax} слов.`,
  titleLong: `Название раздела — не длиннее ${limits.titleMax} символов.`,
} as const;

// ---- Which card ----

export interface SectionText {
  key: string;
  heading: string;
  paragraphs: string[];
  sources: { text: string; verify: boolean }[];
}

export type WorkCardModel =
  | { type: "none" }
  | { type: "start"; caption: string; hint: string; button: string }
  | { type: "running"; caption: string; line: string }
  | { type: "approve"; caption: string; hint: string }
  | { type: "blocked"; caption: string; text: string; noteLabel: string; button: string }
  | { type: "done"; caption: string; file: { kind: string; name: string }; notice: string; sections: SectionText[] };

/** «Раздел N из M» while the sections are written; N is the section being written, never above M. */
export function progressLine(w: WorkWriting): string {
  if (w.step === "outline" || w.total <= 0) {
    return workTexts.planning;
  }
  const n = w.step === "assemble" ? w.total : Math.min(w.done + 1, w.total);
  return `Раздел ${n} из ${w.total}`;
}

/** The written sections to read: the paragraphs and the sources, as they came. */
export function sectionTexts(sections: readonly WorkSection[]): SectionText[] {
  return sections
    .filter((s) => s.paragraphs.length > 0 || s.sources.length > 0)
    .map((s, i) => ({
      key: `${s.number}:${i}`,
      heading: `${s.number}. ${s.title}`,
      paragraphs: s.paragraphs,
      sources: s.sources.map((r) => ({ text: r.text, verify: r.verify })),
    }));
}

/**
 * The card of a work. A work that is not confirmed and has no writing shows nothing here (its questions, its card and
 * the helper are TASK-130). Writing.sections hold the material of a step that runs: it is never shown as a result.
 */
export function workCardModel(w: TurnkeyWork): WorkCardModel {
  const wr = w.writing;
  if (wr === null) {
    // A presentation is not written as a paper.
    return w.status === "confirmed" && w.workType !== "presentation"
      ? { type: "start", caption: workTexts.caption, hint: workTexts.startHint, button: workTexts.start }
      : { type: "none" };
  }
  switch (wr.step) {
    case "outline":
    case "section":
    case "assemble":
      return { type: "running", caption: workTexts.caption, line: progressLine(wr) };
    case "approve":
      return { type: "approve", caption: workTexts.approveCaption, hint: workTexts.approveHint };
    case "blocked":
      return {
        type: "blocked",
        caption: workTexts.caption,
        text: wr.sections.length === 0 ? workTexts.blockedOutlineText : workTexts.blockedText,
        noteLabel: workTexts.noteLabel,
        button: workTexts.regenerate,
      };
    case "none":
      if (wr.result === "") {
        return { type: "none" };
      }
      return {
        type: "done",
        caption: workTexts.doneCaption,
        file: { kind: workTexts.fileKind, name: wr.topic.trim() === "" ? workTexts.fallbackName : wr.topic },
        notice: workTexts.generated,
        sections: sectionTexts(wr.sections),
      };
  }
}

// ---- Polling and stale replies ----

export const pollMs = 3000;
export const retryMs = 15000;

/** A step of the writing is running: the card re-reads the work until it is not. */
export function isRunning(w: TurnkeyWork | null): boolean {
  const step = w?.writing?.step;
  return step === "outline" || step === "section" || step === "assemble";
}

/** The wait before the next read, or null to stop. Nothing is read again once a loaded work does not run. */
export function pollDelay(w: TurnkeyWork | null, failed: boolean): number | null {
  if (w !== null && !isRunning(w)) {
    return null;
  }
  return failed ? retryMs : pollMs;
}

export interface PollOptions {
  read(): Promise<TurnkeyWork>;
  onWork(w: TurnkeyWork): void;
  /** A read failed; the text is shown. */
  onError(text: string): void;
  /** The newest accepted work. */
  current(): TurnkeyWork | null;
}

/** Reads the work now and then again by pollDelay. A missing work is shown and not retried. Returns the stop function. */
export function startPolling(o: PollOptions): () => void {
  let alive = true;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const tick = async () => {
    let failed = false;
    try {
      const w = await o.read();
      if (!alive) {
        return;
      }
      o.onWork(w);
    } catch (e) {
      if (!alive) {
        return;
      }
      failed = true;
      const text = turnkeyErrorText(e);
      o.onError(text);
      if (text === notFoundText) {
        return;
      }
    }
    const wait = pollDelay(o.current(), failed);
    if (wait !== null) {
      timer = setTimeout(() => void tick(), wait);
    }
  };
  void tick();
  return () => {
    alive = false;
    clearTimeout(timer);
  };
}

/** Runs one call at a time; a call made while another is in flight is dropped (returns false). */
export function singleFlight(): (call: () => Promise<void>) => Promise<boolean> {
  let busy = false;
  return async (call) => {
    if (busy) {
      return false;
    }
    busy = true;
    try {
      await call();
    } finally {
      busy = false;
    }
    return true;
  };
}

/** The newest of two replies: an older version never replaces a newer one. */
export function acceptWork(current: TurnkeyWork | null, next: TurnkeyWork): TurnkeyWork {
  return current !== null && next.version < current.version ? current : next;
}

// ---- Plan editor ----

/** A section in the editor; words stay text while typed. */
export interface EditorItem {
  /** 0: a new section. */
  number: number;
  title: string;
  words: string;
}

export const newSectionWords = "300";

export function editorFromSections(sections: readonly WorkSection[]): EditorItem[] {
  return sections.map((s) => ({ number: s.number, title: s.title, words: String(s.words) }));
}

export function addItem(items: readonly EditorItem[]): EditorItem[] {
  return items.length >= limits.maxSections ? [...items] : [...items, { number: 0, title: "", words: newSectionWords }];
}

export function removeItem(items: readonly EditorItem[], index: number): EditorItem[] {
  return items.filter((_, i) => i !== index);
}

/** Moves an item one place up (-1) or down (1); at the edge nothing moves. */
export function moveItem(items: readonly EditorItem[], index: number, dir: -1 | 1): EditorItem[] {
  const to = index + dir;
  if (index < 0 || index >= items.length || to < 0 || to >= items.length) {
    return [...items];
  }
  const out = [...items];
  [out[index], out[to]] = [out[to], out[index]];
  return out;
}

function wordsOf(raw: string): number | null {
  const t = raw.trim();
  if (!/^\d{1,6}$/.test(t)) {
    return null;
  }
  const n = Number(t);
  return n >= limits.minWords && n <= limits.maxWords ? n : null;
}

export interface EditorCheck {
  ok: boolean;
  /** The first problem in words; "" when ok. */
  problem: string;
  badTitles: boolean[];
  badWords: boolean[];
}

/** The light client check; the core checks again. */
export function checkEditor(items: readonly EditorItem[]): EditorCheck {
  const badTitles = items.map((i) => i.title.trim() === "" || runeLen(i.title.trim()) > limits.titleMax);
  const badWords = items.map((i) => wordsOf(i.words) === null);
  const total = items.reduce((sum, i) => sum + (wordsOf(i.words) ?? 0), 0);
  let problem = "";
  if (items.length < limits.minSections || items.length > limits.maxSections) {
    problem = checkTexts.count;
  } else if (badTitles.some(Boolean)) {
    problem = items.some((i) => runeLen(i.title.trim()) > limits.titleMax) ? checkTexts.titleLong : checkTexts.title;
  } else if (badWords.some(Boolean)) {
    problem = checkTexts.words;
  } else if (total > limits.totalWordsMax) {
    problem = checkTexts.total;
  }
  return { ok: problem === "", problem, badTitles, badWords };
}

/** What EditOutline gets: the order of the editor, number 0 for a new section, trimmed titles. */
export function toOutlineItems(items: readonly EditorItem[]): OutlineItem[] {
  return items.map((i) => ({ number: i.number, title: i.title.trim(), words: wordsOf(i.words) ?? 0 }));
}

/** The editor differs from the saved plan, so EditOutline must be called before the next step. */
export function isEdited(items: readonly EditorItem[], sections: readonly WorkSection[]): boolean {
  if (items.length !== sections.length) {
    return true;
  }
  return items.some((it, i) => {
    const s = sections[i];
    return it.number !== s.number || it.title.trim() !== s.title.trim() || wordsOf(it.words) !== s.words;
  });
}

// ---- Revision ----

export interface ReviseOption {
  /** 0: every section. */
  value: number;
  label: string;
}

export function reviseOptions(sections: readonly WorkSection[]): ReviseOption[] {
  return [{ value: 0, label: workTexts.allSections }, ...sections.map((s) => ({ value: s.number, label: `${s.number}. ${s.title}` }))];
}

/** The note for Regenerate: trimmed and cut to the limit in runes. */
export function noteOf(raw: string): string {
  return [...raw.trim()].slice(0, limits.noteMax).join("");
}

/**
 * The section «Сгенерировать заново» of a blocked work sends: 0 (the plan) only when there are no sections; otherwise
 * the first section not written yet, and 0 if every one is written.
 */
export function blockedNumber(w: WorkWriting): number {
  if (w.sections.length === 0) {
    return 0;
  }
  return w.sections.find((s) => !s.written)?.number ?? 0;
}

export function regenerateBlocked(api: TurnkeyApi, w: TurnkeyWork, note: string): Promise<TurnkeyWork> {
  return api.regenerate(w.id, w.writing === null ? 0 : blockedNumber(w.writing), noteOf(note));
}

// ---- Steps after a plan edit ----

/** `fresh` is the work re-read after a failure that came after the plan was saved (null if not saved or unreadable). */
export type StepResult = { ok: true; work: TurnkeyWork } | { ok: false; error: unknown; fresh: TurnkeyWork | null };

/**
 * Saves the plan when edited, then runs `next`. Only the reply of `next` is returned for the card: the reply of
 * EditOutline is not applied, as it could re-mount the card and drop an error shown on it.
 */
async function afterEdit(api: TurnkeyApi, workId: string, items: readonly EditorItem[], edited: boolean, next: () => Promise<TurnkeyWork>): Promise<StepResult> {
  if (edited) {
    try {
      await api.editOutline(workId, toOutlineItems(items));
    } catch (error) {
      return { ok: false, error, fresh: null };
    }
  }
  try {
    return { ok: true, work: await next() };
  } catch (error) {
    let fresh: TurnkeyWork | null = null;
    if (edited) {
      try {
        fresh = await api.work(workId);
      } catch {
        fresh = null;
      }
    }
    return { ok: false, error, fresh };
  }
}

/** «Утвердить план»: EditOutline if edited, then ApproveOutline. */
export function approvePlan(api: TurnkeyApi, workId: string, items: readonly EditorItem[], edited: boolean): Promise<StepResult> {
  return afterEdit(api, workId, items, edited, () => api.approveOutline(workId));
}

/** «Написать заново» of a done work: EditOutline, then StartWriting. */
export function rewriteWork(api: TurnkeyApi, workId: string, items: readonly EditorItem[], edited: boolean): Promise<StepResult> {
  return afterEdit(api, workId, items, edited, () => api.startWriting(workId));
}
