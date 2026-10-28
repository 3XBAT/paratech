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
  type ScheduleKind,
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
  addSubject: "Добавить предмет",
  checking: "Проверяется расписание…",
  checkFailed: "Не удалось проверить расписание",
  emptyWithGroups: (n: number) => `В расписании есть предметы без карточки: ${n}. Их можно создать за один шаг`,
  emptyPlain: "Предметы создаются из расписания или вручную",
  offerLine: (n: number) => `Из расписания можно создать предметы: ${n}`,
  nextHint: (n: number) => `В расписании предметов без карточки: ${n}`,
  doubtful: "Похоже, это один предмет. Если нет — выберите «Отдельно»",
  together: "Вместе",
  separate: "Отдельно",
  skip: "Не заводить",
  memberChoice: "Что сделать с названием",
  skipped: "Не будет заведено",
  checkboxAria: (name: string) => `Завести предмет «${name}»`,
  checkboxAddAria: (subject: string) => `Добавить названия к «${subject}»`,
  addTo: "Добавить к",
  nameLabel: "Название предмета",
  separateLabel: "Название отдельного предмета",
  spellings: "Другие названия",
  close: "Закрыть",
  creating: "Создание…",
  undoAll: "Отменить всё",
  undoing: "Отменяется…",
  retry: "Повторить",
  refresh: "Обновить список",
  showRest: "Показать остальные",
  refreshing: "Обновляется…",
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
  busy: "Ещё выполняется предыдущее действие — повторите через пару секунд",
  unavailable: "Не удалось сохранить. Выбор сохранён — можно повторить",
  undoUnavailable: "Не удалось отменить. Можно повторить",
  undoInvalid: "Отменить уже нельзя: приложение перезапускалось. Заведённые предметы остались в списке",
  undoPartInvalid: "Часть изменений отменить уже нельзя: приложение перезапускалось",
  nothingRemoved: "Ничего не убрано",
  tooManyAliases: "У предмета уже много других названий — новые не добавлены",
  subjectGone: "Предмет был удалён — строка пропущена",
  fallbackKeptAliasesSubject: "предмета",
  kindUnknown: "без типа",
} as const;

/** The core's prefix of the ids it derives for subjects created from the schedule (DeriveSubjectID). */
const derivedIdPrefix = "sched-";

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

const kindOrder: Readonly<Record<ScheduleKind, number>> = { lecture: 0, seminar: 1, lab: 2, unknown: 3 };

function kindWord(k: ScheduleKind): string {
  return k === "unknown" ? reviewTexts.kindUnknown : scheduleKindWords[k];
}

