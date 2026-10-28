// View model of the review of subjects proposed from the schedule (TASK-312, ADR-0033 §4, §7). Pure, no React: the
// selection, the counters, the choices sent to the core and the state machine idle -> saving -> done / problem.
// Names, spellings and aliases are schedule text (untrusted): they are only returned as strings for text nodes and input
// values, and never put into a log or an error.

import {
  type CreateFromScheduleReply,
  type ScheduleAliased,
  type ScheduleChoice,
  type ScheduleCreated,
  type ScheduleGroup,
  type ScheduleKindCount,
  type ScheduleKept,
  type ScheduleMemberChoice,
  type ScheduleSkipped,
  type ScheduleSubjectsReply,
  scheduleKindWords,
  type UndoFromScheduleReply,
} from "./bindings/library.js";
import { plural } from "./plural.js";

/** Russian texts of the review; each refusal says why in plain words and the state offers the way out as a button. */
export const reviewTexts = {
  label: "Предметы из расписания",
  intro: "Отмеченные предметы будут созданы. Название можно поправить прямо в строке.",
  open: "Завести предметы из расписания",
  importSchedule: "Импортировать расписание",
  checking: "Проверяется расписание…",
  emptyText: "Предметы заводятся из расписания и записей, либо вручную. В каждом предмете появятся папки для лекций, семинаров, материалов и заданий.",
  doubtful: "похоже на один предмет",
  separate: "Отдельно",
  skip: "Не заводить",
  skipped: "Не будет заведено",
  checkbox: "Завести предмет",
  checkboxAdd: "Добавить названия к предмету",
  nameLabel: "Название предмета",
  separateLabel: "Название отдельного предмета",
  spellings: "Написания",
  close: "Закрыть",
  creating: "Создаётся…",
  undoAll: "Отменить всё",
  undoing: "Отменяется…",
  retry: "Повторить",
  refresh: "Обновить список",
  refreshing: "Обновляется…",
  toSubjects: "К предметам",
  nothingSelected: "Отметьте хотя бы один предмет",
  fixNames: "Исправьте названия в отмеченных строках",
  createPlain: "Создать предметы",
  allDone: "Все предметы из расписания уже заведены",
  listUnavailable: "Расписание сейчас недоступно. Можно повторить через несколько секунд",
  emptyName: "Введите название",
  tooLong: "Не больше 200 знаков",
  duplicate: "Такое название уже есть в этом списке",
  exists: "Предмет с таким названием уже есть",
  nameTaken: "Такое название уже занято. Измените название в строке и повторите",
  stale: "Расписание или предметы изменились",
  createInvalid: "Список уже нельзя применить — он устарел",
  busy: "Предыдущее действие ещё выполняется",
  unavailable: "Не удалось связаться с локальным хранилищем. Выбор сохранён, действие можно повторить",
  thrown: "Не удалось получить ответ от локального хранилища. Выбор сохранён, действие можно повторить",
  undoBusy: "Ещё идёт предыдущее действие",
  undoInvalid: "Отменить уже нельзя — приложение перезапускалось. Заведённые предметы остались в списке",
  nothingRemoved: "Ничего не убрано",
  tooManyAliases: "У предмета уже много написаний — новые не добавлены",
  subjectGone: "Предмет был удалён — строка пропущена",
  fallbackKeptAliasesSubject: "предмета",
} as const;

/** The most spellings shown under a row. */
const maxSpellingsShown = 5;

/** The longest subject name (subject.MaxNameRunes). */
const maxNameChars = 200;

// ---------------------------------------------------------------------------------------------------------------------
// Words and numbers

export function subjectsWord(n: number): string {
  return `${n} ${plural(n, "предмет", "предмета", "предметов")}`;
}

export function lessonsWord(n: number): string {
  return `${n} ${plural(n, "пара", "пары", "пар")}`;
}

