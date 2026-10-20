// The transcript viewer without React: what to fetch and when, paragraphs, search and highlight pieces. The core owns
// the transcript and every state text; this module only arranges what it returned. Segment text stays plain text:
// the highlight is a list of string pieces the page renders as text nodes and <mark> elements.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import type { RecordingItem } from "./bindings/activity.js";
import {
  errorMessages,
  type TranscriptPage,
  type TranscriptSegment,
  type TranscriptState,
  type TranscriptStatus,
} from "./bindings/transcript.js";

/** How often a running recognition is asked for again. */
export const pollMs = 2000;

/** How often a recording without recognition yet is asked for again: the job may be enqueued later. */
export const idlePollMs = 5000;

/** The longest wait between retries after failed calls. */
export const maxErrorPollMs = 30000;

/** Segments per call; the core caps a page at 1000. */
export const pageSize = 1000;

/** The segments loaded so far: one document, live parts or the stored transcript, never a mix. */
export interface Loaded {
  segments: readonly TranscriptSegment[];
  final: boolean;
}

export const emptyLoaded: Loaded = { segments: [], final: false };

/** Whether the recognition is still going on, so the page keeps asking. */
export function isLive(state: TranscriptState): boolean {
  return state === "waiting" || state === "recognising";
}

/** Whether a recording offers «Открыть расшифровку»: recognition is queued, running, done, or left some text. */
export function canOpenTranscript(st: TranscriptStatus): boolean {
  return isLive(st.state) || st.state === "done" || st.segments > 0;
}

/**
 * Whether a row of «Записи и задачи» offers «Открыть расшифровку», from the chain the list already has (no call per
 * row). The recording is saved and recognition is queued, running or past (also when the summary or the homework
 * waits or failed); hidden while recording or saving and for a failed recording. A failed recognition keeps the
 * button: the parts recognised before the failure are kept.
 */
export function rowOffersTranscript(r: Pick<RecordingItem, "stage" | "errorCode">): boolean {
  switch (r.stage) {
    case "queued":
    case "transcribing":
    case "summarizing":
    case "lectureText":
    case "done":
      return true;
    case "failed":
      // A silent recording is not transcribed (TASK-082); a transcript without speech shows nothing (TASK-110).
      return (
        r.errorCode !== "recordingFailed" &&
        r.errorCode !== "silentRecording" &&
        r.errorCode !== "noSpeech" &&
        r.errorCode !== ""
      );
    default:
      return false;
  }
}

/** The recordings of a lesson, newest first as the core lists them. */
export function lessonRecordings(recordings: readonly RecordingItem[], lessonId: string): RecordingItem[] {
  return lessonId === "" ? [] : recordings.filter((r) => r.lessonId === lessonId);
}

/** What to do after a status: nothing, drop what is shown, or fetch from an index (reset: replace the document). */
export type Plan = { kind: "none" } | { kind: "clear" } | { kind: "fetch"; from: number; reset: boolean };

export function nextFetch(st: TranscriptStatus, loaded: Loaded): Plan {
  const have = loaded.segments.length;
  if (st.segments === 0) {
    return have > 0 ? { kind: "clear" } : { kind: "none" };
  }
  // The stored transcript may differ from the live parts, and fewer segments mean the document was started again.
  if ((st.final !== loaded.final && have > 0) || st.segments < have) {
    return { kind: "fetch", from: 0, reset: true };
  }
  if (st.segments > have) {
    return { kind: "fetch", from: have, reset: false };
  }
  return { kind: "none" };
}

/** Adds a page to the document; null when the page belongs to another document and the page must fetch from 0. */
export function applyPage(loaded: Loaded, page: TranscriptPage): Loaded | null {
  if (page.from === 0) {
    return { segments: page.segments, final: page.final };
  }
  if (page.from !== loaded.segments.length || page.final !== loaded.final) {
    return null;
  }
  return { segments: loaded.segments.concat(page.segments), final: page.final };
}

/** The index to fetch the next page from, or null when the page was the last one. */
export function morePages(loaded: Loaded, page: TranscriptPage): number | null {
  return page.segments.length > 0 && loaded.segments.length < page.total ? loaded.segments.length : null;
}