/** «лекции 12 · семинары 6 · без типа 2»; one kind of unknown lessons says nothing the count of lessons does not. */
export function kindsText(kinds: readonly ScheduleKindCount[]): string {
  const shown = kinds.filter((k) => k.lessons > 0).sort((a, b) => kindOrder[a.kind] - kindOrder[b.kind]);
  if (shown.length === 0 || (shown.length === 1 && shown[0]?.kind === "unknown")) {
    return "";
  }
  return shown.map((k) => `${kindWord(k.kind)} ${k.lessons}`).join(" · ");
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

/** Under the rows: what the omitted names of the schedule wait for. */
export function omittedNote(n: number): string {
  const names = plural(n, "название", "названия", "названий");
  const verb = plural(n, "появится", "появятся", "появятся");
  return `Ещё ${n} ${names} из расписания ${verb} в списке после создания этих предметов`;
}

/** When there is no list to create from: the omitted names are only counted. */
export function omittedLeft(n: number): string {
  const names = plural(n, "название", "названия", "названий");
  const verb = plural(n, "не показано", "не показаны", "не показаны");
  return `Ещё ${n} ${names} из расписания ${verb}`;
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

/** The choice of a doubtful member: together with its group, apart as its own subject, or not created. */
export function setMemberMode(sel: Selection, group: string, member: string, mode: MemberMode): Selection {
  return withMember(sel, group, member, (m) => ({ ...m, mode }));
}

/**
 * The selection of a rebuilt proposal: what the student edited stays for the groups and members that still exist (a
 * name nobody edited follows the new proposal); new groups get the defaults.
 */
export function carrySelection(oldGroups: readonly ScheduleGroup[], old: Selection, groups: readonly ScheduleGroup[]): Selection {
  const fresh = initialSelection(groups);
  const out = new Map<string, RowSel>();
  for (const g of groups) {
    const base = fresh.get(g.key);
    const prev = old.get(g.key);
    const prevGroup = oldGroups.find((x) => x.key === g.key);
    if (base === undefined || prev === undefined) {
      if (base !== undefined) {
        out.set(g.key, base);
      }
      continue;
    }
    const members = new Map(base.members);
    for (const m of g.members) {
      const pm = prev.members.get(m.key);
      const pmGroup = prevGroup?.members.find((x) => x.key === m.key);
      if (pm !== undefined) {
        members.set(m.key, { mode: pm.mode, name: pmGroup !== undefined && pm.name === pmGroup.name ? m.name : pm.name });
      }
    }
    out.set(g.key, {
      checked: prev.checked,
      name: prevGroup !== undefined && prev.name === prevGroup.name ? g.name : prev.name,
      members,
    });
  }
  return out;
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

interface PlanItem {
  id: string;
  name: string;
}

/** A spelling that goes along with an item (as its alias) or to an existing subject; `owner` is the item's id. */
interface Carried {
  owner: string;
  key: string;
}

function planItems(groups: readonly ScheduleGroup[], sel: Selection): { items: PlanItem[]; additions: Set<string>; carried: Carried[] } {
  const items: PlanItem[] = [];
  const additions = new Set<string>();
  const carried: Carried[] = [];
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
        const id = memberItem(g.key, m.key);
        items.push({ id, name: ms.name });
        m.spellings.forEach((s) => carried.push({ owner: id, key: nameKey(s) }));
      } else if (ms.mode === "keep") {
        merged++;
        m.spellings.forEach((s) => own.add(nameKey(s)));
      }
    }
    if (merged === 0) {
      continue;
    }
    own.forEach((s) => carried.push({ owner: groupItem(g.key), key: s }));
    if (g.action === "create") {
      items.push({ id: groupItem(g.key), name: row.name });
    } else {
      own.forEach((s) => additions.add(`${g.key}\u0000${s}`));
    }
  }
  return { items, additions, carried };
}

/** The names the selection would create, as nameKeys. */
export function plannedKeys(groups: readonly ScheduleGroup[], sel: Selection): string[] {
  return planItems(groups, sel).items.map((it) => nameKey(it.name));
}

/**
 * What the selection makes of the proposal. `taken` holds the nameKey of the names and the other names of existing
 * subjects (see takenOf); a new name may not equal one, nor another new name, nor a spelling that goes to another row.
 */
export function planOf(groups: readonly ScheduleGroup[], sel: Selection, taken: ReadonlySet<string>): Plan {
  const { items, additions, carried } = planItems(groups, sel);
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
    } else if ((counts.get(key) ?? 0) > 1 || carried.some((c) => c.key === key && c.owner !== it.id)) {
      problems.set(it.id, "duplicate");
    }
  }
  return { creates: items.length, additions: additions.size, problems };
}