function recordingsWord(n: number): string {
  return `${n} ${plural(n, "запись", "записи", "записей")}`;
}

/** «лекции 12 · семинары 6»; one kind of unknown lessons says nothing the count of lessons does not. */
export function kindsText(kinds: readonly ScheduleKindCount[]): string {
  const shown = kinds.filter((k) => k.lessons > 0);
  if (shown.length === 0 || (shown.length === 1 && shown[0]?.kind === "unknown")) {
    return "";
  }
  return shown.map((k) => `${scheduleKindWords[k.kind]} ${k.lessons}`).join(" · ");
}

/** «14 пар · лекции 12 · семинары 6 · 2 записи» for a group or a member. */
export function metaText(x: { lessons: number; recordings: number; kinds: readonly ScheduleKindCount[] }): string {
  return [x.lessons > 0 ? lessonsWord(x.lessons) : "", kindsText(x.kinds), x.recordings > 0 ? recordingsWord(x.recordings) : ""]
    .filter((s) => s !== "")
    .join(" · ");
}

/** The spellings under a row; the rest is counted, not listed. */
export function spellingsText(spellings: readonly string[]): string {
  if (spellings.length === 0) {
    return "";
  }
  const shown = spellings.slice(0, maxSpellingsShown).join(" · ");
  const more = spellings.length - maxSpellingsShown;
  return `${reviewTexts.spellings}: ${shown}${more > 0 ? ` и ещё ${more}` : ""}`;
}

export function omittedNote(n: number): string {
  const rows = plural(n, "редкая строка", "редкие строки", "редких строк");
  const verb = plural(n, "не показана", "не показаны", "не показаны");
  return `Ещё ${n} ${rows} расписания ${verb}`;
}

// ---------------------------------------------------------------------------------------------------------------------
// The selection

export type MemberMode = "keep" | "separate" | "skip";

export interface MemberSel {
  mode: MemberMode;
  /** The name of a member taken separately. */
  name: string;
}

export interface RowSel {
  checked: boolean;
  /** The name of the subject to create (edited in the row). */
  name: string;
  members: ReadonlyMap<string, MemberSel>;
}

/** By group key; a Map, as the keys are schedule text. */
export type Selection = ReadonlyMap<string, RowSel>;

/** Everything is checked, the names are the proposed ones, doubtful members are merged (О-12 б). */
export function initialSelection(groups: readonly ScheduleGroup[]): Selection {
  const sel = new Map<string, RowSel>();
  for (const g of groups) {
    sel.set(g.key, {
      checked: true,
      name: g.name,
      members: new Map(g.members.map((m) => [m.key, { mode: "keep" as const, name: m.name }])),
    });
  }
  return sel;
}

function withRow(sel: Selection, group: string, change: (r: RowSel) => RowSel): Selection {
  const row = sel.get(group);
  if (row === undefined) {
    return sel;
  }
  return new Map(sel).set(group, change(row));
}

function withMember(sel: Selection, group: string, member: string, change: (m: MemberSel) => MemberSel): Selection {
  return withRow(sel, group, (r) => {
    const m = r.members.get(member);
    return m === undefined ? r : { ...r, members: new Map(r.members).set(member, change(m)) };
  });
}

export function setChecked(sel: Selection, group: string, checked: boolean): Selection {
  return withRow(sel, group, (r) => ({ ...r, checked }));
}

export function setName(sel: Selection, group: string, name: string): Selection {
  return withRow(sel, group, (r) => ({ ...r, name }));
}

/** A second click on the pressed button returns the member to «вместе». */
export function toggleMember(sel: Selection, group: string, member: string, mode: "separate" | "skip"): Selection {
  return withMember(sel, group, member, (m) => ({ ...m, mode: m.mode === mode ? "keep" : mode }));
}

export function setMemberName(sel: Selection, group: string, member: string, name: string): Selection {
  return withMember(sel, group, member, (m) => ({ ...m, name }));
}

