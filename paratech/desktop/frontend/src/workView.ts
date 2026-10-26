// View logic of the writing cards of a turnkey work (TASK-132): which card a work shows, the progress line, the plan
// editor and its checks, polling. The core decides statuses and steps (ARCH-44); here they are only put into words in
// an impersonal voice. Topic, titles, paragraphs, notes and sources are model or student text: pass them through
// untouched and render them as text nodes only.

import { errorTexts, notFoundText, turnkeyErrorText } from "./bindings/turnkey.js";
import type { LibraryApi } from "./bindings/library.js";
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

/** The limits of a presentation (domain/coursework/work/presentation.go, values/presentation.go). */
export const slideLimits = {
  minSlides: 8,
  maxSlides: 12,
  minWords: 15,
  maxWords: 250,
  totalWordsMin: 600,
  totalWordsMax: 1200,
  titleMax: 120,
} as const;

/** paper: an ordinary work; presentation: slides; lab: a lab report. */
export type EditorMode = "paper" | "presentation" | "lab";

export function editorMode(w: WorkWriting | null): EditorMode {
  return w?.presentation === true ? "presentation" : w?.lab === true ? "lab" : "paper";
}

/** Words of a new slide: inside the limit of a slide. */
export const newSlideWords = "60";

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
  reviseSection: "К чему",
  reviseNote: "Замечания",
  reviseSubmit: "Доработать по замечаниям",
  reviseAllCaption: "Работа будет написана заново целиком с учётом замечаний (платно)",
  allSections: "Вся работа",
  slide: "Слайд",
  section: "Раздел",
  slideNotes: "Заметки докладчика",
  questionsCaption: "Вероятные вопросы",
  answerLabel: "Ответ",
  openTalk: "Открыть текст выступления",
  todoCaption: "Что доделать самому",
  todoDepends: "Выводы зависят от ваших данных",
  manualNotRead: "Методичка не прочитана: отчёт написан по тексту задания.",
  manualNoText: "В методичке нет текста, который можно прочитать: отчёт написан по тексту задания.",
  columnFallback: "Колонка",
  editPlan: "Изменить план",
  rewrite: "Написать заново",
  cancel: "Отмена",
  retry: "Повторить",
  stop: "Остановить",
  stopping: "Остановка…",
  stoppedCaption: "Написание остановлено",
  resume: "Продолжить",
  openWord: "Открыть в Word",
  openPowerPoint: "Открыть в PowerPoint",
  copyNote: "Файл открывается как копия: правки в ней в приложение не возвращаются.",
  addSection: "Добавить раздел",
  titleLabel: "Название раздела",
  wordsLabel: "Объём, слов",
  moveUp: "Выше",
  moveDown: "Ниже",
  remove: "Убрать раздел",
  sources: "Источники",
  downloadPdf: "Скачать PDF",
  savingPdf: "Сохранение PDF…",
  pdfSaved: "PDF сохранён в библиотеке рядом с файлом Word.",
  openPdf: "Открыть PDF",
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

export const slideCheckTexts = {
  count: `Нужно от ${slideLimits.minSlides} до ${slideLimits.maxSlides} слайдов.`,
  title: "У каждого слайда должен быть заголовок.",
  titleLong: `Заголовок слайда — не длиннее ${slideLimits.titleMax} символов.`,
  words: `Объём заметок слайда — целое число слов от ${slideLimits.minWords} до ${slideLimits.maxWords}.`,
  totalMin: `Общий объём выступления — не меньше ${slideLimits.totalWordsMin} слов.`,
  totalMax: `Общий объём выступления — не больше ${slideLimits.totalWordsMax} слов.`,
} as const;

/** Labels of the parts of a lab report. */
export const partLabels: Readonly<Record<string, string>> = {
  goal: "Цель",
  equipment: "Оборудование",
  theory: "Теория",
  procedure: "Ход работы",
  results: "Результаты",
  conclusions: "Выводы",
};

// ---- Which card ----

export interface SectionText {
  key: string;
  heading: string;
  paragraphs: string[];
  /** Parallel to paragraphs: the line is a placeholder («▢ …») of a lab report, shown highlighted. */
  marks: boolean[];
  sources: { text: string; verify: boolean }[];
  /** «Цель», «Выводы», …; "" for another work or the part "other". */
  partLabel: string;
  /** Bullets of a slide, or the columns of a two-column slide. */
  bullets: string[];
  columns: { heading: string; bullets: string[] }[];
  /** A slide: its paragraphs are the speaker notes. */
  slide: boolean;
}