export interface MainButton {
  label: string;
  disabled: boolean;
  /** Shown next to a disabled button. */
  reason: string;
  /** A second small fact next to an enabled button («и добавить другие названия: 3»). */
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
      extra: plan.additions > 0 ? `и добавить другие названия: ${plan.additions}` : "",
    };
  }
  if (plan.additions > 0) {
    return { label: `Добавить другие названия: ${plan.additions}`, disabled: false, reason: "", extra: "" };
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

/** The part of a subject the review needs: its id, its name and its other names (the core refuses a new name equal to any). */
export interface SubjectRef {
  id: string;
  name: string;
  aliases: readonly string[];
}

/** The names of subjects by id, for the texts of the outcomes. */
export function subjectNamesOf(subjects: readonly SubjectRef[]): Map<string, string> {
  return new Map(subjects.map((s) => [s.id, s.name]));
}

/**
 * The nameKeys a new subject may not take: the names and the other names of the existing subjects. The subjects this very
 * token made are left out, or a retry after a partial create would find its own work «already existing» (the ones the
 * reply listed by id; after an unreadable reply the core's derived ids, which start with a fixed prefix, whose name is
 * among the ones just sent). The core stays the judge: a real clash comes back as `name_taken`.
 */
export function takenOf(subjects: readonly SubjectRef[], state: Pick<ReviewState, "token" | "batches" | "attempt">): Set<string> {
  const own = new Set((state.batches.find((b) => b.token === state.token)?.created ?? []).map((c) => c.id));
  const sent = new Set(state.attempt);
  const out = new Set<string>();
  for (const s of subjects) {
    if (own.has(s.id) || (s.id.startsWith(derivedIdPrefix) && sent.has(nameKey(s.name)))) {
      continue;
    }
    out.add(nameKey(s.name));
    s.aliases.forEach((a) => out.add(nameKey(a)));
  }
  return out;
}

/** What a press on the main button does now: the plan, and whether the call may start (idle, in the list, no name problem). */
export function createGate(state: ReviewState, subjects: readonly SubjectRef[]): { plan: Plan; ok: boolean } {
  const plan = planOf(state.groups, state.selection, takenOf(subjects, state));
  return { plan, ok: state.phase === "idle" && state.view === "list" && plan.problems.size === 0 && plan.creates + plan.additions > 0 };
}

/** Esc closes the panel unless a call runs or the focus is in a text field (a stray Esc must not discard the edits). */
export function escCloses(target: { tagName: string; type?: string }, busy: boolean): boolean {
  const typing = target.tagName === "INPUT" && target.type !== "checkbox";
  return !busy && !typing;
}

/** «Создано предметов: 3 · Добавлено названий: 2»; "" when nothing was done. «Отменить всё» is offered exactly then. */
export function doneText(batches: readonly Batch[]): string {
  const created = batches.reduce((n, b) => n + b.created.length, 0);
  const aliases = batches.reduce((n, b) => n + aliasCount(b.aliased), 0);
  const parts: string[] = [];
  if (created > 0) {
    parts.push(`Создано предметов: ${created}`);
  }
  if (aliases > 0) {
    parts.push(`Добавлено названий: ${aliases}`);
  }
  return parts.join(" · ");
}

/** A button of an outcome; the controller maps it to a call. `rest` asks the proposal again for the omitted names;
 * `close` only closes the page's outcome line (the panel has its own «Закрыть»). */
export type Step = "retryCreate" | "retryUndo" | "refresh" | "rest" | "close";

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
  /** The nameKeys of the names sent by the last create that did not finish cleanly (see takenOf). */
  attempt: readonly string[];
}

function listNotice(reply: ScheduleSubjectsReply): Notice | null {
  if (reply.problem !== "") {
    return { tone: "fail", text: reviewTexts.listUnavailable, lines: [], steps: ["refresh"] };
  }
  if (reply.groups.length === 0) {
    return {
      tone: "ok",
      text: reviewTexts.allDone,
      lines: reply.omitted > 0 ? [omittedLeft(reply.omitted)] : [],
      steps: reply.omitted > 0 ? ["rest"] : [],
    };
  }
  return null;
}

function listed(reply: ScheduleSubjectsReply): Pick<ReviewState, "view" | "token" | "groups" | "omitted" | "selection" | "notice" | "attempt"> {
  const groups = reply.problem === "" ? reply.groups : [];
  return {
    view: groups.length > 0 ? "list" : "result",
    token: reply.token,
    groups,
    omitted: reply.omitted,
    selection: initialSelection(groups),
    notice: listNotice(reply),
    attempt: [],
  };
}

/** `batches`: what earlier panels of the page created and has not undone yet (the undo goes newest first). */
export function initialState(reply: ScheduleSubjectsReply, batches: readonly Batch[] = []): ReviewState {
  return { phase: "idle", batches, ...listed(reply) };
}

/** The page's own state after the panel closed with something to undo: no list, only what was done. */
export function closedState(batches: readonly Batch[], notice: Notice | null = null): ReviewState {
  return { phase: "idle", view: "result", token: "", groups: [], omitted: 0, selection: new Map(), batches, notice, attempt: [] };
}

/** The merged answer of the undo of every batch; `reply.problem` is the one that stopped the loop. */
export interface UndoResult {
  reply: UndoFromScheduleReply;
  /** The batches not undone yet (the one that stopped the loop and the older ones). */
  remaining: Batch[];
  /** The reply could not be read: the core may have done part of it. */
  thrown: boolean;
  /** The names of the subjects by id, for the spellings that were kept. */
  names: ReadonlyMap<string, string>;
  /** How many batches the core no longer knew (`invalid`), of `tried`. */
  invalid: number;
  tried: number;
}