/** The mode of a member: only a doubtful member can leave its group. */
function modeOf(row: RowSel, m: { key: string; doubtful: boolean }): MemberSel {
  const sel = row.members.get(m.key);
  return sel !== undefined && m.doubtful ? sel : { mode: "keep", name: "" };
}

// ---------------------------------------------------------------------------------------------------------------------
// Names and the plan

/** The key two names are compared by: trimmed, one space, lower case, «ё» as «е». The core's own check follows. */
export function nameKey(s: string): string {
  return s.trim().replace(/\s+/gu, " ").toLowerCase().replaceAll("ё", "е");
}

export type NameProblem = "empty" | "tooLong" | "duplicate" | "exists";

export const nameProblemTexts: Readonly<Record<NameProblem, string>> = {
  empty: reviewTexts.emptyName,
  tooLong: reviewTexts.tooLong,
  duplicate: reviewTexts.duplicate,
  exists: reviewTexts.exists,
};

/** The id of a group row in Plan.problems. */
export const groupItem = (group: string): string => `g:${group}`;

/** The id of a member taken separately in Plan.problems. */
export const memberItem = (group: string, member: string): string => `m:${group}\u0000${member}`;

export interface Plan {
  /** Subjects that will really be created from the selection. */
  creates: number;
  /** Spellings that will be added to existing subjects. */
  additions: number;
  /** Name problems of the subjects that will be created, by groupItem / memberItem. */
  problems: ReadonlyMap<string, NameProblem>;
}

/** What the selection makes of the proposal. `taken` holds the nameKey of the names of existing subjects. */
export function planOf(groups: readonly ScheduleGroup[], sel: Selection, taken: ReadonlySet<string>): Plan {
  const items: { id: string; name: string }[] = [];
  const spellings = new Set<string>();
  for (const g of groups) {
    const row = sel.get(g.key);
    if (row === undefined || !row.checked) {
      continue;
    }
    let merged = 0;
    const own = new Set<string>();
    for (const m of g.members) {
      const ms = modeOf(row, m);
      if (ms.mode === "separate") {
        items.push({ id: memberItem(g.key, m.key), name: ms.name });
      } else if (ms.mode === "keep") {
        merged++;
        m.spellings.forEach((s) => own.add(`${g.key}\u0000${nameKey(s)}`));
      }
    }
    if (merged === 0) {
      continue;
    }
    if (g.action === "create") {
      items.push({ id: groupItem(g.key), name: row.name });
    } else {
      own.forEach((s) => spellings.add(s));
    }
  }

  const counts = new Map<string, number>();
  for (const it of items) {
    counts.set(nameKey(it.name), (counts.get(nameKey(it.name)) ?? 0) + 1);
  }
  const problems = new Map<string, NameProblem>();
  for (const it of items) {
    const key = nameKey(it.name);
    if (key === "") {
      problems.set(it.id, "empty");
    } else if (Array.from(it.name.trim()).length > maxNameChars) {
      problems.set(it.id, "tooLong");
    } else if (taken.has(key)) {
      problems.set(it.id, "exists");
    } else if ((counts.get(key) ?? 0) > 1) {
      problems.set(it.id, "duplicate");
    }
  }
  return { creates: items.length, additions: spellings.size, problems };
}

export interface MainButton {
  label: string;
  disabled: boolean;
  /** Shown next to a disabled button. */
  reason: string;
  /** A second small fact next to an enabled button («и добавить названия: 3»). */
  extra: string;
}

/** The one primary button: «Создать N предметов», or what is left of it. */
export function mainButton(plan: Plan): MainButton {
  const blocked = plan.problems.size > 0 ? reviewTexts.fixNames : "";
  if (plan.creates > 0) {
    return {
      label: `Создать ${subjectsWord(plan.creates)}`,
      disabled: blocked !== "",
      reason: blocked,
      extra: plan.additions > 0 ? `и добавить названия: ${plan.additions}` : "",
    };
  }
  if (plan.additions > 0) {
    return { label: `Добавить названия: ${plan.additions}`, disabled: false, reason: "", extra: "" };
  }
  return { label: reviewTexts.createPlain, disabled: true, reason: reviewTexts.nothingSelected, extra: "" };
}