export interface TalkQuestionView {
  question: string;
  answer: string;
}

export interface TodoItem {
  key: string;
  title: string;
  prompt: string;
}

export type DoneExtras = {
  kind: EditorMode;
  /** «Вероятные вопросы» of a presentation; [] when none. */
  questions: TalkQuestionView[];
  /** The speaker text to open, "" when there is none. */
  talkTarget: string;
  /** «Что доделать самому» of a lab report: the placeholders and the fixed line; null for another work. */
  todo: { items: TodoItem[]; depends: string } | null;
  /** The line about the lab manual ("" when it was read, absent or not set yet). */
  manualLine: string;
};

export type WorkCardModel =
  | { type: "none" }
  | { type: "start"; caption: string; hint: string; button: string }
  | { type: "running"; caption: string; line: string }
  | { type: "stopped"; caption: string; line: string; button: string }
  | { type: "approve"; caption: string; hint: string }
  | { type: "blocked"; caption: string; text: string; noteLabel: string; button: string }
  | ({ type: "done"; caption: string; file: { kind: string; name: string }; notice: string; sections: SectionText[] } & DoneExtras);

/** «Раздел N из M» («Слайд N из M» of a presentation) while the parts are written; N never above M. */
export function progressLine(w: WorkWriting): string {
  if (w.step === "outline" || w.total <= 0) {
    return workTexts.planning;
  }
  const n = w.step === "assemble" ? w.total : Math.min(w.done + 1, w.total);
  return `${w.presentation === true ? workTexts.slide : workTexts.section} ${n} из ${w.total}`;
}

const placeholderMark = "▢";

/** The written sections to read: the paragraphs and the sources, as they came. */
export function sectionTexts(sections: readonly WorkSection[]): SectionText[] {
  return sections
    .map((s) => ({
      s,
      paragraphs: s.paragraphs ?? [],
      sources: s.sources ?? [],
      bullets: s.bullets ?? [],
      columns: s.columns ?? [],
    }))
    .filter((x) => x.paragraphs.length > 0 || x.sources.length > 0 || x.bullets.length > 0 || x.columns.length > 0)
    .map(({ s, paragraphs, sources, bullets, columns }, i) => ({
      key: `${s.number}:${i}`,
      heading: `${s.number}. ${s.title}`,
      paragraphs,
      marks: paragraphs.map((p) => p.startsWith(placeholderMark)),
      sources: sources.map((r) => ({ text: r.text, verify: r.verify })),
      partLabel: partLabels[s.part ?? ""] ?? "",
      bullets,
      columns: columns.map((c) => ({ heading: c.heading, bullets: c.bullets })),
      slide: (s.kind ?? "") !== "",
    }));
}

/** The line about the lab manual; the manual is "" until the outline is set, then nothing is said. */
export function manualLine(manual: string): string {
  return manual === "not_read" ? workTexts.manualNotRead : manual === "no_text" ? workTexts.manualNoText : "";
}

/** The speaker text to open (a presentation only): the id of the material, "" when it is not ready. */
export function talkTarget(w: TurnkeyWork): string {
  const wr = w.writing;
  return wr?.presentation === true && wr.step === "none" && wr.result !== "" ? (wr.talkMaterialId ?? "") : "";
}

function doneExtras(w: TurnkeyWork, wr: WorkWriting): DoneExtras {
  const kind = editorMode(wr);
  const lab = kind === "lab";
  return {
    kind,
    questions: kind === "presentation" ? (wr.questions ?? []).map((q) => ({ question: q.question, answer: q.answer })) : [],
    talkTarget: talkTarget(w),
    todo: lab
      ? {
          items: (wr.placeholders ?? []).map((p, i) => ({ key: `${p.section}:${i}`, title: `${p.section}. ${p.title}`, prompt: p.prompt })),
          depends: workTexts.todoDepends,
        }
      : null,
    manualLine: lab ? manualLine(wr.manual ?? "") : "",
  };
}

/**
 * The card of a work. A work that is not confirmed and has no writing shows nothing here (its questions, its card and
 * the helper are turnkeyCardModel, TASK-130). Writing.sections hold the material of a step that runs: it is never shown as a result.
 */