export type Action =
  | { type: "check"; group: string; checked: boolean }
  | { type: "rename"; group: string; name: string }
  | { type: "member"; group: string; member: string; mode: MemberMode }
  | { type: "renameMember"; group: string; member: string; name: string }
  | { type: "saving" }
  | { type: "created"; reply: CreateFromScheduleReply }
  | { type: "createThrown" }
  | { type: "undoing" }
  | { type: "undone"; result: UndoResult }
  | { type: "refreshing" }
  | { type: "refreshed"; reply: ScheduleSubjectsReply }
  | { type: "refreshThrown" }
  /** The page hands over (or takes back) the batches when the panel closes (or opens). */
  | { type: "adopt"; batches: readonly Batch[]; notice?: Notice | null }
  /** The student closes the outcome line of the page. */
  | { type: "dismiss" };

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
          return `«${name}» не заведён: такое название уже занято`;
        }
        const subject = names.get(s.subjectId);
        return subject === undefined
          ? "Одно название не добавлено: оно уже занято"
          : `Названия для «${subject}» не добавлены: одно из них уже занято`;
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
    case "": {
      // A clean success still tells about the names the proposal left out, with the button that shows them.
      const rest = state.omitted > 0;
      const lines = [...skippedLines(state, reply.skipped), ...(rest ? [omittedNote(state.omitted)] : [])];
      const nothing = reply.created.length === 0 && reply.aliased.length === 0;
      return {
        view: "result",
        notice: lines.length === 0 ? null : { tone: "ok", text: reply.skipped.length > 0 && nothing ? "Ничего не заведено" : "", lines, steps: [rest ? "rest" : "refresh"] },
      };
    }
    case "stale":
      return fail(reviewTexts.stale, ["refresh"], "result");
    case "invalid":
      return fail(reviewTexts.createInvalid, ["refresh"], "result");
    case "name_taken":
      // No row is named by the core here: the text says what to do, «Повторить» sends the (edited) list again.
      return fail(reviewTexts.nameTaken, ["retryCreate"], "list");
    case "busy":
      return fail(reviewTexts.busy, ["retryCreate"], "list");
    case "unavailable":
      return fail(reviewTexts.unavailable, ["retryCreate"], "list");
  }
}

function keptLine(k: ScheduleKept): string {
  return k.reason === "has_notes" ? `В «${k.name}» уже есть конспект — оставлен` : `В «${k.name}» уже есть файл — оставлен`;
}

/** «Отменено: предметов 2 · названий 1»; "" when nothing was taken back. */
function undoSummary(reply: UndoFromScheduleReply): string {
  const unaliased = aliasCount(reply.unaliased);
  const parts: string[] = [];
  if (reply.removed.length > 0) {
    parts.push(`предметов ${reply.removed.length}`);
  }
  if (unaliased > 0) {
    parts.push(`названий ${unaliased}`);
  }
  return parts.length > 0 ? `Отменено: ${parts.join(" · ")}` : "";
}

/** What stayed: one plain line per subject or name kept, and a word about batches the core no longer knew. */
function undoLines(r: UndoResult): string[] {
  const reply = r.reply;
  return [
    ...reply.kept.map(keptLine),
    ...reply.keptAliases.map(
      (k) => `Другие названия у ${r.names.has(k.subjectId) ? `«${r.names.get(k.subjectId)}»` : reviewTexts.fallbackKeptAliasesSubject} оставлены: расписание изменилось`,
    ),
    ...(r.invalid > 0 && r.invalid < r.tried ? [reviewTexts.undoPartInvalid] : []),
  ];
}