/**
 * The wait before the next status, or null to stop: errors is the number of failed ticks in a row (0 after a good
 * one), last the latest status. A failed call is retried with a doubling wait, so a transient error (the DB busy right
 * after a restart) never stops the viewer. Only "done" and "failed" stop; the page also stops when it goes away.
 */
export function nextPollMs(last: TranscriptStatus | null, errors: number): number | null {
  if (errors > 0) {
    return Math.min(maxErrorPollMs, pollMs * 2 ** Math.min(errors - 1, 10));
  }
  if (last === null) {
    return pollMs;
  }
  if (last.state === "done" || last.state === "failed") {
    return null;
  }
  return isLive(last.state) ? pollMs : idlePollMs;
}

/** The head of the viewer: the core's message, the progress and what to say while there is no text. */
export interface StatusView {
  message: string;
  failed: boolean;
  /** 0..100 while recognising, null otherwise. */
  progress: number | null;
  /** Under the head while the shown text is the live parts; "" otherwise. */
  partial: string;
  /** Instead of the text while nothing is loaded; "" to show nothing. */
  empty: string;
}

export const partialNote = "Показан текст, распознанный на сейчас: новые фразы появляются сами, после окончания текст обновится.";

const emptyTexts: Readonly<Record<TranscriptState, string>> = {
  none: "",
  waiting: "Текст появится, когда начнётся распознавание.",
  recognising: "Первые фразы появятся через несколько секунд.",
  done: "В записи не распознано ни одной фразы.",
  failed: "",
};

export function statusView(st: TranscriptStatus, loadedCount: number): StatusView {
  return {
    message: st.message,
    failed: st.state === "failed",
    progress: st.state === "recognising" ? Math.min(100, Math.max(0, st.percent)) : null,
    partial: isLive(st.state) && !st.final && loadedCount > 0 ? partialNote : "",
    empty: loadedCount === 0 ? emptyTexts[st.state] : "",
  };
}

/** The text of a segment as the page shows and searches it. */
export function segmentText(seg: TranscriptSegment): string {
  return seg.text.trim();
}

/** A paragraph: consecutive segments with the timestamp of the first. */
export interface Paragraph {
  /** The index of the first segment: stable while segments are appended. */
  first: number;
  start: string;
  startMs: number;
  segments: readonly TranscriptSegment[];
}

/** A pause this long starts a new paragraph. */
export const paragraphGapMs = 2000;
/** A paragraph this long ends at the next sentence end, and at the latest at maxParagraphChars. */
export const softParagraphChars = 400;
export const maxParagraphChars = 900;

const sentenceEnd = /[.!?…]["»”)]*$/;

/** Groups segments into paragraphs; appending segments never changes the earlier paragraphs. */
export function paragraphs(segments: readonly TranscriptSegment[]): Paragraph[] {
  const out: Paragraph[] = [];
  let cur: TranscriptSegment[] = [];
  let chars = 0;
  const flush = () => {
    if (cur.length > 0) {
      out.push({ first: cur[0].index, start: cur[0].start, startMs: cur[0].startMs, segments: cur });
    }
    cur = [];
    chars = 0;
  };
  for (const seg of segments) {
    const prev = cur.length > 0 ? cur[cur.length - 1] : null;
    if (
      prev !== null &&
      (seg.startMs - prev.endMs >= paragraphGapMs ||
        chars >= maxParagraphChars ||
        (chars >= softParagraphChars && sentenceEnd.test(segmentText(prev))))
    ) {
      flush();
    }
    cur.push(seg);
    chars += segmentText(seg).length + 1;
  }
  flush();
  return out;
}

/**
 * Case-folds for search and keeps the length, so an index into the folded text is an index into the original: a
 * character whose lower case has another length stays as it is. «ё» is searched as «е».
 */
export function foldText(s: string): string {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    const l = c.toLowerCase();
    out += l === "ё" ? "е" : l.length === 1 ? l : c;
  }
  return out;
}