export function workCardModel(w: TurnkeyWork): WorkCardModel {
  const wr = w.writing;
  if (wr === null) {
    return w.status === "confirmed"
      ? { type: "start", caption: workTexts.caption, hint: workTexts.startHint, button: workTexts.start }
      : { type: "none" };
  }
  switch (wr.step) {
    case "outline":
    case "section":
    case "assemble":
      if (wr.active === false) {
        return { type: "stopped", caption: workTexts.stoppedCaption, line: progressLine(wr), button: workTexts.resume };
      }
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
        file: { kind: w.workType === "presentation" || wr.presentation === true ? ".pptx" : workTexts.fileKind, name: wr.topic.trim() === "" ? workTexts.fallbackName : wr.topic },
        notice: workTexts.generated,
        sections: sectionTexts(wr.sections),
        ...doneExtras(w, wr),
      };
  }
}

// ---- Polling and stale replies ----

export const pollMs = 3000;
export const retryMs = 15000;

function isWritingStep(w: TurnkeyWork | null): boolean {
  const step = w?.writing?.step;
  return step === "outline" || step === "section" || step === "assemble";
}

/**
 * A step of the writing is running: the card re-reads the work until it is not. The core sets writing.active from the
 * existence of a pending/running job and StartWriting enqueues it before it replies, so active=false on a writing step
 * means the job is gone (stopped or failed), not "not started yet". parseWork always gives a boolean; the undefined
 * branch (counted as running) exists only for test fixtures that omit the flag.
 */
export function isRunning(w: TurnkeyWork | null): boolean {
  return isWritingStep(w) && w?.writing?.active !== false;
}

/** A writing step without a job: «Написание остановлено», offers «Продолжить». */
export function isStopped(w: TurnkeyWork | null): boolean {
  return isWritingStep(w) && w?.writing?.active === false;
}

/**
 * The wait before the next read, or null to stop. A running step is read every pollMs. A stopped step is still read,
 * slowly (retryMs), so a job that is in fact alive after a transient read error reappears; the loop ends on unmount.
 * Other loaded works are not polled. A stored triage is not polled: the core keeps no problem, so it cannot tell a call
 * in flight from a call that is due («Повторить»).
 */
export function pollDelay(w: TurnkeyWork | null, failed: boolean): number | null {
  if (w !== null && isStopped(w)) {
    return retryMs;
  }
  if (w !== null && !isRunning(w)) {
    return null;
  }
  return failed ? retryMs : pollMs;
}

