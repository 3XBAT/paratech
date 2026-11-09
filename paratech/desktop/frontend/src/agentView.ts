// View logic of the agent panel: context label, Ctrl+K, the Russian wording of the cards and which buttons a request
// offers. The core decides statuses and validates cards (ARCH-44); here they are only put into words. The voice is
// impersonal («Создан предмет…», not «Я создал…»). Model text (summary, notes, questions, options, labels, replies) is
// passed through untouched and must be rendered as a text node only, never as HTML.

import type { AddedAssignmentView, AnswerInput, ArgView, AskInput, AssignmentLineView, CallProblem, CardView, ChatProblem, ChatView, ContextFill, EarlierRequestView, LineResultView, MentionView, OfferView, PlanStepView, PlanView, QuestionView, UpdateView, WithdrawResult } from "./bindings/agent.js";
import { estimateTexts, loadLabels } from "./durationEstimate.js";
import { plural } from "./plural.js";
import { askMentions, maxDraftLength, maxMentions, type MentionPill, mentionPills } from "./agentMentions.js";
import { approveLabel, type PlanProgress, planProgress, stepMarks, type StepMark, planWindow } from "./planView.js";
import { endReason, type Entry, hasTicket, isBusyRefused, isFinalStatus, isRunning, orderedEntries } from "./agentState.js";
import { type PageId, pageById } from "./nav.js";
import { dayText, daysBetween, daysLeftText, dueText, parseMoment } from "./dueText.js";
import { doneTexts } from "./assignmentDoneView.js";
import type { IconName } from "./ui/Icon.js";

/** One row of the assignment list of a reply card. */
export interface AssignmentRow {
  id: string;
  title: string;
  /** "" when the subject is unknown. */
  subject: string;
  /** «сегодня до 20:00», «завтра до 18:30», «до сб, 24 окт., 18:30» or «просрочено, пт, 23 окт., 20:00»; "" without a date. */
  due: string;
  /** Overdue: drawn with the urgent colour of «Сегодня». */
  overdue: boolean;
}

export const noTitleLabel = "Без названия";

/** A date-only "YYYY-MM-DD" is a local calendar day (not UTC midnight, which shifts the day west of UTC). */
function parseDue(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s);
  return m === null ? parseMoment(s) : new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
}

/** The local day `d` at "HH:MM"; null when the time is empty or unreadable. */
function atTime(d: Date, time: string): Date | null {
  const m = /^(\d{2}):(\d{2})$/.exec(time);
  if (m === null || Number(m[1]) > 23 || Number(m[2]) > 59) {
    return null;
  }
  return new Date(d.getFullYear(), d.getMonth(), d.getDate(), Number(m[1]), Number(m[2]));
}

/**
 * The deadline moment (date and time) read against `now` in the one wording of dueText: «сегодня до 20:00»,
 * «завтра до 18:30», «до сб, 24 окт., 18:30»; a moment that has passed keeps its date: «просрочено, пт, 23 окт., 20:00».
 * Without a time (an older core) only the day counts.
 */
function dueWhen(d: Date, time: string, now: Date): { text: string; overdue: boolean } {
  const at = atTime(d, time);
  if (at !== null) {
    return { text: dueText(at.toISOString(), now), overdue: at.getTime() <= now.getTime() };
  }
  const days = daysBetween(d, now);
  if (days < 0) {
    return { text: `${daysLeftText(days)}, ${dayText(d, now)}`, overdue: true };
  }
  return { text: days < 2 ? daysLeftText(days) : `до ${dayText(d, now)}`, overdue: false };
}

/** The row of an assignment; the subject name comes from the student's subjects. */
export function assignmentRow(a: AssignmentLineView, subjects: Readonly<Record<string, string>>, now: Date): AssignmentRow {
  const d = parseDue(a.due);
  const when = d === null ? null : dueWhen(d, a.time, now);
  const due = when?.text ?? "";
  const overdue = when?.overdue ?? false;
  const subject = Object.prototype.hasOwnProperty.call(subjects, a.subjectId) ? subjects[a.subjectId] : "";
  return { id: a.id, title: a.title.trim() === "" ? noTitleLabel : a.title, subject, due, overdue };
}

export const addedAssignmentPrefix = "Задание добавлено";
export const openAssignmentLabel = "Открыть";

/**
 * «Задание добавлено: <название> · <предмет> · завтра до 18:30». The DTO does not say whether the student named a
 * time (none arrives as 20:00), so date and time are always shown; the parts that are absent are left out.
 */
export function addedAssignmentText(a: AddedAssignmentView, subjects: Readonly<Record<string, string>>, now: Date): string {
  const title = a.title.trim() === "" ? noTitleLabel : a.title;
  const parts = [title];
  if (Object.prototype.hasOwnProperty.call(subjects, a.subjectId) && subjects[a.subjectId] !== "") {
    parts.push(subjects[a.subjectId]);
  }
  const d = parseDue(a.due);
  if (d !== null) {
    parts.push(dueWhen(d, a.time, now).text);
  }
  return `${addedAssignmentPrefix}: ${parts.join(" · ")}`;
}

/** The agent screen of the core (values.Screen) for a section of the window; the core refuses unknown screens. */
const screens: Record<PageId, string> = {
  today: "today",
  // The Go core has no «recordings» screen and refuses unknown ones (not changed here): the panel asks as on «Сегодня».
  recordings: "today",
  subjects: "subjects",
  knowledge: "subjects",
  tickets: "subjects",
  homework: "assignments",
  schedule: "schedule",
  journal: "journal",
  settings: "settings",
  models: "settings",
  connections: "settings",
  diagnostics: "settings",
  profile: "settings",
  appearance: "settings",
};

export const noSubjectLabel = "Без предмета";

/** The context chip of the panel and the ids a request is asked with. */
export interface AgentContext {
  label: string;
  /** The label can be removed (the request is then asked «Без предмета»). */
  removable: boolean;
  ask: Omit<AskInput, "text">;
}

/**
 * The context by the current screen. The window has no subject selection yet, so the label is the section; removing it
 * drops the ids and the thread becomes «Без предмета». The page opens an assignment: it is part of the context.
 */
export function contextFor(page: PageId, assignmentId: string, removed: boolean): AgentContext {
  const screen = screens[page];
  if (removed) {
    return { label: noSubjectLabel, removable: false, ask: { subjectId: "", lessonId: "", assignmentId: "", screen } };
  }
  return {
    label: pageById(page).label,
    removable: true,
    ask: { subjectId: "", lessonId: "", assignmentId: page === "homework" ? assignmentId : "", screen },
  };
}

/** The request with the chosen «@» objects (kind, id and label only); no `mentions` key when there are none. */
export function askInput(ctx: AgentContext, text: string, mentions: readonly MentionView[] = []): AskInput {
  const list = askMentions(mentions);
  return list === undefined ? { ...ctx.ask, text } : { ...ctx.ask, text, mentions: list };
}

/**
 * True for Ctrl+K (Cmd+K on a Mac keyboard layout), the shortcut of the panel. It reads the physical key: on the
 * Russian layout `key` is «л», `code` stays "KeyK".
 */