/** The choices the core expects: unchecked rows `skip`, checked rows their own action; the name only when edited. */
export function buildChoices(groups: readonly ScheduleGroup[], sel: Selection): ScheduleChoice[] {
  return groups.map((g): ScheduleChoice => {
    const row = sel.get(g.key);
    if (row === undefined || !row.checked) {
      return { group: g.key, action: "skip", name: "", members: [] };
    }
    const members: ScheduleMemberChoice[] = [];
    for (const m of g.members) {
      const ms = modeOf(row, m);
      if (ms.mode === "separate") {
        const name = ms.name.trim();
        members.push({ key: m.key, action: "separate", name: name === m.name ? "" : name });
      } else if (ms.mode === "skip") {
        members.push({ key: m.key, action: "skip", name: "" });
      }
    }
    const name = row.name.trim();
    return { group: g.key, action: g.action, name: g.action === "create" && name !== g.name ? name : "", members };
  });
}

// ---------------------------------------------------------------------------------------------------------------------
// What the token has done, and the texts of the outcomes

/** What a token created and aliased so far (a retry with the same token adds to it). */
export interface Batch {
  token: string;
  created: ScheduleCreated[];
  aliased: ScheduleAliased[];
}

export function aliasCount(aliased: readonly ScheduleAliased[]): number {
  return aliased.reduce((n, a) => n + a.aliases.length, 0);
}

/** Merges a reply of createFromSchedule into the batch of its token (union by id / per subject). */
export function mergeBatch(batches: readonly Batch[], token: string, reply: Pick<CreateFromScheduleReply, "created" | "aliased">): Batch[] {
  if (reply.created.length === 0 && reply.aliased.length === 0) {
    return [...batches];
  }
  const old = batches.find((b) => b.token === token);
  const created = new Map<string, ScheduleCreated>();
  for (const c of [...(old?.created ?? []), ...reply.created]) {
    created.set(c.id, c);
  }
  const aliased = new Map<string, string[]>();
  for (const a of [...(old?.aliased ?? []), ...reply.aliased]) {
    const list = aliased.get(a.subjectId) ?? [];
    for (const s of a.aliases) {
      if (!list.includes(s)) {
        list.push(s);
      }
    }
    aliased.set(a.subjectId, list);
  }
  const merged: Batch = {
    token,
    created: [...created.values()],
    aliased: [...aliased].map(([subjectId, aliases]) => ({ subjectId, aliases })),
  };
  return old === undefined ? [...batches, merged] : batches.map((b) => (b.token === token ? merged : b));
}

/** «Заведено 3 предмета · Добавлено названий: 2»; "" when nothing was done. «Отменить всё» is offered exactly then. */
export function doneText(batches: readonly Batch[]): string {
  const created = batches.reduce((n, b) => n + b.created.length, 0);
  const aliases = batches.reduce((n, b) => n + aliasCount(b.aliased), 0);
  const parts: string[] = [];
  if (created > 0) {
    parts.push(`Заведено ${subjectsWord(created)}`);
  }
  if (aliases > 0) {
    parts.push(`Добавлено названий: ${aliases}`);
  }
  return parts.join(" · ");
}

/** A button of an outcome; the controller maps it to a call. */
export type Step = "retryCreate" | "retryUndo" | "refresh" | "toSubjects";

export interface Notice {
  tone: "ok" | "fail";
  /** The main line; "" when only the lines below speak. */
  text: string;
  /** One plain line per item (skipped rows, kept subjects). */
  lines: string[];
  steps: Step[];
}