/** The query as searched: folded, without spaces at the ends; "" searches nothing. */
export function searchQuery(query: string): string {
  return foldText(query.trim());
}

export interface Range {
  start: number;
  end: number;
}

/** The non-overlapping matches of a folded query in a text. */
export function findRanges(text: string, folded: string): Range[] {
  if (folded === "") {
    return [];
  }
  const hay = foldText(text);
  const out: Range[] = [];
  for (let at = hay.indexOf(folded); at >= 0; at = hay.indexOf(folded, at + folded.length)) {
    out.push({ start: at, end: at + folded.length });
  }
  return out;
}

/** The matches per paragraph and where each paragraph's matches start in the whole list. */
export interface SearchResult {
  total: number;
  counts: number[];
  offsets: number[];
}

export function searchParagraphs(paras: readonly Paragraph[], folded: string): SearchResult {
  const counts: number[] = [];
  const offsets: number[] = [];
  let total = 0;
  for (const p of paras) {
    let n = 0;
    if (folded !== "") {
      for (const seg of p.segments) {
        n += findRanges(segmentText(seg), folded).length;
      }
    }
    offsets.push(total);
    counts.push(n);
    total += n;
  }
  return { total, counts, offsets };
}

/** The current match inside paragraph i, as its number within the paragraph; -1 when it is elsewhere. */
export function localMatch(r: SearchResult, i: number, current: number): number {
  return current >= r.offsets[i] && current < r.offsets[i] + r.counts[i] ? current - r.offsets[i] : -1;
}

/** One piece of a segment's text; match is the number of the match within the paragraph, -1 for plain text. */
export interface Piece {
  text: string;
  match: number;
}

/** A paragraph's segments as text pieces, the matches of the folded query numbered through the paragraph. */
export function paragraphPieces(p: Paragraph, folded: string): Piece[][] {
  let n = 0;
  return p.segments.map((seg) => {
    const text = segmentText(seg);
    const pieces: Piece[] = [];
    let at = 0;
    for (const r of findRanges(text, folded)) {
      if (r.start > at) {
        pieces.push({ text: text.slice(at, r.start), match: -1 });
      }
      pieces.push({ text: text.slice(r.start, r.end), match: n++ });
      at = r.end;
    }
    if (at < text.length || pieces.length === 0) {
      pieces.push({ text: text.slice(at), match: -1 });
    }
    return pieces;
  });
}

/** The next (+1) or previous (-1) match, round the ends; -1 when there is none. */
export function stepMatch(current: number, total: number, dir: 1 | -1): number {
  if (total <= 0) {
    return -1;
  }
  if (current < 0 || current >= total) {
    return dir === 1 ? 0 : total - 1;
  }
  return (current + dir + total) % total;
}

/** "3 из 12", "Не найдено", or "" while there is no query. */
export function matchCountText(current: number, total: number, folded: string): string {
  if (folded === "") {
    return "";
  }
  return total === 0 ? "Не найдено" : `${Math.min(current, total - 1) + 1} из ${total}`;
}

/** The player time of a timestamp. */
export function playSeconds(startMs: number): number {
  return Math.max(0, startMs) / 1000;
}

export const noAudioHint = "Запись недоступна для прослушивания: по меткам времени её не проиграть.";
export const noSelectionHint = "Выделите текст в расшифровке.";
export const playFailedText = "Не удалось проиграть запись.";

/** «Сохранить как…»: what to show; null when the user cancelled the dialog. */
export function saveResultText(r: { status: string; message: string; name: string }): { ok: boolean; text: string } | null {
  if (r.status === "cancelled") {
    return null;
  }
  if (r.status === "saved") {
    return { ok: true, text: r.name !== "" ? `Сохранено: ${r.name}` : r.message };
  }
  return { ok: false, text: r.message };
}

const knownErrors: ReadonlySet<string> = new Set(Object.values(errorMessages));

/** The core's fixed text of a failed call as it is; anything else (a malformed reply) gets the generic text. */
export function errorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return knownErrors.has(msg) ? msg : errorMessages.unavailable;
}