export function isFocusPanelKey(e: { code: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }): boolean {
  return (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.code === "KeyK";
}

/** What Ctrl+K does: a closed panel opens (and takes focus), an open one closes. */
export function panelShortcut(open: boolean): "open" | "close" {
  return open ? "close" : "open";
}

/** What a key does in the request field: Enter sends, Shift+Enter breaks the line, Enter confirming an IME word does neither. */
export function composerKey(e: { key: string; shiftKey: boolean; isComposing?: boolean; keyCode?: number }): "send" | "none" {
  if (e.key !== "Enter" || e.shiftKey || e.isComposing === true || e.keyCode === 229) {
    return "none";
  }
  return "send";
}

/** The request field sits under the header until the first request, then at the bottom of the panel like in a chat. */
export function inputPlacement(entryCount: number): "top" | "bottom" {
  return entryCount === 0 ? "top" : "bottom";
}

/** A request text to send: trimmed, null when empty. */
export function requestText(raw: string): string | null {
  const t = raw.trim();
  return t === "" ? null : t;
}

// ---- Chats ----
// Titles, mention labels and replies are typed or model text: the page renders them as text nodes only.

export const chatTexts = {
  newChat: "Новый чат",
  untitled: "Без названия",
  chats: "Чаты",
  list: "Список чатов",
  archive: "Архив",
  toArchive: "В архив",
  restore: "Вернуть",
  rename: "Переименовать",
  renameLabel: "Название чата",
  running: "Идёт запрос",
  archivedMark: "в архиве",
  noChats: "Чатов пока нет.",
  emptyArchive: "Архив пуст.",
  earlierCaption: "Ранее в этом чате",
  openSource: "Открыть исходный чат",
} as const;

/** The core adds this to the title of a fork (domain/agent/thread/fork.go). */
const forkTitleSuffix = " (fork)";

/**
 * A chat title split so the suffix of a fork never falls under the ellipsis: the name is cut, the suffix is kept whole.
 * A title that is only the suffix, or has none, stays one piece.
 */
export function splitForkTitle(title: string): { name: string; suffix: string } {
  if (title.endsWith(forkTitleSuffix) && title.length > forkTitleSuffix.length) {
    return { name: title.slice(0, -forkTitleSuffix.length), suffix: forkTitleSuffix };
  }
  return { name: title, suffix: "" };
}

/** The longest chat title, in characters (runes), as the core checks it. */
export const maxChatTitle = 80;

export type ChatOp = "list" | "open" | "current" | "new" | "rename" | "archive" | "restore";

const chatFailTexts: Record<ChatOp, string> = {
  list: "Не удалось загрузить чаты.",
  open: "Не удалось загрузить прежние запросы чата.",
  current: "Не удалось загрузить текущий запрос чата.",
  new: "Не удалось начать новый чат.",
  rename: "Не удалось переименовать чат.",
  archive: "Не удалось отправить чат в архив.",
  restore: "Не удалось вернуть чат из архива.",
};

export const chatBusyText = "Чат занят: дождитесь конца запроса или остановите его.";
export const chatGoneText = "Чат не найден: список обновлён.";

/** The wording of a problem of a chat call; "" for no problem. An unknown code reads as a failure of the operation. */
export function chatProblemText(p: ChatProblem | string, op: ChatOp): string {
  switch (p) {
    case "":
      return "";
    case "busy":
      return op === "rename" || op === "archive" ? chatBusyText : "Чат занят, попробуйте ещё раз.";
    case "not_found":
      return chatGoneText;
    default:
      return chatFailTexts[op];
  }
}

/** The list is re-read when the chat is gone. */
export function refreshesChats(p: ChatProblem | string): boolean {
  return p === "not_found";
}

export type TitleCheck = { ok: true; title: string } | { ok: false; text: string };

/** A chat title to send: trimmed, not empty, at most 80 characters. */
export function checkChatTitle(raw: string): TitleCheck {
  const title = raw.trim();
  if (title === "") {
    return { ok: false, text: "Название не может быть пустым." };
  }
  if (Array.from(title).length > maxChatTitle) {
    return { ok: false, text: `Название длиннее ${maxChatTitle} знаков.` };
  }
  return { ok: true, title };
}

export interface ChatRow {
  threadId: string;
  /** Never empty. */
  title: string;
  archived: boolean;
  /** A request of the chat runs (the core says so, or the panel sees it). */
  running: boolean;
  lastActive: string;
}

export interface ChatSections {
  active: ChatRow[];
  archived: ChatRow[];
}

function activityMs(s: string): number {
  const t = Date.parse(s);
  return Number.isNaN(t) ? Number.NEGATIVE_INFINITY : t;
}

/** A request of the chat is going on, by what the panel has seen (updates are quicker than the list). */
function runsLocally(entries: readonly Entry[], threadId: string): boolean {
  return entries.some(
    (e) =>
      (e.threadId === threadId && e.update !== null && isRunning(e.update.status)) ||
      (e.threadId === "" && e.callProblem === "" && e.update === null && e.withdrawn !== true && e.scope !== null && (e.scope.threadId ?? "") === threadId),
  );
}

/** Unarchived chats by last activity (newest first), then the archive in the same order; running chats are marked. */
export function chatSections(chats: readonly ChatView[], entries: readonly Entry[]): ChatSections {
  const rows = chats.map(
    (c): ChatRow => ({
      threadId: c.threadId,
      title: c.title.trim() === "" ? chatTexts.untitled : c.title,
      archived: c.archived,
      running: c.running || runsLocally(entries, c.threadId),
      lastActive: c.lastActive,
    }),
  );
  const byActivity = (a: ChatRow, b: ChatRow) => {
    const x = activityMs(a.lastActive);
    const y = activityMs(b.lastActive);
    return x === y ? 0 : y > x ? 1 : -1;
  };
  // Array.sort is stable: chats of equal activity keep the core's order.
  return { active: rows.filter((r) => !r.archived).sort(byActivity), archived: rows.filter((r) => r.archived).sort(byActivity) };
}

/**
 * The chat to show after the list was read: the open one while it exists; otherwise (or while none is open) the newest
 * unarchived one, "" when there is none.
 */
export function reconcileOpenChat(chats: readonly ChatView[], openId: string, entries: readonly Entry[] = []): string {
  if (openId !== "" && chats.some((c) => c.threadId === openId)) {
    return openId;
  }
  return chatSections(chats, entries).active[0]?.threadId ?? "";
}

/** The chat after archiving `id`: stay unless it was the open one, then the newest unarchived. */
export function openAfterArchive(chats: readonly ChatView[], openId: string, id: string, entries: readonly Entry[] = []): string {
  if (openId !== id) {
    return openId;
  }
  return chatSections(chats, entries).active.find((r) => r.threadId !== id)?.threadId ?? "";
}

// ---- The context ring (ADR-0028 «The ring», TASK-208) ----
// The core decides the level (fill.level); the page never turns a percent into a level.

export const ringTexts = {
  /** The start of the tooltip and of the accessible name; a non-breaking space keeps «%» with the number. */
  label: (percent: number) => `Контекст чата: ${percent} %`,
  normal: "Агент помнит весь разговор.",
  high: "Скоро агент начнёт сжимать начало разговора.",
  full: "Память чата почти заполнена: новый чат ответит точнее.",
  /** The history no longer fits and the start was dropped (not necessarily by a compaction); not blaming. */
  overflow: "Начало разговора агент уже не помнит: новый чат ответит точнее.",
  /** The quiet line by the field while the agent compacts the chat (TASK-270). */
  compacting: "Уплотнение контекста…",
  hint: "Начать новый чат? Этот чат почти заполнен и останется в списке.",
  /** Under the field when the start was already dropped (overflow). */
  overflowHint: "Начать новый чат? Начало этого разговора агент уже не помнит; чат останется в списке.",
  hintButton: chatTexts.newChat,
} as const;

export interface RingModel {
  /** Integer 0..100, what the arc is filled to. */
  percent: number;
  /** An overflow reads as «full». */
  level: ContextFill["level"];
  /** The tooltip and the accessible name: «Контекст чата: N %.» and one plain sentence. */
  text: string;
  /** The line under the field with «Новый чат». */
  hint: boolean;
  /** The sentence of that line: the overflow one when the start is already dropped; empty without a hint. */
  hintText: string;
}

/** The ring for a fill; null (unknown fill, or an empty chat at 0 %) draws nothing. */
export function ringModel(fill: ContextFill | null): RingModel | null {
  if (fill === null) {
    return null;
  }
  const percent = Number.isFinite(fill.percent) ? Math.min(100, Math.max(0, Math.floor(fill.percent))) : 0;
  if (percent === 0) {
    return null;
  }
  const level = fill.overflow ? "full" : fill.level;
  const hint = level === "full";
  return {
    percent,
    level,
    text: `${ringTexts.label(percent)}. ${fill.overflow ? ringTexts.overflow : ringTexts[level]}`,
    hint,
    hintText: !hint ? "" : fill.overflow ? ringTexts.overflowHint : ringTexts.hint,
  };
}

/** The last known fill of every chat, by thread id. A chat with an unreadable fill has no entry. */
export type FillMap = Readonly<Record<string, ContextFill>>;

function sameFill(a: ContextFill | undefined, b: ContextFill | undefined): boolean {
  return a === b || (a !== undefined && b !== undefined && a.percent === b.percent && a.level === b.level && a.overflow === b.overflow);
}

/**
 * The chat has a request that is not over: the core sends the update fill only at DONE, STOPPED, FAILED and CANCELLED,
 * so until then the list (which already counts the request) must not move the ring. Covers a request sent but not yet
 * named, and plan or question waits.
 */
function requestOpen(entries: readonly Entry[], threadId: string): boolean {
  if (entries.some((e) => e.threadId === "" && e.callProblem === "" && e.update === null && e.withdrawn !== true && e.scope !== null && (e.scope.threadId ?? "") === threadId)) {
    return true;
  }
  const last = orderedEntries(entries)
    .filter((e) => e.threadId === threadId && e.update !== null)
    .at(-1);
  return last?.update != null && !finishedStatuses.has(last.update.status);
}

/** The fill of a chat the page already knows is kept while its request is not over; otherwise the given one is taken. */
function chosenFill(prev: FillMap, c: ChatView, entries: readonly Entry[]): ContextFill | undefined {
  const keep = c.running || requestOpen(entries, c.threadId) ? prev[c.threadId] : undefined;
  return keep ?? c.fill ?? undefined;
}

/**
 * The fills after the chat list was read. A chat with a request that is not over keeps its last fill; with none known
 * yet it takes the list's. A null fill in the list removes the entry. Chats that are no longer listed are dropped.
 */
export function fillsFromList(prev: FillMap, chats: readonly ChatView[], entries: readonly Entry[]): FillMap {
  const next: Record<string, ContextFill> = {};
  for (const c of chats) {
    const fill = chosenFill(prev, c, entries);
    if (fill !== undefined) {
      next[c.threadId] = fill;
    }
  }
  const same = Object.keys(next).length === Object.keys(prev).length && Object.keys(next).every((id) => sameFill(prev[id], next[id]));
  return same ? prev : next;
}

/** The fills after a chat call (new, rename, archive, restore) replied with the chat: the ring does not wait for the list read. */
export function fillsFromChat(prev: FillMap, chat: ChatView, entries: readonly Entry[]): FillMap {
  const fill = chosenFill(prev, chat, entries);
  return fill === undefined || sameFill(prev[chat.threadId], fill) ? prev : { ...prev, [chat.threadId]: fill };
}

/** The fills after an update: only an update that carries a fill (the end of a request) changes its own chat's. */
export function fillsFromUpdate(prev: FillMap, update: Pick<UpdateView, "threadId" | "fill">): FillMap {
  if (update.fill === null || update.threadId === "" || sameFill(prev[update.threadId], update.fill)) {
    return prev;
  }
  return { ...prev, [update.threadId]: update.fill };
}

/** The chats whose context the agent is compacting right now (TASK-270), by thread id. */
export type CompactingMap = Readonly<Record<string, true>>;

/**
 * The compacting chats after an update. A compaction update (non-empty thread only: an Ask without a chat shows nothing)
 * adds or removes its chat. A real update of a chat also removes it: the request starts only after the compaction, so a
 * lost end update can never leave the note on. Anything else returns the same object.
 */
export function compactingFromUpdate(prev: CompactingMap, update: Pick<UpdateView, "threadId" | "compaction" | "compacting">): CompactingMap {
  const id = update.threadId;
  if (id === "") {
    return prev;
  }
  const has = Object.prototype.hasOwnProperty.call(prev, id);
  if (update.compaction && update.compacting) {
    return has ? prev : { ...prev, [id]: true };
  }
  if (!has) {
    return prev;
  }
  const { [id]: _gone, ...rest } = prev;
  return rest;
}

/** The compacting chats after the chat list was read: chats that are no longer listed are dropped. */
export function compactingFromList(prev: CompactingMap, chats: readonly ChatView[]): CompactingMap {
  const ids = Object.keys(prev);
  const listed = new Set(chats.map((c) => c.threadId));
  if (ids.every((id) => listed.has(id))) {
    return prev;
  }
  return Object.fromEntries(ids.filter((id) => listed.has(id)).map((id) => [id, true as const]));
}

/** Is the open chat being compacted. No chat open: no. */
export function compactingOf(map: CompactingMap, openId: string): boolean {
  return openId !== "" && Object.prototype.hasOwnProperty.call(map, openId);
}

/** The fill to draw for the open chat; null: nothing. */
export function fillOf(fills: FillMap, openId: string): ContextFill | null {
  return openId !== "" && Object.prototype.hasOwnProperty.call(fills, openId) ? fills[openId] : null;
}

/**
 * The requests of the thread that belong to the open chat. Updates carry their own threadId, so the requests of other
 * chats stay in the state untouched and only the open chat's are drawn. A request the core has not named yet belongs to
 * the chat it was asked in (entry.scope.threadId). One without a thread or scope, one asked with no chat (""), and one
 * asked in a chat that is not in `chatIds` (gone) are shown in whichever chat is open, so a failed Ask is never lost.
 */
export function visibleEntries(entries: readonly Entry[], openId: string, chatIds?: ReadonlySet<string>): Entry[] {
  return entries.filter((e) => {
    if (e.threadId !== "") {
      return e.threadId === openId;
    }
    const asked = e.scope?.threadId ?? "";
    if (e.scope === null || asked === "") {
      return true;
    }
    return asked === openId || (chatIds !== undefined && !chatIds.has(asked));
  });
}

export interface EarlierItem {
  key: string;
  turn: number;
  text: string;
  /** The names stored at mention time; a missing one reads as the kind. */
  mentions: MentionPill[];
  /** The reply line; "" when no card was shown. */
  reply: string;
  /** «Начать заново отсюда» from this request; null when it cannot be offered (no number or no text). */
  restart: RestartTarget | null;
}

/**
 * Earlier requests of the open chat as text. A request the panel holds live (its own entry) is left out: it is drawn
 * with its cards. No card buttons here, whatever the `current` flag says.
 */
export function earlierItems(requests: readonly EarlierRequestView[], entries: readonly Entry[], openId: string): EarlierItem[] {
  return requests
    .filter((r) => !entries.some((e) => e.threadId === openId && e.turn === r.turn))
    .map((r) => ({
      key: `earlier:${openId}:${r.turn}`,
      turn: r.turn,
      text: r.text,
      mentions: mentionPills(r.mentions),
      reply: r.reply,
      restart: restartTarget(openId, r.turn, r.text, r.mentions),
    }))
    .sort((a, b) => a.turn - b.turn);
}

/**
 * The mentions of a live request that has no scope of its own (the current request of a chat opened after a restart):
 * the history holds them under the same turn. A request asked in this session keeps them in its scope.
 */
export function restoredMentions(e: Entry, requests: readonly EarlierRequestView[], openId: string): readonly MentionView[] | undefined {
  if (e.scope !== null || e.threadId === "" || e.threadId !== openId) {
    return undefined;
  }
  return requests.find((r) => r.turn === e.turn)?.mentions;
}

// ---- «Начать заново отсюда» → «Скрыть» (ADR-0028, TASK-249) ----
// The core hides the requests from the chosen one on when the next request is sent with `restartFrom`. Here: where the
// action is offered, why «Скрыть» is not available, the pending edit and the request that carries `restartFrom`.

export const restartTexts = {
  action: "Начать заново отсюда",
  title: "Начать заново отсюда?",
  // Under each choice, in the owner's words (2026-10-27).
  hideHint: "Текст сообщения перейдёт в поле ввода. После отправки сообщения ниже пропадут из чата, и агент их забудет. Сделанные действия не отменяются.",
  hide: "Скрыть",
  fork: "Форк",
  forkHint: "Будет создан новый чат с историей до этого сообщения. Этот чат не изменится.",
  forking: "Создаётся…",
  forkNewChat: "Новый чат",
  cancel: "Отмена",
  stop: "Остановить",
  stopping: "Останавливается…",
  cancelRequest: "Отменить запрос",
  cancelling: "Отменяется…",
  note: "Правка сообщения. После отправки всё, что ниже него, пропадёт из чата.",
  undoNote: "Отменить",
  undoLabel: "Отменить правку",
  restoreText: "Вернуть текст сообщения",
} as const;

// ---- The quiet actions under a message (TASK-365) ----
// A row of icons under the student's message («Копировать», «Начать заново отсюда») and under the agent's reply
// («Копировать», «Повторить ответ»). The name is the tooltip (hover and focus) and the aria-label.

export const messageActionTexts = {
  copy: "Копировать",
  copied: "Скопировано",
  copyFailed: "Не удалось скопировать. Выделите текст и нажмите Ctrl+C",
  rerun: "Повторить ответ",
} as const;

/** How long «Скопировано» / the failure hint stays on the button (long enough to read the hint). */
export const copyNoteMs = 2000;

export type CopyState = "idle" | "copied" | "failed";

/** The tooltip (and the live announcement) of the copy button in a state; the aria-label stays «Копировать». */
export function copyNote(s: CopyState): string {
  return s === "copied" ? messageActionTexts.copied : s === "failed" ? messageActionTexts.copyFailed : messageActionTexts.copy;
}

/** Only a message with something in it can be copied. */
export function copyable(text: string): boolean {
  return text.trim() !== "";
}

/** What «Копировать» takes from the agent's reply: the text of its reply cards only, never a service card. */
export function replyCopyText(cards: readonly CardModel[]): string {
  return cards
    .flatMap((c) => (c.type === "reply" && copyable(c.text) ? [c.text.trim()] : []))
    .join("\n\n");
}

/** Copies the text and says how it went; a refusal or an error of the clipboard call is «failed», never thrown. */
export async function copyOutcome(copy: (text: string) => Promise<void>, text: string): Promise<Exclude<CopyState, "idle">> {
  try {
    await copy(text);
    return "copied";
  } catch {
    return "failed";
  }
}

/** The earlier request «Начать заново отсюда» starts from. */
export interface RestartTarget {
  threadId: string;
  /** The number the core knows the request by (from 1). */
  turn: number;
  text: string;
  mentions: MentionView[];
}

/** The target of a request of the open chat; null without a known number (turn < 1) or without a text to put into the field. */
export function restartTarget(openId: string, turn: number, text: string, mentions: readonly MentionView[] | undefined): RestartTarget | null {
  if (openId === "" || !Number.isInteger(turn) || turn < 1 || text.trim() === "") {
    return null;
  }
  return { threadId: openId, turn, text, mentions: (mentions ?? []).slice(0, maxMentions).map((m) => ({ kind: m.kind, id: m.id, label: m.label })) };
}

/**
 * The target of a request the panel holds live; one the core has not named yet (no number, or a placeholder made by
 * «Повторить» that has no update yet) or of another chat has none.
 */
export function liveRestartTarget(e: Entry, requests: readonly EarlierRequestView[], openId: string): RestartTarget | null {
  if (e.threadId === "" || e.threadId !== openId || e.update === null) {
    return null;
  }
  const own = requests.find((r) => r.turn === e.turn);
  return restartTarget(openId, e.turn, e.text !== "" ? e.text : (own?.text ?? ""), e.scope?.mentions ?? own?.mentions);
}

/** The accessible name of the action: the visible words first, then which message (the start of its text). */
export function restartLabel(t: Pick<RestartTarget, "text">): string {
  const flat = t.text.replace(/\s+/g, " ").trim();
  const cut = Array.from(flat);
  return `${restartTexts.action}: ${cut.length > 40 ? `${cut.slice(0, 40).join("")}…` : flat}`;
}

/** What stops «Скрыть» and the way out: the request to stop or cancel, as a button of the dialog. */
export interface RestartBlock {
  reason: string;
  /** The button that removes the cause; null when waiting is the only way (sending, loading, no connection). */
  fix: { kind: "stop" | "cancel"; label: string; entry: Entry } | null;
}

const restartReasons = {
  running: "Агент ещё работает над запросом. Дождитесь конца или остановите его.",
  plan: "Агент ждёт решения по плану: подтвердите его или отмените запрос.",
  answers: "Агент ждёт ответов на вопросы: ответьте на них или отмените запрос.",
  waiting: "Запрос ещё не закончен: дождитесь конца или отмените его.",
  sending: "Запрос ещё отправляется. Подождите несколько секунд.",
  offline: "Нет связи с paratech. Начать заново можно, когда связь появится.",
  loading: "Панель ещё загружается. Подождите несколько секунд.",
} as const;

/**
 * Why «Скрыть» is unavailable right now, null when it is. The same rule as the core's (the current request must be over),
 * read from the requests the panel shows: running, waiting for «Да» or for answers, or just sent. `entries` are those of the
 * open chat; `mode` is the panel mode (the field must accept a request).
 */
export function restartBlock(entries: readonly Entry[], openId: string, mode: { inputEnabled: boolean; offline: boolean }): RestartBlock | null {
  if (!mode.inputEnabled) {
    return { reason: mode.offline ? restartReasons.offline : restartReasons.loading, fix: null };
  }
  if (entries.some((e) => e.threadId === "" && e.callProblem === "" && e.update === null && e.withdrawn !== true && e.scope !== null && (e.scope.threadId ?? "") === openId)) {
    return { reason: restartReasons.sending, fix: null };
  }
  const current = orderedEntries(entries)
    .filter((e) => e.threadId === openId && e.update !== null && e.update.status !== "")
    .at(-1);
  const status = current?.update?.status ?? "";
  if (current === undefined || isFinalStatus(status)) {
    return null;
  }
  if (isRunning(status)) {
    return { reason: restartReasons.running, fix: { kind: "stop", label: restartTexts.stop, entry: current } };
  }
  const reason = status === "AWAITING_PLAN" ? restartReasons.plan : status === "AWAITING_ANSWERS" ? restartReasons.answers : restartReasons.waiting;
  return { reason, fix: { kind: "cancel", label: restartTexts.cancelRequest, entry: current } };
}

/** The edit «Скрыть» started: the field holds the text of `turn` until it is sent or taken back. */
export interface RestartPending extends RestartTarget {
  /** What the field held before the text replaced it; «Отменить» brings it back. */
  before: { draft: string; pills: MentionView[] };
  /** Why the last send was refused ("" when none): shown next to the field. */
  problem: string;
  /** The refused send, kept when the field was busy and could not take it back: «Вернуть текст сообщения» puts it there. */
  unsent: FieldContent | null;
}

/** The pending edit for a target: the field's old content is kept for «Отменить». */
export function startRestart(target: RestartTarget, before: { draft: string; pills: readonly MentionView[] }): RestartPending {
  return { ...target, before: { draft: before.draft, pills: [...before.pills] }, problem: "", unsent: null };
}

/** The text that goes into the field for editing (cut to the field's limit). */
export function restartDraft(target: RestartTarget): string {
  return target.text.slice(0, maxDraftLength);
}

/** What the field holds: the text and the «@» pills. */
export interface FieldContent {
  draft: string;
  pills: MentionView[];
}

/** The dialog (`asking`) and the pending edit; one per panel. */
export interface RestartFlow {
  asking: RestartTarget | null;
  pending: RestartPending | null;
}

/** A step of the flow and the new content of the field, or null when the field stays as it is. */
export interface RestartStep {
  flow: RestartFlow;
  field: FieldContent | null;
}

export const noRestart: RestartFlow = { asking: null, pending: null };

const keep = (flow: RestartFlow): RestartStep => ({ flow, field: null });

/** «Начать заново отсюда» was pressed: the dialog opens, nothing else changes. */
export function openRestart(s: RestartFlow, target: RestartTarget): RestartStep {
  return keep({ ...s, asking: target });
}

/** «Отмена», Esc or a click outside: the dialog closes, nothing else changes. */
export function closeRestartDialog(s: RestartFlow): RestartStep {
  return keep({ ...s, asking: null });
}

/**
 * «Скрыть»: only when nothing blocks it. The message goes into the field (its mentions as pills) and the edit is pending.
 * What the field held is kept for «Отменить» — from the first edit on, so a second pick does not lose it.
 */
export function hideRestart(s: RestartFlow, field: FieldContent, block: RestartBlock | null): RestartStep {
  if (s.asking === null || block !== null) {
    return keep(s);
  }
  const pending = startRestart(s.asking, s.pending?.before ?? field);
  return { flow: { asking: null, pending }, field: { draft: restartDraft(s.asking), pills: s.asking.mentions } };
}

/** «Отменить» next to the field: no restart, the field returns to what it held. */
export function cancelRestartEdit(s: RestartFlow): RestartStep {
  if (s.pending === null) {
    return keep(s);
  }
  return { flow: { ...s, pending: null }, field: { draft: s.pending.before.draft, pills: s.pending.before.pills } };
}

/** Another chat is open (or the open one is gone): the dialog closes and an edit of a different chat is taken back. */
export function restartOnChat(s: RestartFlow, openId: string): RestartStep {
  const closed = { ...s, asking: null };
  return s.pending !== null && s.pending.threadId !== openId ? cancelRestartEdit(closed) : keep(closed);
}

/** The edit goes with the send: it is spent (the field is emptied by the send itself). */
export function spendRestart(s: RestartFlow): RestartStep {
  return keep({ ...s, pending: null });
}

/**
 * The send was refused: the edit is pending again with the reason. `field` is what the field holds now: empty, the text
 * is back in it (`unsent`, the refused send; default the message's own text). Not empty (typed again, pills), the field
 * is left alone, «Отменить» returns it as it is and «Вернуть текст сообщения» offers the refused text.
 */
export function refuseRestart(s: RestartFlow, sent: RestartPending, problem: string, now?: { field?: FieldContent; unsent?: FieldContent }): RestartStep {
  const field = now?.field ?? { draft: "", pills: [] };
  const text = now?.unsent ?? { draft: restartDraft(sent), pills: sent.mentions };
  const why = restartRefusalText(problem);
  if (field.draft === "" && field.pills.length === 0) {
    return { flow: { ...s, pending: { ...sent, problem: why, unsent: null } }, field: text };
  }
  return { flow: { ...s, pending: { ...sent, before: { draft: field.draft, pills: [...field.pills] }, problem: why, unsent: text } }, field: null };
}

/** «Вернуть текст сообщения»: the refused text replaces what the field holds now; «Отменить» brings that back. */
export function restoreUnsent(s: RestartFlow, field: FieldContent): RestartStep {
  const p = s.pending;
  if (p === null || p.unsent === null) {
    return keep(s);
  }
  return { flow: { ...s, pending: { ...p, before: { draft: field.draft, pills: [...field.pills] }, problem: "", unsent: null } }, field: p.unsent };
}

/** What the panel does when the core refused the request sent with `restartFrom`. */
export interface RestartRefusal {
  step: RestartStep;
  /** The refused request leaves the thread (its text is in the field or kept for «Вернуть текст сообщения»). */
  discard: boolean;
}

/**
 * Nothing is restored when the chat is no longer open or another edit has started meanwhile: the refused request stays
 * where it is (its «Отправить ещё раз» sends it without `restartFrom`). Otherwise the edit comes back, whatever the field holds.
 */
export function restartRefusal(s: RestartFlow, sent: RestartPending, problem: string, now: { open: boolean; field: FieldContent; unsent: FieldContent }): RestartRefusal {
  if (!now.open || s.pending !== null) {
    return { step: keep(s), discard: false };
  }
  return { step: refuseRestart(s, sent, problem, now), discard: true };
}

/**
 * A hint text arrived from outside and replaces the field: a pending edit ends there (its pills go too), or the send would
 * hide messages for unrelated text. With no edit the step leaves the field to the caller.
 */
export function restartOnOutsideDraft(s: RestartFlow, text: string): RestartStep {
  if (s.pending === null) {
    return keep(s);
  }
  return { flow: { ...s, pending: null }, field: { draft: text, pills: [] } };
}

/** The request as «Отправить ещё раз» sends it: an ordinary one, a restart is started again from the note. */
export function withoutRestartFrom(ask: AskInput): AskInput {
  const plain = { ...ask };
  delete plain.restartFrom;
  return plain;
}

/** The request with `restartFrom` when an edit of the open chat is pending; otherwise as it came. */
export function withRestart(ask: AskInput, pending: RestartPending | null, openId: string): AskInput {
  if (pending === null || openId === "" || pending.threadId !== openId) {
    return ask;
  }
  return { ...ask, threadId: openId, restartFrom: pending.turn };
}

/** The earlier requests as the chat will show them once the core hid the ones from `from` on. */
export function withoutRestarted<T extends { turn: number }>(requests: readonly T[], from: number): T[] {
  return requests.filter((r) => r.turn < from);
}

/** Why a restart request was refused, in plain words, with the way on. */
export function restartRefusalText(problem: string): string {
  switch (problem) {
    case "busy":
      return "Не удалось отправить: агент ещё занят запросом. Дождитесь конца или остановите его и отправьте снова.";
    case "invalid":
      return "Не удалось отправить: чат изменился. Отмените правку и выберите сообщение ещё раз.";
    case "offline":
      return "Не удалось отправить: нет связи с paratech. Можно повторить, когда связь появится.";
    case "closed":
      return "Не удалось отправить: приложение закрывается.";
    default:
      return "Не удалось отправить. Можно попробовать ещё раз или отменить правку.";
  }
}

// ---- «Форк» (TASK-271) ----
// The core copies the history before the chosen request into a NEW chat (already the current one) and gives back that
// request's text for its field. The old chat is not touched; "Скрыть" is not involved.

/**
 * Why «Форк» did not work, in plain words, and the way on: "retry" is the «Форк» button itself (enabled again), "new" is the
 * «Новый чат» button of the same option (retrying cannot help).
 */
export interface ForkFailure {
  text: string;
  way: "retry" | "new";
}

/** An unknown code, a failed call and "busy" read as one retryable failure; no raw code reaches the page. */
export function forkFailure(problem: string): ForkFailure {
  switch (problem) {
    case "not_found":
      return { text: "Этот чат уже недоступен.", way: "new" };
    case "invalid":
      return { text: "Этого сообщения уже нет в чате: оно скрыто или изменилось. Чат обновлён.", way: "new" };
    default:
      return { text: "Не удалось создать новый чат. Можно повторить.", way: "retry" };
  }
}

/** A refusal that arrives after the dialog was closed goes to the note of the chat bar, with the way to try again. */
export const forkNotCreatedNote = "Новый чат не создан. Можно снова выбрать «Начать заново отсюда» → «Форк».";

/** The refusal that means the chat's earlier requests changed: the panel reads them again. */
export function forkChangedChat(problem: string): boolean {
  return problem === "invalid";
}

const maxNoteTitle = 40;

/** The quiet line after a successful «Форк»: a new chat is open and the source one is as it was. The title is text, cut sensibly. */
export function forkOpenedNote(sourceTitle: string): string {
  const flat = sourceTitle.replace(/\s+/g, " ").trim();
  const chars = Array.from(flat === "" ? chatTexts.untitled : flat);
  const title = chars.length > maxNoteTitle ? `${chars.slice(0, maxNoteTitle).join("").trimEnd()}…` : chars.join("");
  return `Открыт новый чат. Исходный «${title}» не изменился.`;
}

/** The call in flight (one at a time) and the last refusal, which the dialog shows. */
export interface ForkState {
  busy: boolean;
  failure: ForkFailure | null;
}

export const noFork: ForkState = { busy: false, failure: null };

/** «Форк» was pressed: a second press while the call runs starts nothing. */
export function beginFork(s: ForkState): { state: ForkState; start: boolean } {
  return s.busy ? { state: s, start: false } : { state: { busy: true, failure: null }, start: true };
}

/** The call came back with a problem. */
export function endFork(problem: string): ForkState {
  return { busy: false, failure: forkFailure(problem) };
}

/** The dialog opened or closed: an old refusal is forgotten, a call still running is not. */
export function forgetFork(s: ForkState): ForkState {
  return s.failure === null ? s : { ...s, failure: null };
}

function samePills(a: MentionView, b: MentionView): boolean {
  return a.kind === b.kind && a.id === b.id;
}

/**
 * The field of the new chat: the text of the request forked from, with its mentions as pills. A field that already holds
 * typed text is not overwritten (the student's words are not lost): the request follows it after an empty line. Over the
 * field's limit the end of the request is cut, never the typed text.
 */
export function forkField(field: FieldContent, text: string, mentions: readonly MentionView[]): FieldContent {
  const pills = [...field.pills];
  for (const m of mentions) {
    if (pills.length < maxMentions && !pills.some((p) => samePills(p, m))) {
      pills.push({ kind: m.kind, id: m.id, label: m.label });
    }
  }
  const kept = field.draft.trim() === "" ? "" : field.draft.replace(/\s+$/, "");
  const typed = kept === "" || text.trim() === "" ? kept : `${kept}\n\n`;
  const chars = Array.from(`${typed}${text}`);
  return { draft: chars.length > maxDraftLength ? chars.slice(0, maxDraftLength).join("") : chars.join(""), pills };
}

function sameField(a: FieldContent, b: FieldContent): boolean {
  return a.draft === b.draft && a.pills.length === b.pills.length && a.pills.every((p, i) => samePills(p, b.pills[i]));
}

/**
 * «Форк» succeeded; `from` is the request forked from. The dialog closes and the pending «Скрыть» edit (it belongs to the
 * old chat) ends, but the student's words are not lost:
 * - the field still holds the edit's own text: it returns to what it held before the edit, then the request's text follows;
 * - the edited text (and pills) of the same request replaces the request's text, after what the field held before;
 * - the edited text of another request is kept too, between what the field held before and the forked request's text;
 * - after a refused send the field is already what the student typed: it stays, the request's text follows.
 * With no pending edit the typed text stays and the request follows it, as on any chat switch.
 */
export function forkOpened(
  s: RestartFlow,
  field: FieldContent,
  text: string,
  mentions: readonly MentionView[],
  from: { threadId: string; turn: number },
): RestartStep {
  const flow = { asking: null, pending: null };
  const p = s.pending;
  if (p === null || p.unsent !== null) {
    return { flow, field: forkField(field, text, mentions) };
  }
  const base = { draft: p.before.draft, pills: p.before.pills };
  if (sameField(field, { draft: restartDraft(p), pills: p.mentions })) {
    return { flow, field: forkField(base, text, mentions) };
  }
  if (p.threadId === from.threadId && p.turn === from.turn) {
    return { flow, field: forkField(base, field.draft, field.pills) };
  }
  return { flow, field: forkField(forkField(base, field.draft, field.pills), text, mentions) };
}

/** Keys of the warning dialog: Esc takes it back, Tab stays among its `count` buttons, Shift+Tab goes backwards. */
export function dialogKey(e: { key: string; shiftKey: boolean }, at: number, count: number): { act: "cancel" } | { act: "focus"; index: number } | null {
  if (e.key === "Escape") {
    return { act: "cancel" };
  }
  if (e.key === "Tab" && count > 0) {
    const next = e.shiftKey ? (at <= 0 ? count - 1 : at - 1) : at < 0 || at >= count - 1 ? 0 : at + 1;
    return { act: "focus", index: next };
  }
  return null;
}

// ---- Elapsed time of a running request ----

/**
 * «5с», «59с», «1м 0с», «2м 5с»: no space before the unit letter, one between minutes and seconds. A negative or unreadable
 * value reads «0с»; minutes just keep growing (no hours).
 */
export function formatElapsed(seconds: number): string {
  const s = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  return s < 60 ? `${s}с` : `${Math.floor(s / 60)}м ${s % 60}с`;
}

// ---- Texts ----

export const texts = {
  title: "paratech",
  hint: "Ctrl+K",
  send: "Отправить",
  /** Visually hidden text of the waiting dots (the polite live region). */
  waiting: "Агент отвечает…",
  draftCaption: "Ответ пишется · черновик",
  /** Visually hidden status of a typing reply (the reply text itself is never in a live region). */
  replyTyping: "Ответ печатается…",
  replyReady: "Ответ готов",
  /** Small caption under the text a student had read when they pressed «Остановить». */
  replyStopped: "Ответ остановлен",
  /** The stop line of a request that broke off (a lost connection, a failure) while its reply was typing; the text read stays above it. */
  replyCut: "Ответ оборван. Можно повторить.",
  /** The same for a lost connection: says why and when the repeat will work. */
  replyCutOffline: "Ответ оборван: нет связи. Можно повторить, когда связь вернётся.",
  /** The accessible label of a request that waits for the previous one (the dots' region; «waiting» is for a running one). */
  waitingQueued: "Запрос ждёт предыдущий",
  /** A request sent while another one of the chat still runs. */
  queued: "ждёт предыдущий запрос",
  /** The small icon of a waiting question: it takes this question back (the other icon stops the running one). */
  withdraw: "Отменить этот запрос",
  withdrawFailed: "Не удалось отменить запрос. Можно попробовать ещё раз.",
  /** The line of a question taken back while the field held a draft (never overwritten): its text stays in the thread. */
  withdrawnLine: "Запрос отменён. Текст можно скопировать.",
  /** The line of a question refused as busy when five questions already wait in the chat; it replaces the busy line. */
  queueFull: "Не принят: уже ждут пять запросов. Можно отменить один из них или остановить текущий.",
  // The busy-refused request: what its icons do (the line names the way out, the icons carry no visible word).
  busyBoth: "Предыдущий запрос ещё выполняется. Его можно остановить или отправить этот ещё раз.",
  busyStop: "Предыдущий запрос ещё выполняется. Его можно остановить.",
  busyResend: "Предыдущий запрос ещё выполняется. Этот можно отправить ещё раз.",
  stopPrevious: "Остановить предыдущий запрос",
  removeContext: "Убрать контекст",
  offlineBanner: "Нет связи с paratech",
  empty: "Можно задать вопрос или попросить что-нибудь сделать.",
  planCaption: "План",
  /** The plan card's refusal: «Да, выполнить» / «Не выполнять» (plain «Отмена» is for dialogs). */
  planDecline: "Не выполнять",
  progressCaption: "Работа идёт",
  resultCaption: "Готово",
  answerCaption: "Ответ по материалам",
  assignmentsLabel: "Задания",
  cannotDoCaption: "Целиком не получится",
  yourPart: "Остаётся на вашей стороне",
  ourPart: "Берёт на себя paratech",
  confirmCaption: "Нужно подтверждение",
  confirmPending: "Подтверждение пока недоступно.",
  actionDone: "Действие выполнено",
  undo: "Отменить",
  undoAll: "Отменить всё",
  undoBusy: "Отмена…",
  undoDone: "Отменено",
  skip: "Пропустить вопросы",
  submitAnswers: "Ответить",
  reviseLabel: "Что изменить в плане",
  reviseSend: "Отправить",
  textAnswerLabel: "Свой вариант",
  skipTextAnswer: "Как считаешь нужным",
  noArtifact: "Файл недоступен для показа",
  answerReady: "Ответ с источниками подготовлен.",
  stale: "нет связи — может быть неактуально",
  threadNote: "Последняя беседа",
  rerunTitle: messageActionTexts.rerun,
} as const;

/** Button ids of a request; the panel maps them to the calls. */
export type ButtonId = "approve" | "revise" | "cancel" | "stop" | "continue" | "resume" | "retry" | "restart" | "rerun" | "resend";

export const buttonLabels: Record<ButtonId, string> = {
  approve: "Да, выполнить",
  revise: "Изменить",
  cancel: "Отмена",
  stop: "Остановить",
  continue: "Повторить",
  resume: "Продолжить",
  retry: "Повторить",
  restart: "Повторить",
  rerun: "Повторить",
  resend: "Отправить ещё раз",
};

const callProblemTexts: Record<Exclude<CallProblem, "">, string> = {
  invalid: "Запрос не принят: проверьте текст.",
  offline: "Нет связи с paratech.",
  closed: "Приложение закрывается.",
  unavailable: "Пока недоступно.",
  busy: "Действие уже меняется, попробуйте ещё раз.",
  failed: "Не удалось отправить запрос.",
};

/** The wording of a refused call. */
export function callProblemText(p: CallProblem): string {
  return p === "" ? "" : callProblemTexts[p];
}

// The icons carry no visible word, so a line whose way on is an icon says it in the sentence, without quoting a button.
const updateProblemTexts: Record<string, string> = {
  busy: "Предыдущий запрос ещё выполняется.",
  stale: "Запрос устарел: нужно задать его заново.",
  invalid: "Запрос не принят: проверьте текст.",
  budget: "Остановлено: лимит запроса.",
  retry_required: "Шаг не удался. Можно повторить.",
  restart_required: "Запрос устарел. Можно повторить.",
  rerun_blocked: "Повтор недоступен: запрос уже изменил данные. Можно отменить действия или задать вопрос заново.",
  failed: "Не удалось выполнить запрос.",
};

interface StopText {
  text: string;
  /** What the way on is, said when its icon is shown (dropped when the stop card does not offer it). */
  hint: string;
  buttons: ButtonId[];
}

/** Why a request stopped: the text of the stop card and the buttons it offers. */
const stopTexts: Record<string, StopText> = {
  // «Повторить» is Retry (a new key) since TASK-396: «Продолжить» with the same key stays for the student's stop and the quota.
  // An offline stop with no open step is refused by Retry; the panel then continues it (retryWatchStep).
  offline: { text: "Не завершено: нет связи.", hint: "Можно повторить, когда связь вернётся.", buttons: ["retry", "cancel"] },
  // «Остановить» keeps «Продолжить» (the same free replay): «Повторить» would promise another attempt, which is the rerun button.
  user: { text: "Остановлено.", hint: "Можно продолжить.", buttons: ["resume", "cancel"] },
  // The request does not continue by itself after a top-up: the retry is the next step.
  quota: { text: "Не завершено: закончились минуты обработки.", hint: "Повторить можно после пополнения.", buttons: ["continue", "cancel"] },
  failed: { text: "Не получилось выполнить шаг.", hint: "Можно повторить.", buttons: ["retry"] },
  expired: { text: "Запрос устарел.", hint: "Можно повторить.", buttons: ["restart"] },
  budget: { text: "Остановлено: лимит запроса", hint: "", buttons: [] },
};

const wayOn: ReadonlySet<ButtonId> = new Set(["continue", "resume", "retry", "restart"]);

/** The line of a stop card: the reason, then the way on while the card still offers it. */
function stopLine(s: StopText): string {
  return s.hint !== "" && s.buttons.some((b) => wayOn.has(b)) ? `${s.text} ${s.hint}` : s.text;
}

// ---- Plan ----

/** Impersonal names of the steps of a plan. */
const toolTexts: Record<string, string> = {
  list_subjects: "Просмотр предметов",
  find_lessons: "Поиск пар",
  list_assignments: "Просмотр заданий",
  job_status: "Проверка хода работы",
  delivery_status: "Проверка отправки письма",
  search: "Поиск по материалам",
  answer_question: "Ответ по материалам",
  inspect_upload: "Просмотр загруженного файла",
  create_assignment: "Добавление задания",
  create_subject: "Создание предмета",
  create_folder: "Создание папки",
  place_upload: "Размещение файла",
  link_material: "Привязка материала",
  set_lesson_info: "Уточнение данных пары",
  make_summary: "Подготовка конспекта",
  make_lecture_text: "Подготовка текста лекции",
  find_homework: "Поиск заданий",
  generate_draft: "Подготовка решения",
  edit_due: "Изменение срока",
  triage_assignment: "Разбор задания",
  write_work: "Подготовка работы",
  propose_deadline: "Предложение срока",
  propose_send: "Подготовка письма",
  propose_delete_recording: "Предложение удалить запись",
  // Wave 10 (ADR-0033 §10).
  propose_subjects_from_schedule: "Предметы из расписания",
  change_assignment: "Изменение задания",
  mark_assignments_done: "Отметка заданий выполненными",
  // The student's own notes are «Мой конспект» (glossary); «Конспект» alone is what paratech makes.
  list_notes: "Просмотр «Мой конспект»",
  note: "Работа с «Мой конспект»",
  get_settings: "Просмотр настроек",
  set_setting: "Изменение настройки",
  quota_status: "Проверка остатка минут",
  recording: "Запись",
  offer_button: "Предложена кнопка",
};

const idKindTexts: Record<string, string> = {
  subject: "предмет",
  folder: "папка",
  lesson: "пара",
  recording: "запись",
  material: "материал",
  upload: "файл",
  assignment: "задание",
  work: "работа",
  version: "версия",
};

/** YYYY-MM-DD as DD.MM.YYYY; anything else stays as it came (shown as text). */
export function dateText(v: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(v);
  return m ? `${m[3]}.${m[2]}.${m[1]}` : v;
}

function argText(a: ArgView): string {
  switch (a.type) {
    case "date":
      return `срок ${dateText(a.value)}`;
    case "label":
    case "enum":
      return a.value;
    case "id":
      return idKindTexts[a.idKind] ?? "";
    case "ids":
      return a.values.length > 0 ? `${idKindTexts[a.idKind] ?? "объекты"}: ${a.values.length}` : "";
    default:
      return "";
  }
}

/** «Создание предмета · <метка>»: the tool's name, then its labels, dates and values. An unknown tool is generic. */
export function stepText(step: PlanStepView): string {
  const head = toolTexts[step.tool] ?? "Шаг";
  const parts = step.args.map(argText).filter((p) => p !== "");
  const later = step.deferred.length > 0 ? ["после предыдущих шагов"] : [];
  return [head, ...parts, ...later].join(" · ");
}

// ---- Artifacts ----

const artifactKinds: Record<string, string> = {
  recordings: "Запись",
  transcripts: "Расшифровка",
  exports: "Файл",
  materials: "Материал",
};

export interface ArtifactLabel {
  /** «Запись», «Расшифровка», «Файл» or «Материал». */
  kind: string;
  /** The file name only, never the directories. */
  name: string;
}

/**
 * Reads the artifact reference "<kind>/<path>" of a result card into a label. It is only a label: the panel opens
 * nothing by a path. A kind outside the known ones or a path that is absolute, climbs up or has control characters
 * gives null.
 */
export function parseArtifact(ref: string): ArtifactLabel | null {
  const i = ref.indexOf("/");
  if (i <= 0) {
    return null;
  }
  const kind = artifactKinds[ref.slice(0, i)];
  const path = ref.slice(i + 1);
  const bad =
    kind === undefined ||
    path === "" ||
    path.startsWith("/") ||
    path.includes("\\") ||
    path.includes(":") ||
    /[\u0000-\u001f\u007f]/.test(path) ||
    path.split("/").some((seg) => seg === ".." || seg === "." || seg === "");
  if (bad) {
    return null;
  }
  return { kind, name: path.slice(path.lastIndexOf("/") + 1) };
}

// ---- Questions ----

/** `default` of a choice that preselects nothing (the subject question): the user has to pick. */
const noDefault = -1;

/**
 * The answers of «Пропустить вопросы»: the preselected option of a choice, a neutral text otherwise.
 * A choice without a default takes its LAST option (the neutral «Без предмета»), never the first.
 */
export function skipAnswers(questions: readonly QuestionView[]): AnswerInput[] {
  return questions.map((q, i) =>
    q.kind === "choice"
      ? { question: i, option: q.default === noDefault ? q.options.length - 1 : q.default, text: "" }
      : { question: i, option: 0, text: texts.skipTextAnswer },
  );
}

/** The typed answers; null while a text question is empty or a choice without a default is not picked (the form stays closed). */
export function collectAnswers(
  questions: readonly QuestionView[],
  picked: Readonly<Record<number, number>>,
  typed: Readonly<Record<number, string>>,
): AnswerInput[] | null {
  const out: AnswerInput[] = [];
  for (const [i, q] of questions.entries()) {
    if (q.kind === "choice") {
      const option = picked[i] ?? q.default;
      if (option === noDefault) {
        return null;
      }
      out.push({ question: i, option, text: "" });
      continue;
    }
    const t = (typed[i] ?? "").trim();
    if (t === "") {
      return null;
    }
    out.push({ question: i, option: 0, text: t });
  }
  return out;
}

/** «Вопросы · раунд 1 из 2». */
export function roundCaption(round: number): string {
  return `Уточнение · раунд ${round} из 2`;
}

// ---- Cards ----

export interface ButtonModel {
  id: ButtonId;
  label: string;
}

export type CardModel =
  | {
      type: "plan";
      caption: string;
      summary: string;
      steps: string[];
      notes: string[];
      buttons: ButtonModel[];
      /** Number of steps. */
      total: number;
      /** Steps a collapsed list leaves out (0 up to the limit). */
      hidden: number;
      /** Marker of every step, or [] while the core sends no step states. */
      marks: StepMark[];
      /** «Шаг k из N» while executing; null otherwise. */
      progress: PlanProgress | null;
    }
  | { type: "questions"; caption: string; questions: QuestionView[]; active: boolean; skip: string; submit: string }
  | { type: "progress"; caption: string; detail: string; buttons: ButtonModel[] }
  | { type: "waiting"; buttons: ButtonModel[] }
  | { type: "result"; caption: string; artifact: ArtifactLabel | null; fallback: string }
  | { type: "answer"; caption: string; text: string; answerId: string }
  | { type: "reply"; text: string; assignments: AssignmentLineView[] }
  | { type: "cannot_do"; caption: string; yourTitle: string; yourPart: string[]; ourTitle: string; ourPart: string[] }
  | { type: "confirm"; caption: string; text: string; target: { kind: "proposal" | "preview" | "assignment"; id: string } | null }
  | {
      type: "action";
      /** The sentence of the line. */
      text: string;
      /** Plain muted lines under the sentence (the saved text of a note); [] for most lines. */
      detail: string[];
      actionId: string;
      /** `shown` false: the line has no «Отменить» (the stop of a recording cannot be undone, or the line offers a button). */
      undo: { kind: UndoKind; label: string; busyLabel: string; enabled: boolean; shown: boolean };
      /** What the line says once the undo is done. */
      undoneText: string;
      /** The button the line offers once the undo is done («Начать запись» after «Остановить»); null when none. */
      afterUndoOffer: OfferModel | null;
      assignment: AddedAssignmentView | null;
      /** The button the line offers instead of «Отменить»; null when none. */
      offer: OfferModel | null;
    }
  | { type: "unknown"; text: string }
  /** The schedule_subjects card: the review of subjects from the schedule, read from the core by this token. */
  | { type: "schedule"; token: string }
  /** The offer_button card: the page's own button, no heading. */
  | { type: "offer"; offer: OfferModel }
  | { type: "stopped"; text: string; buttons: ButtonModel[] }
  | { type: "work"; workId: string }
  | { type: "problem"; text: string; buttons: ButtonModel[] };

function buttons(ids: ButtonId[]): ButtonModel[] {
  return ids.map((id) => ({ id, label: buttonLabels[id] }));
}

// ---- Words or icons on the buttons of the cards (TASK-368) ----
// Owner rule: a small or secondary button is an icon with a tooltip (its old words) and an aria-label. The words stay on
// the main action of a card («Да, выполнить») and on its yes/no pair («Не выполнять»). TASK-371 (owner 03.11): the stopped
// and problem lines of the chat are small lines next to a message, not cards with a main action, so their «Повторить»,
// «Продолжить» and «Отправить ещё раз» are icons too (the line itself says what happened).

export type ButtonCard = "plan" | "progress" | "waiting" | "stopped" | "problem";

/** The icon of a secondary card button; null: the button is a main action (or half of a yes/no pair) and keeps its word. */
export function cardButtonIcon(id: ButtonId, card: ButtonCard): IconName | null {
  switch (id) {
    case "revise":
      return "pencil";
    case "stop":
      return "stop";
    case "cancel":
      // «Не выполнять» answers the plan's question; «Отменить запрос» of a stopped request is a way out.
      return card === "plan" ? null : "x";
    case "continue":
    case "retry":
    case "restart":
    case "rerun":
    case "resend":
      // Another try of the same request (the words «Повторить» / «Отправить ещё раз»).
      return "retry";
    case "resume":
      return "play";
    default:
      return null;
  }
}

/** What the secondary button of an action line does: stops a started recording, or undoes the action. */
export type UndoKind = "undo" | "stop";

/** The icon of the secondary button of an action line, by what it does (never by its words). */
export function undoIcon(kind: UndoKind): IconName {
  return kind === "stop" ? "stop" : "undo";
}

/** Page buttons an offer names: «Отменить» (undo_action) is an icon, the others are the card's main action and keep words. */
export function offerIcon(offer: OfferView): IconName | null {
  return offer.action === "undo_action" ? "undo" : null;
}

/**
 * The target a confirm card names: a `confirm` card a proposal or a deletion preview, a `delete_assignment` card the
 * assignment; anything else has no card to show.
 */
function confirmTarget(c: CardView): { kind: "proposal" | "preview" | "assignment"; id: string } | null {
  const t = c.target;
  if (t === null || t.id === "") {
    return null;
  }
  if (c.kind === "delete_assignment") {
    return t.kind === "assignment" ? { kind: "assignment", id: t.id } : null;
  }
  return t.kind === "proposal" || t.kind === "preview" ? { kind: t.kind, id: t.id } : null;
}

// ---- Lines of the actions of wave 10 (ADR-0033 §10) ----

export const resultTexts = {
  // The words of the assignment toast (assignmentDoneView.ts), for one assignment and for several.
  markedDone: "Отмечено выполненным",
  markedDoneMany: doneTexts.markedMany,
  markedUndone: doneTexts.unmarked,
  markedUndoneMany: doneTexts.unmarkedMany,
  noteText: "Записано в «Мой конспект»",
  noteUpload: "Файл добавлен в «Мой конспект»",
  noteMove: "«Мой конспект» перенесён",
  noteRename: "«Мой конспект» переименован",
  noteRemove: "«Мой конспект» удалён",
  recordingStarted: "Запись началась",
  recordingStopped: "Запись остановлена",
  /** The undo of a started recording stops it and keeps the audio. */
  stopRecording: "Остановить",
  stopping: "Остановка…",
  startRecording: "Начать запись",
  /** The stub of a card kind a newer core sends (no actions: nothing to press). */
  unknownCard: "Эту часть ответа нельзя показать в этой версии paratech. Обновите приложение.",
} as const;

/** Setting lines by the core's setting name; the words are those of the settings page. */
const settingOnOff: Readonly<Record<string, { on: string; off: string }>> = {
  // The settings page calls it «Автоматически предлагать запись в начале пары».
  recording_prompts: { on: "Предложение записи в начале пары включено", off: "Предложение записи в начале пары выключено" },
  ac_power_only: { on: "Обработка только от розетки включена", off: "Обработка только от розетки выключена" },
};

/** The most excerpt lines a note line shows (the core sends at most 3). */
const maxExcerptLines = 3;

/** A sentence and the plain lines under it. */
export interface Wording {
  text: string;
  detail: string[];
}

/** «Отмечено выполненным: 1 задание» for one, «Отмечены выполненными: 3 задания» for several. */
function counted(one: string, many: string, count: number): Wording | null {
  if (!Number.isInteger(count) || count < 1) {
    return null;
  }
  return { text: `${count === 1 ? one : many}: ${count} ${plural(count, "задание", "задания", "заданий")}`, detail: [] };
}

function settingWording(r: LineResultView): Wording | null {
  if (r.setting === "recognition_load") {
    return Object.hasOwn(loadLabels, r.value)
      ? { text: `${estimateTexts.setting}: ${loadLabels[r.value as keyof typeof loadLabels].toLowerCase()}`, detail: [] }
      : null;
  }
  const words = Object.hasOwn(settingOnOff, r.setting) ? settingOnOff[r.setting] : undefined;
  if (words === undefined || (r.value !== "on" && r.value !== "off")) {
    return null;
  }
  return { text: r.value === "on" ? words.on : words.off, detail: [] };
}

/** The sentence of a result; null for a kind or value the panel does not know (the generic line says «Действие выполнено»). */
export function resultWording(r: LineResultView): Wording | null {
  const plain = (text: string): Wording => ({ text, detail: [] });
  switch (r.kind) {
    case "marked_done":
      return counted(resultTexts.markedDone, resultTexts.markedDoneMany, r.count);
    case "marked_undone":
      return counted(resultTexts.markedUndone, resultTexts.markedUndoneMany, r.count);
    case "note_text": {
      const lines = r.excerpt.filter((l) => l.trim() !== "").slice(0, maxExcerptLines);
      return lines.length === 0 ? plain(resultTexts.noteText) : { text: `${resultTexts.noteText}:`, detail: lines };
    }
    case "note_upload":
      return plain(resultTexts.noteUpload);
    case "note_move":
      return plain(resultTexts.noteMove);
    case "note_rename":
      return plain(resultTexts.noteRename);
    case "note_remove":
      return plain(resultTexts.noteRemove);
    case "recording_started":
      return plain(resultTexts.recordingStarted);
    case "recording_stopped":
      return plain(resultTexts.recordingStopped);
    case "setting":
      return settingWording(r);
    default:
      return null;
  }
}

/** A page button an offer names, with its label from the template; the model supplies no label. */
export interface OfferModel {
  label: string;
  offer: OfferView;
}

/** The reason a press of the call link gives if it ever arrives: no page offers it yet (TASK-261), so no button is drawn. */
export const offerUnavailableText = "Недоступно в этой версии";

/**
 * The offers of ADR-0033 §3: the label is the one the page's own button carries (test/agentwave10cards324b pins them to
 * the pages' constants), `target` the kind of the one id the button needs ("" none). A button that only navigates is
 * labelled by what happens («Открыть «Подключения»»). The call link has no page yet: no button at all.
 */
interface OfferSpec {
  label: string;
  target: "" | "material" | "recording" | "work" | "lesson";
  /** No page offers it in this version: the model draws nothing (a dead button is worse than none). */
  unavailable?: boolean;
}

// ADR-0033 §3 binds google_sign_in and open_help to the page's own buttons; the sign-in needs the page's fields (the
// stored client) and the only help is the calendar guide there, so both lead to «Подключения» and are named so.
const openConnections = `Открыть «${pageById("connections").label}»`;

export const offerSpecs: Readonly<Record<string, OfferSpec>> = {
  open_material: { label: "Открыть", target: "material" },
  open_call_link: { label: "Открыть ссылку на пару", target: "lesson", unavailable: true },
  copy_call_link: { label: "Скопировать ссылку на пару", target: "lesson", unavailable: true },
  open_summary: { label: "Открыть конспект", target: "recording" },
  open_lecture_text: { label: "Открыть текст лекции", target: "recording" },
  open_transcript: { label: "Открыть расшифровку", target: "recording" },
  // The work's file opens in Word / PowerPoint, where «Сохранить как» is: the pages have no save dialog for a work.
  save_as: { label: "Открыть работу", target: "work" },
  google_sign_in: { label: openConnections, target: "" },
  google_sync: { label: "Синхронизировать", target: "" },
  download_model: { label: "Открыть модели речи", target: "" },
  sound_check: { label: "Проверить звук", target: "" },
  open_help: { label: openConnections, target: "" },
  undo_action: { label: "Отменить", target: "" },
  start_recording: { label: resultTexts.startRecording, target: "" },
};

/**
 * The button of an offer; null for an action the panel does not know, or one whose id is missing or of another kind (no
 * button is better than a made-up or a dead one).
 */
export function offerModel(offer: OfferView | null | undefined): OfferModel | null {
  if (offer === null || offer === undefined || !Object.hasOwn(offerSpecs, offer.action)) {
    return null;
  }
  const spec = offerSpecs[offer.action];
  if (spec.unavailable === true || (spec.target !== "" && (offer.target === null || offer.target.kind !== spec.target || offer.target.id === ""))) {
    return null;
  }
  return { label: spec.label, offer };
}

/**
 * A card the Go Driver sends after the reply: a confirm card (also delete_assignment), an action line, the schedule review
 * and the stop line. The streamed reply is drawn, and a cut-off one kept, right above the first of them, so the final reply
 * card takes the place of the draft without jumping over them.
 */
export function isLateCard(c: CardModel): boolean {
  return c.type === "confirm" || c.type === "action" || c.type === "schedule" || c.type === "stopped";
}

/** The index of the first late card, or -1. */
export function firstLateCard(cards: readonly CardModel[]): number {
  return cards.findIndex(isLateCard);
}

/**
 * The React key of a card. Late cards are keyed by what they are (target, action, token), not by their place: the reply
 * card arriving above them must not remount a confirm card (its state, its focus). Others carry kind, caption and index so
 * a new card does not inherit the typed state of the old one.
 */
export function cardKey(c: CardModel, index: number): string {
  const byIndex = `${c.type}:${"caption" in c ? c.caption : ""}:${index}`;
  switch (c.type) {
    case "confirm":
      return c.target === null ? `${c.type}:${c.caption}` : `confirm:${c.target.kind}:${c.target.id}`;
    case "action":
      return c.actionId === "" ? byIndex : `action:${c.actionId}`;
    case "schedule":
      return c.token === "" ? byIndex : `schedule:${c.token}`;
    default:
      return byIndex;
  }
}

/**
 * The keys of a whole list, all different. The planner is untrusted and may propose the same target twice, and every
 * confirm card without a target has the same base key, so a repeated key gets `#2`, `#3`... The counter counts the EARLIER
 * cards with the same key, not the list position, so a reply card arriving above does not change the key of a confirm card.
 */
export function cardKeys(cards: readonly CardModel[]): string[] {
  const seen = new Map<string, number>();
  const used = new Set<string>();
  return cards.map((c, i) => {
    const base = cardKey(c, i);
    let n = (seen.get(base) ?? 0) + 1;
    let key = n === 1 ? base : `${base}#${n}`;
    // A made-up suffix may equal another card's own key; step on until it is free.
    while (used.has(key)) {
      n += 1;
      key = `${base}#${n}`;
    }
    seen.set(base, n);
    used.add(key);
    return key;
  });
}

/** The action line card. */
export type ActionCardModel = Extract<CardModel, { type: "action" }>;

function actionModel(c: CardView): ActionCardModel {
  const result = c.result ?? null;
  const wording = result === null ? null : resultWording(result);
  const offer = offerModel(c.offer);
  const started = result?.kind === "recording_started";
  // The stop of a recording has no compensation, and a line that offers a button has no «Отменить» (ADR-0033 §2, О-7 а).
  const shown = offer === null && result?.kind !== "recording_stopped";
  return {
    type: "action",
    text: wording?.text ?? texts.actionDone,
    detail: wording?.detail ?? [],
    actionId: c.actionId,
    undo: {
      kind: started ? "stop" : "undo",
      label: started ? resultTexts.stopRecording : texts.undo,
      busyLabel: started ? resultTexts.stopping : texts.undoBusy,
      enabled: c.actionId !== "",
      shown,
    },
    // Pressing «Остановить» is not an undo in the student's eyes: the line then says what happened.
    undoneText: started ? resultTexts.recordingStopped : texts.undoDone,
    // After «Остановить» the way on is a new recording.
    afterUndoOffer: started ? offerModel({ action: "start_recording", target: null }) : null,
    assignment: c.assignment ?? null,
    offer,
  };
}

const activeStatuses = new Set(["PLANNING", "EXECUTING"]);

/** The plan card: one «Да» for the whole plan, named by the number of steps. */
function planModel(plan: PlanView, status: string): Extract<CardModel, { type: "plan" }> {
  const total = plan.steps.length;
  const states = plan.steps.map((s) => s.state);
  const planLabels: Partial<Record<ButtonId, string>> = { approve: approveLabel(total), cancel: texts.planDecline };
  const awaiting = buttons(["approve", "revise", "cancel"]).map((b) => ({ ...b, label: planLabels[b.id] ?? b.label }));
  return {
    type: "plan",
    caption: texts.planCaption,
    summary: plan.summary,
    steps: plan.steps.map(stepText),
    notes: plan.notes,
    buttons: status === "AWAITING_PLAN" ? awaiting : [],
    total,
    hidden: planWindow(total, false, -1).hidden,
    marks: stepMarks(states, status),
    progress: planProgress(states, status),
  };
}

function cardModel(c: CardView, status: string): CardModel | null {
  switch (c.kind) {
    case "plan":
      if (!c.plan) {
        return null;
      }
      return planModel(c.plan, status);
    case "questions":
      if (!c.questions) {
        return null;
      }
      return {
        type: "questions",
        caption: roundCaption(c.questions.round),
        questions: c.questions.questions,
        active: status === "AWAITING_ANSWERS",
        skip: texts.skip,
        submit: texts.submitAnswers,
      };
    case "progress":
      return { type: "progress", caption: texts.progressCaption, detail: "", buttons: activeStatuses.has(status) ? buttons(["stop"]) : [] };
    case "result":
      return { type: "result", caption: texts.resultCaption, artifact: parseArtifact(c.artifact), fallback: texts.noArtifact };
    case "answer":
      return { type: "answer", caption: texts.answerCaption, text: texts.answerReady, answerId: c.answerId };
    case "reply":
      return { type: "reply", text: c.text, assignments: c.assignments ?? [] };
    case "cannot_do":
      if (!c.cannotDo) {
        return null;
      }
      return {
        type: "cannot_do",
        caption: texts.cannotDoCaption,
        yourTitle: texts.yourPart,
        yourPart: c.cannotDo.yourPart,
        ourTitle: texts.ourPart,
        ourPart: c.cannotDo.ourPart,
      };
    case "confirm":
    case "delete_assignment":
      return { type: "confirm", caption: texts.confirmCaption, text: texts.confirmPending, target: confirmTarget(c) };
    case "action_line":
      return actionModel(c);
    case "unknown":
      return { type: "unknown", text: resultTexts.unknownCard };
    case "schedule_subjects":
      // The token of the package the desktop built; the card reads everything else from the core.
      return c.target !== null && c.target.kind === "schedule_subjects" && c.target.id !== "" ? { type: "schedule", token: c.target.id } : null;
    case "offer_button": {
      const offer = offerModel(c.offer);
      return offer === null ? null : { type: "offer", offer };
    }
    case "work":
      // The card reads the work itself (TurnkeyService.Work); a work card without an id has nothing to read.
      return c.workId !== "" ? { type: "work", workId: c.workId } : null;
    case "stopped":
      return null; // Built once from the stop reason below.
  }
}

/** What «Отменить» of an action line did: running, done, or the Russian text of a refusal. */
export interface UndoNote {
  busy: boolean;
  done: boolean;
  text: string;
}

/** The action lines of a request that offer «Отменить» (for «Отменить всё»). */
export function undoableActions(cards: readonly CardModel[]): string[] {
  return cards.flatMap((c) => (c.type === "action" && c.undo.shown && c.undo.enabled ? [c.actionId] : []));
}

/**
 * The latest action of the chat that can still be undone, for the offer «Отменить» (undo_action): `requests` are the cards
 * of the chat's requests in order, `notes` what «Отменить» already did per action. null when there is none.
 */
export function latestUndoableAction(requests: readonly (readonly CardModel[])[], notes: Readonly<Record<string, UndoNote | undefined>>): string | null {
  return latestUndoableCard(requests, notes)?.actionId ?? null;
}

/** The same action line as a card, so the offer can say what it would undo. */
export function latestUndoableCard(requests: readonly (readonly CardModel[])[], notes: Readonly<Record<string, UndoNote | undefined>>): ActionCardModel | null {
  for (let i = requests.length - 1; i >= 0; i--) {
    const open = (requests[i] ?? []).filter(
      (c): c is ActionCardModel => c.type === "action" && c.undo.shown && c.undo.enabled && notes[c.actionId]?.done !== true && notes[c.actionId]?.busy !== true,
    );
    if (open.length > 0) {
      return open[open.length - 1] ?? null;
    }
  }
  return null;
}

/** The sentence of an action line (the added assignment reads with its subject and deadline); no trailing colon. */
export function actionWording(c: ActionCardModel, subjects: Readonly<Record<string, string>>, now: Date): string {
  const text = c.assignment ? addedAssignmentText(c.assignment, subjects, now) : c.text;
  return text.replace(/:$/, "");
}

/** What a request shows: the student's text, the cards in order, and the state line while nothing else says it. */
export interface EntryModel {
  key: string;
  /** What the student typed; "" for a request from the history. */
  userText: string;
  /** The «@» objects of the request, with the names stored at mention time. */
  mentions: MentionPill[];
  /** The refusal text when the call failed, "" otherwise. */
  callLine: string;
  /** The call runs and nothing has come back yet: the waiting dots show instead of a line. */
  waiting: boolean;
  /**
   * The question waits in the core's queue under a ticket: «ждёт предыдущий запрос», a stop for the running one and
   * «Отменить этот запрос» (withdraw), no timer.
   */
  queued: boolean;
  /** The row offers the stop of the running request: only the oldest waiting question of the chat does. */
  stopPrevious: boolean;
  /** The question was taken back and its text stays as a quiet cancelled line (no waiting row, no icons but the copy). */
  withdrawn: boolean;
  /** The attempt (1..3) of the automatic retry of the step the request runs, shown beside the timer; 0: none. */
  retry: number;
  cards: CardModel[];
  /** «Повторить» is offered under the request. */
  rerun: boolean;
}

/** What the thread says about a request that the request alone does not know. */
export interface EntryContext {
  /** The request is the last one of the thread. */
  last: boolean;
  /** A running request blocks this refused one, and «Остановить» can stop it. */
  blocked: boolean;
  /** The mentions of a request restored after a restart (no scope of its own); the scope's own win. */
  mentions?: readonly MentionView[];
  /** The request ended while its reply was typing: the text read stays above, and a lost connection or a failure says the reply broke off. */
  replyCut?: boolean;
  /** A refused-as-busy question found five questions already waiting in its chat (queueIsFull): the line says so. */
  queueFull?: boolean;
  /** The question is the oldest waiting one of its chat (isFirstQueued); absent: yes. */
  firstQueued?: boolean;
}

/** The core retries a failed step by itself this many times (backoff.InteractiveRetries). */
export const maxAutoRetries = 3;

/** «Повторная попытка 2 из 3», the quiet line beside the timer while the core retries a step. */
export function retryAttemptText(attempt: number): string {
  return `Повторная попытка ${attempt} из ${maxAutoRetries}`;
}

/** The attempt to show: 1..3 while the request runs, else 0 (a stop or an answer ends the retry). */
function retryOf(u: UpdateView): number {
  return activeStatuses.has(u.status) && Number.isInteger(u.retry) && u.retry >= 1 && u.retry <= maxAutoRetries ? u.retry : 0;
}

/**
 * The visually hidden status of one request (a polite live region that stays in place, so its texts are announced as it
 * changes): null when the request has none. «Ответ печатается…» while the reply draft is shown, «Ответ готов» when the
 * final reply card has replaced it; empty while a request runs without a draft (the waiting dots speak). The reply text
 * itself never goes here.
 */
export function replyStatus(s: { typing: boolean; wasTyping: boolean; replyCard: boolean; running: boolean }): string | null {
  if (s.typing) {
    return texts.replyTyping;
  }
  if (s.wasTyping && s.replyCard) {
    return texts.replyReady;
  }
  return s.running ? "" : null;
}

const finishedStatuses = new Set(["DONE", "FAILED", "CANCELLED", "STOPPED"]);

export function entryModel(e: Entry, ctx: EntryContext = { last: false, blocked: false }): EntryModel {
  const callLine = e.callProblem !== "" ? callProblemText(e.callProblem) : "";
  const withdrawn = e.withdrawn === true && e.update === null;
  const waiting = e.callProblem === "" && e.update === null && !withdrawn;
  const queued = waiting && hasTicket(e);
  const base = {
    key: e.key,
    userText: e.text,
    mentions: mentionPills(e.scope?.mentions ?? ctx.mentions),
    callLine,
    waiting,
    queued,
    stopPrevious: queued && ctx.firstQueued !== false,
    withdrawn,
    retry: 0,
    rerun: false,
  };
  const u = e.update;
  if (u === null) {
    return { ...base, cards: [] };
  }

  const cards: CardModel[] = [];
  for (const c of u.cards) {
    const m = cardModel(c, u.status);
    if (m) {
      cards.push(m);
    }
  }
  if (!cards.some((c) => c.type === "progress") && activeStatuses.has(u.status)) {
    cards.push({ type: "waiting", buttons: buttons(["stop"]) });
  }

  // One stop card, from the stopped card or the update's own reason; the problem adds a button for what to do next.
  // A running request hides the stopped card: it is a leftover of the previous run (progress updates keep the cards
  // and move only the status). The problem is fresh: a progress update clears the old one in the state.
  const running = activeStatuses.has(u.status);
  const problem = u.problem;
  const stopCard = running ? undefined : u.cards.find((c) => c.kind === "stopped");
  const reason = running ? "" : stopCard?.stopReason || u.stopReason;
  let stop: StopText | null = reason !== "" ? (stopTexts[reason] ?? { text: "Остановлено", hint: "", buttons: [] }) : null;
  // A reply that broke off in the middle (a lost connection, a failure): the same retry icon, a clearer line. The text the
  // student read stays above it, so the line is the only caption. Other reasons keep their own line.
  const cutHere = ctx.replyCut === true && (reason === "failed" || reason === "offline");
  if (stop && cutHere) {
    stop = { ...stop, text: reason === "offline" ? texts.replyCutOffline : texts.replyCut, hint: "" };
  }
  // The problem's own «Повторить» is the next step: the stop card must not offer a second way on, nor a second «Повторить».
  if (stop && (problem === "retry_required" || problem === "restart_required")) {
    stop = { ...stop, buttons: stop.buttons.filter((b) => b !== "continue" && b !== "resume" && b !== "retry" && b !== "restart") };
  }
  if (stop) {
    // The same action is «Отменить запрос» here as in the restart dialog.
    const stopButtons = buttons(stop.buttons).map((b) => (b.id === "cancel" ? { ...b, label: restartTexts.cancelRequest } : b));
    cards.push({ type: "stopped", text: stopLine(stop), buttons: stopButtons });
  }
  // A cut reply says it in the stop line already; the generic «Не удалось выполнить запрос.» under it would be a second line.
  const coveredByCut = cutHere && problem === "failed";
  if (problem !== "" && !coveredByCut) {
    let next: ButtonId[] = problem === "retry_required" ? ["retry"] : problem === "restart_required" ? ["restart"] : [];
    let text = updateProblemTexts[problem] ?? updateProblemTexts.failed;
    // A failure with no stop reason of its own: the problem line is the one that says the reply broke off.
    if (ctx.replyCut === true && !stop && problem === "failed") {
      text = texts.replyCut;
    }
    let nextButtons = buttons(next);
    // A request refused as busy has no thread: it can stop the one that blocks it, or be sent again. Only the last one.
    if (isBusyRefused(e) && ctx.last) {
      const canStop = ctx.blocked;
      const canResend = e.text !== "" && e.scope !== null;
      // Five questions already wait: sending again would be refused the same way, so only the stop stays and the line
      // is the one sentence pair about the queue. «Отправить ещё раз» returns once the queue is not full (recomputed per render).
      const full = ctx.queueFull === true;
      next = [...(canStop ? (["stop"] as const) : []), ...(canResend && !full ? (["resend"] as const) : [])];
      // Two icons: the line says what each one does, and the stop one names the request it stops.
      nextButtons = buttons(next).map((b) => (b.id === "stop" ? { ...b, label: texts.stopPrevious } : b));
      if (full) {
        text = texts.queueFull;
      } else if (canStop && canResend) {
        text = texts.busyBoth;
      } else if (canStop) {
        text = texts.busyStop;
      } else if (canResend) {
        text = texts.busyResend;
      }
    }
    cards.push({ type: "problem", text, buttons: nextButtons });
  } else if (!stop && u.status === "CANCELLED") {
    cards.push({ type: "stopped", text: "Отменено", buttons: [] });
  }

  // The new-request «Повторить» only under the last request, and not where the stop card or the problem already offers the next step.
  const offered = [...(stop?.buttons ?? []), ...(problem === "retry_required" ? ["retry" as const] : []), ...(problem === "restart_required" ? ["restart" as const] : [])];
  // After rerun_blocked it stays: the core checks again (the effects may have been undone since).
  const rerun =
    ctx.last &&
    e.threadId !== "" &&
    finishedStatuses.has(u.status) &&
    !offered.includes("retry") &&
    !offered.includes("restart") &&
    // The stop card's `continue` button is worded «Повторить» too: two of them under one request would be confusing.
    // `resume` («Продолжить», after «Остановить») does not clash: the rerun stays.
    !offered.includes("continue");
  return { ...base, cards, rerun, retry: retryOf(u) };
}

// ---- «Отменить этот запрос» of a waiting question (TASK-399) ----

/** What the panel does with the result of Withdraw: "thrown" is a call that failed before an answer. */
export interface WithdrawStep {
  /** The row leaves the thread (its text returns to the empty field). */
  discard: boolean;
  /** The question's text (and mentions) return to the field. */
  restore: boolean;
  /** The row stays as a quiet cancelled line (the field is busy, so the text could not return): never dropped. */
  keep: boolean;
  /** The input takes focus: the student goes on typing. */
  focus: boolean;
  /** A short line for the student; "" says nothing. */
  note: string;
}

/**
 * withdrawn: the question never ran. With an EMPTY field the row goes and the text returns there; with a busy field (a
 * typed draft is never overwritten) the row stays as a cancelled line whose text can be copied, so nothing typed is lost.
 * not_found / invalid: the question is not in the queue and will never run, the same. Either way the input takes focus.
 * started: it began meanwhile, the update that carries its ticket replaces the row. failed or a thrown call: the row
 * stays and the line says it can be tried again.
 */
export function withdrawStep(result: WithdrawResult | "thrown", fieldEmpty: boolean): WithdrawStep {
  switch (result) {
    case "withdrawn":
    case "not_found":
    case "invalid":
      return { discard: fieldEmpty, restore: fieldEmpty, keep: !fieldEmpty, focus: true, note: "" };
    case "started":
      return { discard: false, restore: false, keep: false, focus: false, note: "" };
    default:
      return { discard: false, restore: false, keep: false, focus: false, note: texts.withdrawFailed };
  }
}

// ---- «Повторить» after an offline stop (TASK-396, TASK-399) ----
// An offline stop with an open step is retried (a new key). One with NO open step (its calls waited for the Driver) has
// no key to change: Retry refuses it as an invalid transition, which reaches the panel as an update of the request with
// no status and the problem "invalid" (driver.go problemOf), and Continue is what works. The panel presses Continue once,
// so the student clicks once and sees no error in between. No other problem leads there (budget, busy... are shown).

/** The request a «Повторить» of an offline stop was pressed on. */
export interface RetryWatch {
  threadId: string;
  turn: number;
  /** The key of its entry, for the guard of the buttons. */
  key: string;
}

/** The request stopped offline: its «Повторить» may be refused for lack of an open step. */
export function stoppedOffline(u: UpdateView | null): boolean {
  return u !== null && u.status === "STOPPED" && endReason(u) === "offline";
}

export type RetryWatchStep = "refused" | "went_on" | "keep";

/** One watch per request, so two requests (or chats) never share a refusal. */
export function retryWatchKey(r: { threadId: string; turn: number }): string {
  return `${r.threadId}:${r.turn}`;
}

/**
 * What an update means for the watched request: "refused" (no status, problem "invalid": continue instead and hide this
 * update), "went_on" (the request moved on or another refusal came: stop watching and show it), "keep" (another request,
 * or the repeat of the offline stop itself). A stop with a problem (a budget...) or for another reason is a real answer.
 */
export function retryWatchStep(w: Pick<RetryWatch, "threadId" | "turn">, u: UpdateView): RetryWatchStep {
  if (u.threadId !== w.threadId || u.turn !== w.turn || u.compaction || u.queued) {
    return "keep";
  }
  if (u.status === "") {
    return u.problem === "invalid" ? "refused" : "went_on";
  }
  return u.status === "STOPPED" && u.problem === "" && endReason(u) === "offline" ? "keep" : "went_on";
}