export type Phase = "idle" | "saving" | "undoing" | "refreshing";

/** "list" shows the rows to choose from; "result" only what happened. */
export type View = "list" | "result";

export interface ReviewState {
  phase: Phase;
  view: View;
  token: string;
  groups: readonly ScheduleGroup[];
  omitted: number;
  selection: Selection;
  batches: readonly Batch[];
  notice: Notice | null;
}

function listNotice(reply: ScheduleSubjectsReply): Notice | null {
  if (reply.problem !== "") {
    return { tone: "fail", text: reviewTexts.listUnavailable, lines: [], steps: ["refresh"] };
  }
  if (reply.groups.length === 0) {
    return {
      tone: "ok",
      text: reviewTexts.allDone,
      lines: reply.omitted > 0 ? [omittedNote(reply.omitted)] : [],
      steps: reply.omitted > 0 ? ["refresh"] : [],
    };
  }
  return null;
}

function listed(reply: ScheduleSubjectsReply): Pick<ReviewState, "view" | "token" | "groups" | "omitted" | "selection" | "notice"> {
  const groups = reply.problem === "" ? reply.groups : [];
  return {
    view: groups.length > 0 ? "list" : "result",
    token: reply.token,
    groups,
    omitted: reply.omitted,
    selection: initialSelection(groups),
    notice: listNotice(reply),
  };
}

export function initialState(reply: ScheduleSubjectsReply): ReviewState {
  return { phase: "idle", batches: [], ...listed(reply) };
}

/** The merged answer of the undo of every batch; `problem` is the one that stopped the loop. */
export interface UndoResult {
  reply: UndoFromScheduleReply;
  /** The batches not undone yet. */
  remaining: Batch[];
  /** The reply could not be read: the core may have done part of it. */
  thrown: boolean;
  /** The names of the subjects by id, for the spellings that were kept. */
  names: ReadonlyMap<string, string>;
}

export type Action =
  | { type: "check"; group: string; checked: boolean }
  | { type: "rename"; group: string; name: string }
  | { type: "member"; group: string; member: string; mode: "separate" | "skip" }
  | { type: "renameMember"; group: string; member: string; name: string }
  | { type: "saving" }
  | { type: "created"; reply: CreateFromScheduleReply }
  | { type: "createThrown" }
  | { type: "undoing" }
  | { type: "undone"; result: UndoResult }
  | { type: "refreshing" }
  | { type: "refreshed"; reply: ScheduleSubjectsReply }
  | { type: "refreshThrown" };

/** The name a skipped row is called by: the edited name of its group or member, else the proposed one. */
function skippedName(state: ReviewState, key: string): string {
  for (const g of state.groups) {
    const row = state.selection.get(g.key);
    if (g.key === key && row !== undefined) {
      return row.name.trim() || g.name;
    }
    const m = g.members.find((x) => x.key === key);
    if (m !== undefined) {
      const ms = row?.members.get(key);
      return ms?.mode === "separate" ? ms.name.trim() || m.name : m.name;
    }
  }
  return "";
}

export function skippedLines(state: ReviewState, skipped: readonly ScheduleSkipped[], names: ReadonlyMap<string, string> = new Map()): string[] {
  return skipped.map((s) => {
    switch (s.reason) {
      case "name_taken": {
        const name = skippedName(state, s.key);
        if (name !== "") {
          return `„${name}“ не заведён: такое название уже занято`;
        }
        const subject = names.get(s.subjectId);
        return subject === undefined
          ? "Одно название не добавлено: оно уже занято"
          : `Названия для „${subject}“ не добавлены: одно из них уже занято`;
      }
      case "too_many_aliases":
        return reviewTexts.tooManyAliases;
      case "subject_gone":
        return reviewTexts.subjectGone;
    }
  });
}