/** «Остановка…» lasts while the job is active; once it is gone the flag is dropped so a later «Продолжить» shows «Остановить». */
export function nextStopping(prev: boolean, w: TurnkeyWork | null): boolean {
  return prev && w !== null && canStop(w);
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

/**
 * The newest of two replies: an older version never replaces a newer one. The core does not store the problem of a
 * reader call, so a read of the same version without one keeps the problem the reply carried.
 */
export function acceptWork(current: TurnkeyWork | null, next: TurnkeyWork): TurnkeyWork {
  if (current === null) {
    return next;
  }
  if (next.version < current.version) {
    return current;
  }
  if (next.version === current.version && next.problem === "" && current.problem !== "" && next.status === current.status) {
    return { ...next, problem: current.problem, message: current.message };
  }
  return next;
}

// ---- Plan editor ----

/** A section in the editor; words stay text while typed. */
export interface EditorItem {
  /** 0: a new section. */
  number: number;
  title: string;
  words: string;
  /** Slide kind / lab part of a section of the saved plan; absent for a new one. */
  kind?: string;
  part?: string;
}

export const newSectionWords = "300";

export function editorFromSections(sections: readonly WorkSection[]): EditorItem[] {
  return sections.map((s) => ({ number: s.number, title: s.title, words: String(s.words), kind: s.kind ?? "", part: s.part ?? "" }));
}

/** The title slide and the conclusion slide keep their places and cannot be removed. */
function isFixedSlide(it: EditorItem): boolean {
  return it.kind === "title" || it.kind === "conclusion";
}

/** Adds a section; in a presentation before the conclusion slide, within the slide limits. */
export function addItem(items: readonly EditorItem[], mode: EditorMode = "paper"): EditorItem[] {
  if (mode === "presentation") {
    if (items.length >= slideLimits.maxSlides) {
      return [...items];
    }
    const fresh: EditorItem = { number: 0, title: "", words: newSlideWords };
    const last = items.length - 1;
    return last >= 0 && items[last].kind === "conclusion" ? [...items.slice(0, last), fresh, items[last]] : [...items, fresh];
  }
  return items.length >= limits.maxSections ? [...items] : [...items, { number: 0, title: "", words: newSectionWords }];
}

/**
 * A section can be removed unless it is the title or the conclusion slide, or the only section of a lab report with
 * the part results or conclusions.
 */
export function canRemove(items: readonly EditorItem[], index: number): boolean {
  const it = items[index];
  if (it === undefined || isFixedSlide(it)) {
    return false;
  }
  if (it.part === "results" || it.part === "conclusions") {
    return items.some((o, i) => i !== index && o.part === it.part);
  }
  return true;
}

export function removeItem(items: readonly EditorItem[], index: number): EditorItem[] {
  return canRemove(items, index) ? items.filter((_, i) => i !== index) : [...items];
}

/** A section can move one place up (-1) or down (1) unless it or its neighbour is a fixed slide, or at the edge. */
export function canMove(items: readonly EditorItem[], index: number, dir: -1 | 1): boolean {
  const to = index + dir;
  if (index < 0 || index >= items.length || to < 0 || to >= items.length) {
    return false;
  }
  return !isFixedSlide(items[index]) && !isFixedSlide(items[to]);
}

/** Moves an item one place up (-1) or down (1); at the edge or next to a fixed slide nothing moves. */
export function moveItem(items: readonly EditorItem[], index: number, dir: -1 | 1): EditorItem[] {
  if (!canMove(items, index, dir)) {
    return [...items];
  }
  const to = index + dir;
  const out = [...items];
  [out[index], out[to]] = [out[to], out[index]];
  return out;
}

function wordsOf(raw: string, mode: EditorMode = "paper"): number | null {
  const t = raw.trim();
  if (!/^\d{1,6}$/.test(t)) {
    return null;
  }
  const n = Number(t);
  const [min, max] = mode === "presentation" ? [slideLimits.minWords, slideLimits.maxWords] : [limits.minWords, limits.maxWords];
  return n >= min && n <= max ? n : null;
}

export interface EditorCheck {
  ok: boolean;
  /** The first problem in words; "" when ok. */
  problem: string;
  badTitles: boolean[];
  badWords: boolean[];
}

/** The light client check; the core checks again. */
export function checkEditor(items: readonly EditorItem[], mode: EditorMode = "paper"): EditorCheck {
  const slides = mode === "presentation";
  const titleMax = slides ? slideLimits.titleMax : limits.titleMax;
  const [minCount, maxCount] = slides ? [slideLimits.minSlides, slideLimits.maxSlides] : [limits.minSections, limits.maxSections];
  const texts = slides ? slideCheckTexts : checkTexts;
  const badTitles = items.map((i) => i.title.trim() === "" || runeLen(i.title.trim()) > titleMax);
  const badWords = items.map((i) => wordsOf(i.words, mode) === null);
  const total = items.reduce((sum, i) => sum + (wordsOf(i.words, mode) ?? 0), 0);
  let problem = "";
  if (items.length < minCount || items.length > maxCount) {
    problem = texts.count;
  } else if (badTitles.some(Boolean)) {
    problem = items.some((i) => runeLen(i.title.trim()) > titleMax) ? texts.titleLong : texts.title;
  } else if (badWords.some(Boolean)) {
    problem = texts.words;
  } else if (slides && total < slideLimits.totalWordsMin) {
    problem = slideCheckTexts.totalMin;
  } else if (total > (slides ? slideLimits.totalWordsMax : limits.totalWordsMax)) {
    problem = slides ? slideCheckTexts.totalMax : checkTexts.total;
  }
  return { ok: problem === "", problem, badTitles, badWords };
}

/** What EditOutline gets: the order of the editor, number 0 for a new section, trimmed titles. */
export function toOutlineItems(items: readonly EditorItem[], mode: EditorMode = "paper"): OutlineItem[] {
  return items.map((i) => ({ number: i.number, title: i.title.trim(), words: wordsOf(i.words, mode) ?? 0 }));
}

/** The editor differs from the saved plan, so EditOutline must be called before the next step. */
export function isEdited(items: readonly EditorItem[], sections: readonly WorkSection[], mode: EditorMode = "paper"): boolean {
  if (items.length !== sections.length) {
    return true;
  }
  return items.some((it, i) => {
    const s = sections[i];
    return it.number !== s.number || it.title.trim() !== s.title.trim() || wordsOf(it.words, mode) !== s.words;
  });
}

// ---- Revision ----

export interface ReviseOption {
  /** 0: every section. */
  value: number;
  label: string;
}

/** «Вся работа», then «Слайд N. …» (a presentation) or «Раздел N. …». */
export function reviseOptions(sections: readonly WorkSection[], mode: EditorMode = "paper"): ReviseOption[] {
  const unit = mode === "presentation" ? workTexts.slide : workTexts.section;
  return [{ value: 0, label: workTexts.allSections }, ...sections.map((s) => ({ value: s.number, label: `${unit} ${s.number}. ${s.title}` }))];
}

/** «Доработать по замечаниям» needs a note. */
export function canSubmitNote(raw: string): boolean {
  return noteOf(raw) !== "";
}

/** The caption under the choice: only «Вся работа» is rewritten as a whole (paid). */
export function reviseCaption(number: number): string {
  return number === 0 ? workTexts.reviseAllCaption : "";
}

/** «Доработать по замечаниям»: Regenerate(id, 0 or the section, note); an empty note makes no call (null). */
export function reviseWork(api: TurnkeyApi, workId: string, number: number, raw: string): Promise<TurnkeyWork> | null {
  const note = noteOf(raw);
  return note === "" ? null : api.regenerate(workId, number, note);
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
async function afterEdit(api: TurnkeyApi, workId: string, version: number, items: readonly EditorItem[], edited: boolean, next: () => Promise<TurnkeyWork>, mode: EditorMode): Promise<StepResult> {
  if (edited) {
    try {
      await api.editOutlineAt(workId, version, toOutlineItems(items, mode));
    } catch (error) {
      // A stale plan: the card shows the error and the work is read again.
      return { ok: false, error, fresh: turnkeyErrorText(error) === staleText ? await readOrNull(api, workId) : null };
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

export type ActionErrorEvent = { type: "fail"; text: string } | { type: "start" } | { type: "ok" } | { type: "poll" };

/**
 * The error of the last button call. It lives in the parent of the keyed card body: a call can change the step, which
 * re-mounts the body. Only a new call or its success clears it; a poll never does.
 */
export function nextActionError(prev: string, ev: ActionErrorEvent): string {
  return ev.type === "fail" ? ev.text : ev.type === "poll" ? prev : "";
}

/** «Повторить» of a running step whose last call failed: StartWriting on a writing work only enqueues the step again. */
export function retryWriting(api: TurnkeyApi, workId: string): Promise<TurnkeyWork> {
  return api.startWriting(workId);
}

/** «Утвердить план»: EditOutlineAt (the version the plan was shown at) if edited, then ApproveOutline. */
export function approvePlan(api: TurnkeyApi, workId: string, version: number, items: readonly EditorItem[], edited: boolean, mode: EditorMode = "paper"): Promise<StepResult> {
  return afterEdit(api, workId, version, items, edited, () => api.approveOutline(workId), mode);
}

/** «Написать заново» of a done work: EditOutlineAt, then StartWriting. */
export function rewriteWork(api: TurnkeyApi, workId: string, version: number, items: readonly EditorItem[], edited: boolean, mode: EditorMode = "paper"): Promise<StepResult> {
  return afterEdit(api, workId, version, items, edited, () => api.startWriting(workId), mode);
}

/** «Сохранить план»: the same stale rule, the saved work is the result. */
export async function savePlanAt(api: TurnkeyApi, workId: string, version: number, items: readonly EditorItem[], mode: EditorMode = "paper"): Promise<StepResult> {
  try {
    return { ok: true, work: await api.editOutlineAt(workId, version, toOutlineItems(items, mode)) };
  } catch (error) {
    return { ok: false, error, fresh: turnkeyErrorText(error) === staleText ? await readOrNull(api, workId) : null };
  }
}

async function readOrNull(api: TurnkeyApi, workId: string): Promise<TurnkeyWork | null> {
  try {
    return await api.work(workId);
  } catch {
    return null;
  }
}

/** The core's refusal of a plan edit made on an old version (TASK-178). */
export const staleText = errorTexts[6];

/** The core's refusal when a writing job could not be cancelled (TASK-178). */
export const notStoppedText = errorTexts[7];

/**
 * «Остановить»: the reply of the core. `watch` is true while the job is still active (a running step ends
 * asynchronously); the single poller then keeps reading while the step runs. A refusal (ErrNotStopped) rejects.
 */
export async function stopWork(api: Pick<TurnkeyApi, "stopWriting">, workId: string): Promise<{ work: TurnkeyWork; watch: boolean }> {
  const work = await api.stopWriting(workId);
  return { work, watch: canStop(work) };
}

/** «Открыть в Word / PowerPoint»: a presentation is a .pptx, every other ready work a .docx. */
export function openLabel(w: TurnkeyWork): string {
  return w.workType === "presentation" ? workTexts.openPowerPoint : workTexts.openWord;
}

/** The material to open: the ready file, "" if there is none. Only this id crosses to the core. */
export function openTarget(w: TurnkeyWork): string {
  return w.writing?.step === "none" && w.writing.result !== "" ? w.writing.materialId : "";
}

/** «Скачать PDF» belongs to a ready paper or lab report; a presentation has none. */
export function pdfOffered(w: TurnkeyWork): boolean {
  const wr = w.writing;
  return wr !== null && wr.step === "none" && wr.result !== "" && wr.presentation !== true && w.workType !== "presentation";
}

/** The saved PDF to open, "" while there is none. */
export function pdfTarget(w: TurnkeyWork): string {
  return pdfOffered(w) ? (w.writing?.pdfMaterialId ?? "") : "";
}

/** «Скачать PDF» → ExportPDF; a work that offers no PDF makes no call (null). */
export function exportPdf(api: Pick<TurnkeyApi, "exportPdf">, w: TurnkeyWork): Promise<TurnkeyWork> | null {
  return pdfOffered(w) && pdfTarget(w) === "" ? api.exportPdf(w.id) : null;
}

/** «Открыть PDF»: a copy of the saved PDF, like the other open buttons. */
export async function openPdf(library: Pick<LibraryApi, "openMaterial">, w: TurnkeyWork): Promise<OpenView | null> {
  return openById(library, pdfTarget(w));
}

export type OpenView ={ kind: "ok" | "fail"; text: string };

/** Opens the copy of the ready file; a refused or failed open is a fixed text, never a path. */
export async function openWork(library: Pick<LibraryApi, "openMaterial">, w: TurnkeyWork): Promise<OpenView | null> {
  return openById(library, openTarget(w));
}

/** «Открыть текст выступления»: the speaker text of a presentation, a copy as well. */
export async function openTalk(library: Pick<LibraryApi, "openMaterial">, w: TurnkeyWork): Promise<OpenView | null> {
  return openById(library, talkTarget(w));
}

async function openById(library: Pick<LibraryApi, "openMaterial">, id: string): Promise<OpenView | null> {
  if (id === "") {
    return null;
  }
  const r = await library.openMaterial(id);
  return r.status === "opened" ? { kind: "ok", text: r.message } : { kind: "fail", text: r.message };
}

/**
 * «Повторить» next to the error of a running step: only for a failed start/retry call. A refused stop (the fixed text or
 * any other error of that call) never offers it: StartWriting would not undo a stop (TASK-191 L1).
 */
export function retryOffered(error: string, stopFailed: boolean): boolean {
  return error !== "" && !stopFailed && error !== notStoppedText;
}

export const metaTexts = { subject: "Предмет", topic: "Тема", inbox: "Входящие" } as const;

export interface WorkMeta {
  subject: string;
  topic: string;
}

/**
 * Subject and topic of the work for the card header. The subject name comes from the library by `subjectId`; an id the
 * library no longer has is «Входящие» (where the paper goes). Both are student or model text: text nodes only.
 */
export function workMeta(w: TurnkeyWork, subjects: ReadonlyMap<string, string>): WorkMeta {
  // parseWork always gives strings; `||` also covers test fixtures that omit the fields.
  const id = w.subjectId || (w.writing?.subjectId ?? "");
  const topic = (w.topic || (w.writing?.topic ?? "")).trim();
  const subject = id === "" ? "" : (subjects.get(id) ?? metaTexts.inbox);
  return { subject, topic };
}

/** «Предмет: X · Тема: Y», the known parts only; "" when none. */
export function metaLine(m: WorkMeta): string {
  return [m.subject !== "" ? `${metaTexts.subject}: ${m.subject}` : "", m.topic !== "" ? `${metaTexts.topic}: ${m.topic}` : ""]
    .filter((p) => p !== "")
    .join(" · ");
}

/** A stop button shows while a writing job runs. */
export function canStop(w: TurnkeyWork): boolean {
  return w.writing?.active === true;
}