/** The outcome of the undo of every batch: what was undone, what was kept and what could not be, and the way out. */
function undoNotice(r: UndoResult): Notice {
  const reply = r.reply;
  const summary = undoSummary(reply);
  const lines = undoLines(r);
  if (r.thrown || reply.problem !== "") {
    return {
      tone: "fail",
      text: !r.thrown && reply.problem === "busy" ? reviewTexts.busy : reviewTexts.undoUnavailable,
      lines: [...(summary !== "" ? [summary] : []), ...lines],
      steps: ["retryUndo"],
    };
  }
  if (r.tried > 0 && r.invalid === r.tried) {
    // Nothing to retry or refresh: the panel's own «Закрыть» is the way out (the page's line adds one, see lineSteps).
    return { tone: "fail", text: reviewTexts.undoInvalid, lines: [], steps: [] };
  }
  return { tone: "ok", text: summary !== "" ? summary : reviewTexts.nothingRemoved, lines, steps: ["refresh"] };
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
      return editable ? { ...state, selection: setMemberMode(state.selection, action.group, action.member, action.mode) } : state;
    case "renameMember":
      return editable ? { ...state, selection: setMemberName(state.selection, action.group, action.member, action.name) } : state;
    case "saving":
      return editable ? { ...state, phase: "saving", notice: null, attempt: plannedKeys(state.groups, state.selection) } : state;
    case "created": {
      const batches = mergeBatch(state.batches, state.token, action.reply);
      const attempt = action.reply.problem === "" ? [] : state.attempt;
      return { ...state, phase: "idle", batches, attempt, ...createNotice(state, action.reply) };
    }
    case "createThrown":
      return { ...state, phase: "idle", view: "list", notice: { tone: "fail", text: reviewTexts.unavailable, lines: [], steps: ["retryCreate"] } };
    case "undoing":
      return state.phase === "idle" && state.batches.length > 0 ? { ...state, phase: "undoing", notice: null } : state;
    case "undone": {
      // What was undone before a problem stays on screen with it; the batches left are the ones to try again.
      const r = action.result;
      const stopped = r.thrown || r.reply.problem !== "";
      return { ...state, phase: "idle", view: stopped ? state.view : "result", batches: r.remaining, notice: undoNotice(r) };
    }
    case "refreshing":
      return state.phase === "idle" ? { ...state, phase: "refreshing" } : state;
    case "refreshed": {
      // The student's edits stay for the groups that are still proposed.
      const next = listed(action.reply);
      return { ...state, phase: "idle", ...next, selection: carrySelection(state.groups, state.selection, next.groups) };
    }
    case "refreshThrown":
      return {
        ...state,
        phase: "idle",
        view: "result",
        notice: { tone: "fail", text: reviewTexts.listUnavailable, lines: [], steps: ["refresh"] },
      };
    case "adopt":
      return { ...state, phase: "idle", batches: action.batches, notice: action.notice ?? null };
    case "dismiss":
      return state.phase === "idle" ? { ...state, notice: null } : state;
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

/**
 * Takes back every batch, newest first (the core rebuilds each token's base state by removing only that token's
 * subjects). A batch the core no longer knows is dropped and the rest goes on; any other problem stops the loop. What
 * was undone before it, and what the stopping reply itself reports, is kept in the result.
 */
export async function undoFlow(api: Pick<ReviewApi, "undo">, batches: readonly Batch[], names: ReadonlyMap<string, string>): Promise<Action> {
  const total: UndoFromScheduleReply = { removed: [], kept: [], unaliased: [], keptAliases: [], problem: "" };
  const left = [...batches];
  let invalid = 0;
  let tried = 0;
  const finish = (problem: UndoFromScheduleReply["problem"], thrown: boolean): Action => ({
    type: "undone",
    result: { reply: { ...total, problem }, remaining: [...left], thrown, names, invalid, tried },
  });
  for (let i = batches.length - 1; i >= 0; i--) {
    const b = batches[i];
    if (b === undefined) {
      continue;
    }
    tried++;
    let reply: UndoFromScheduleReply;
    try {
      reply = await api.undo(b.token);
    } catch {
      return finish("unavailable", true);
    }
    total.removed.push(...reply.removed);
    total.kept.push(...reply.kept);
    total.unaliased.push(...reply.unaliased);
    total.keptAliases.push(...reply.keptAliases);
    if (reply.problem === "invalid") {
      invalid++;
      left.pop();
      continue;
    }
    if (reply.problem !== "") {
      return finish(reply.problem, false);
    }
    left.pop();
  }
  return finish("", false);
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

/** "failed": the check itself did not work; "none": it worked and the schedule has nothing to create. */
export type OfferStatus = "checking" | "failed" | "none" | "groups";

export function offerStatus(o: OfferState): OfferStatus {
  if (!o.loaded) {
    return "checking";
  }
  if (o.reply === null || o.reply.problem !== "") {
    return "failed";
  }
  return o.reply.groups.length > 0 ? "groups" : "none";
}

/** The number of groups to offer; null while it is being checked, 0 when there is nothing (or the check failed). */
export function offerGroups(o: OfferState): number | null {
  if (!o.loaded) {
    return null;
  }
  return o.reply !== null && o.reply.problem === "" ? o.reply.groups.length : 0;
}

/** The buttons of an outcome line of the page (no proposal there to refresh): what is left, or «Закрыть». */
export function lineSteps(n: Notice): Step[] {
  const steps = n.steps.filter((s) => s !== "refresh" && s !== "rest");
  return steps.length > 0 ? steps : ["close"];
}