function createNotice(state: ReviewState, reply: CreateFromScheduleReply): { view: View; notice: Notice | null } {
  const fail = (text: string, steps: Step[], view: View): { view: View; notice: Notice } => ({
    view,
    notice: { tone: "fail", text, lines: [], steps },
  });
  switch (reply.problem) {
    case "":
      return {
        view: "result",
        notice:
          reply.skipped.length === 0
            ? null
            : {
                tone: "ok",
                text: reply.created.length === 0 && reply.aliased.length === 0 ? "Ничего не заведено" : "",
                lines: skippedLines(state, reply.skipped),
                steps: ["refresh"],
              },
      };
    case "stale":
      return fail(reviewTexts.stale, ["refresh"], "result");
    case "invalid":
      return fail(reviewTexts.createInvalid, ["refresh"], "result");
    case "name_taken":
      return fail(reviewTexts.nameTaken, [], "list");
    case "busy":
      return fail(reviewTexts.busy, ["retryCreate"], "list");
    case "unavailable":
      return fail(reviewTexts.unavailable, ["retryCreate"], "list");
  }
}

function keptLine(k: ScheduleKept): string {
  return k.reason === "has_notes" ? `В „${k.name}“ уже есть конспект — оставлен` : `В „${k.name}“ уже есть файл — оставлен`;
}

function undoNotice(r: UndoResult): Notice {
  const reply = r.reply;
  const unaliased = aliasCount(reply.unaliased);
  const parts: string[] = [];
  if (reply.removed.length > 0) {
    parts.push(`убрано ${subjectsWord(reply.removed.length)}`);
  }
  if (unaliased > 0) {
    parts.push(`убрано названий: ${unaliased}`);
  }
  const lines = [
    ...reply.kept.map(keptLine),
    ...reply.keptAliases.map(
      (k) => `Названия у ${r.names.has(k.subjectId) ? `„${r.names.get(k.subjectId)}“` : reviewTexts.fallbackKeptAliasesSubject} оставлены: расписание изменилось`,
    ),
  ];
  return {
    tone: "ok",
    text: parts.length > 0 ? `Отменено: ${parts.join(" · ")}` : reviewTexts.nothingRemoved,
    lines,
    steps: ["refresh"],
  };
}

function undoProblemNotice(r: UndoResult): Notice {
  if (r.thrown) {
    return { tone: "fail", text: reviewTexts.thrown, lines: [], steps: ["retryUndo"] };
  }
  switch (r.reply.problem) {
    case "invalid":
      return { tone: "fail", text: reviewTexts.undoInvalid, lines: [], steps: ["toSubjects"] };
    case "busy":
      return { tone: "fail", text: reviewTexts.undoBusy, lines: [], steps: ["retryUndo"] };
    default:
      return { tone: "fail", text: reviewTexts.unavailable, lines: [], steps: ["retryUndo"] };
  }
}

/** The state machine. Edits are accepted only while idle in the list; a call is started only from idle. */
export function reduce(state: ReviewState, action: Action): ReviewState {
  const editable = state.phase === "idle" && state.view === "list";
  switch (action.type) {
    case "check":
      return editable ? { ...state, selection: setChecked(state.selection, action.group, action.checked) } : state;
    case "rename":
      return editable ? { ...state, selection: setName(state.selection, action.group, action.name) } : state;
    case "member":
      return editable ? { ...state, selection: toggleMember(state.selection, action.group, action.member, action.mode) } : state;
    case "renameMember":
      return editable ? { ...state, selection: setMemberName(state.selection, action.group, action.member, action.name) } : state;
    case "saving":
      return editable ? { ...state, phase: "saving", notice: null } : state;
    case "created": {
      const batches = mergeBatch(state.batches, state.token, action.reply);
      return { ...state, phase: "idle", batches, ...createNotice(state, action.reply) };
    }
    case "createThrown":
      return { ...state, phase: "idle", view: "list", notice: { tone: "fail", text: reviewTexts.thrown, lines: [], steps: ["retryCreate"] } };
    case "undoing":
      return state.phase === "idle" && state.batches.length > 0 ? { ...state, phase: "undoing", notice: null } : state;
    case "undone": {
      const r = action.result;
      if (r.thrown || r.reply.problem !== "") {
        const gone = r.reply.problem === "invalid" && !r.thrown;
        return { ...state, phase: "idle", batches: gone ? [] : r.remaining, notice: undoProblemNotice(r) };
      }
      return { ...state, phase: "idle", view: "result", batches: r.remaining, notice: undoNotice(r) };
    }
    case "refreshing":
      return state.phase === "idle" ? { ...state, phase: "refreshing" } : state;
    case "refreshed":
      return { ...state, phase: "idle", ...listed(action.reply) };
    case "refreshThrown":
      return {
        ...state,
        phase: "idle",
        view: "result",
        notice: { tone: "fail", text: reviewTexts.listUnavailable, lines: [], steps: ["refresh"] },
      };
  }
}

// ---------------------------------------------------------------------------------------------------------------------
// The calls (the controller runs them; they never throw)

export interface ReviewApi {
  create(token: string, choices: ScheduleChoice[]): Promise<CreateFromScheduleReply>;
  undo(token: string): Promise<UndoFromScheduleReply>;
  refresh(): Promise<ScheduleSubjectsReply>;
}

/** Applies the selection to the proposal of the state's token. A rejected call (unreadable reply) is an action too. */
export async function createFlow(api: Pick<ReviewApi, "create">, state: ReviewState): Promise<Action> {
  try {
    return { type: "created", reply: await api.create(state.token, buildChoices(state.groups, state.selection)) };
  } catch {
    return { type: "createThrown" };
  }
}

/** Takes back every batch, one token after another; stops at the first problem and keeps the batches not undone. */
export async function undoFlow(api: Pick<ReviewApi, "undo">, batches: readonly Batch[], names: ReadonlyMap<string, string>): Promise<Action> {
  const total: UndoFromScheduleReply = { removed: [], kept: [], unaliased: [], keptAliases: [], problem: "" };
  let remaining = [...batches];
  for (const b of batches) {
    let reply: UndoFromScheduleReply;
    try {
      reply = await api.undo(b.token);
    } catch {
      return { type: "undone", result: { reply: { ...total, problem: "unavailable" }, remaining, thrown: true, names } };
    }
    if (reply.problem !== "") {
      return { type: "undone", result: { reply: { ...total, problem: reply.problem }, remaining, thrown: false, names } };
    }
    total.removed.push(...reply.removed);
    total.kept.push(...reply.kept);
    total.unaliased.push(...reply.unaliased);
    total.keptAliases.push(...reply.keptAliases);
    remaining = remaining.slice(1);
  }
  return { type: "undone", result: { reply: total, remaining, thrown: false, names } };
}

export async function refreshFlow(api: Pick<ReviewApi, "refresh">): Promise<Action> {
  try {
    return { type: "refreshed", reply: await api.refresh() };
  } catch {
    return { type: "refreshThrown" };
  }
}

// ---------------------------------------------------------------------------------------------------------------------
// The page: the button and the empty state

/** What the page knows of the proposal: not asked yet, or the answer (null: the call failed). */
export interface OfferState {
  loaded: boolean;
  reply: ScheduleSubjectsReply | null;
}

export const offerChecking: OfferState = { loaded: false, reply: null };

/** The number of groups to offer; null while it is being checked, 0 when there is nothing (or the core is unavailable). */
export function offerGroups(o: OfferState): number | null {
  if (!o.loaded) {
    return null;
  }
  return o.reply !== null && o.reply.problem === "" ? o.reply.groups.length : 0;
}

/** The ids of the names of subjects, for the exists check and for the texts. */
export function subjectNameKeys(names: ReadonlyMap<string, string>): Set<string> {
  return new Set([...names.values()].map(nameKey));
}
