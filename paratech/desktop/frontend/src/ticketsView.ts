// Display mapping for the tickets screen (TASK-134): labels, chips, money and the gating of the paid run. What the core
// reports (status, stalled, overCap, coverage) is only shown here; the paid run starts from the student's click on
// «Подтвердить» and never from a rule on this page. Ticket titles are the student's text: the page shows them as plain
// text, so nothing here formats or links them.

import type { Coverage, TicketItem, TicketReport, TicketSet } from "./bindings/tickets.js";
import { plural } from "./plural.js";
import type { IconName } from "./ui/Icon.js";
import type { ChipKind } from "./ui/StatusChip.js";

export interface ChipView {
  kind: ChipKind;
  icon: IconName;
  label: string;
  hint: string;
}

/** How often a running set is asked for. */
export const ticketsPollMs = 3000;

/** The ticket count above which a run is over the cap and needs the explicit cost consent (docs ADR-0025). */
export const capTickets = 33;

/** The place of the finished file; there is no binding that opens a .docx from this screen. */
export const filedFolder = "Билеты";

export const texts = {
  title: "Билеты",
  label: "Экзамен",
  back: "Предметы",
  loading: "Загрузка…",
  loadFailed: "Не удалось загрузить. Попробуйте ещё раз.",
  subject: "Предмет",
  noSubjects: "Сначала нужен предмет: добавьте его в «Предметах».",
  noSets: "Для этого предмета билетов пока нет.",
  pasteTitle: "Вставить список билетов",
  pasteLabel: "Список билетов (нумерованный: «1.», «1)» или «Билет 1»)",
  pasteHint: "Поиск ответов начнётся только после подтверждения.",
  pasteAction: "Найти билеты",
  fileTitle: "Список из файла предмета",
  fileLabel: "Файл со списком",
  fileAction: "Прочитать файл",
  noFiles: "В предмете нет файлов: файл со списком билетов можно добавить в «Предметах».",
  confirm: "Подтвердить и запустить",
  confirmAgain: "Запустить снова",
  cancel: "Отменить",
  cancelBusy: "Отменяется…",
  undo: "Вернуть файл во «Входящие»",
  show: "Показать билеты",
  hide: "Скрыть билеты",
  stalledTitle: "Застряло",
  stalledHint: "Подбор ответов остановился и сам не продолжится. «Запустить снова» продолжит с того места, где он встал.",
  notCoveredHint: "Не найдено в лекциях.",
  generatedNote: "Ответы сгенерированы по лекциям: перед сдачей их нужно прочитать и проверить.",
  openNote:
    "Кнопки «Открыть .docx» здесь пока нет: файл лежит в папке «Билеты» этого предмета, открыть его можно из «Предметов».",
  costConsent: "Стоимость больше обычной, согласие на неё обязательно.",
} as const;

/** Money for people: micro-USD to dollars with cents, "$2,70"; a positive amount is never shown as zero. */
export function usd(microUsd: number): string {
  if (!Number.isFinite(microUsd) || microUsd < 0) {
    return "—";
  }
  const cents = microUsd > 0 ? Math.max(1, Math.round(microUsd / 10000)) : 0;
  const whole = Math.trunc(cents / 100);
  const rest = String(cents % 100).padStart(2, "0");
  return `$${whole},${rest}`;
}

/** "5 билетов". */
export function ticketCount(n: number): string {
  return `${n} ${plural(n, "билет", "билета", "билетов")}`;
}

/** The estimate line of the proposal and of the confirm card. */
export function budgetLine(s: TicketSet): string {
  const b = s.budget;
  return `Примерная стоимость подбора ответов: ${usd(b.estimateMicroUsd)} за ${ticketCount(b.tickets)}; предел обычного прогона — ${usd(b.capMicroUsd)}.`;
}

/** The consent sentence next to the checkbox of a run over the cap. */
export function costConsentLabel(s: TicketSet): string {
  return `Оплата около ${usd(s.budget.estimateMicroUsd)} подтверждена: это больше обычного предела ${usd(s.budget.capMicroUsd)}`;
}

/** The run is over the cap: the core says so; the ticket count is a second guard. */
export function needsCostConsent(s: TicketSet): boolean {
  return s.budget.overCap || s.budget.tickets > capTickets || s.total > capTickets;
}

/** The set is being answered (or waits to be): the page keeps asking the core. */
export function isRunning(s: TicketSet): boolean {
  return s.status === "confirmed" || s.status === "answering";
}

/** Progress in percent, 0..100; an empty set is 0. */
export function percent(s: TicketSet): number {
  return s.total <= 0 ? 0 : Math.min(100, Math.round((s.done / s.total) * 100));
}

/** "Готово 3 из 12". */
export function progressText(s: TicketSet): string {
  return `Готово ${s.done} из ${s.total}`;
}

/** The chip of a set: the status, and for a running one the state of its job (a wait, stalled). */
export function statusView(s: TicketSet): ChipView {
  switch (s.status) {
    case "proposed":
      return { kind: "neutral", icon: "doc", label: "Ждёт подтверждения", hint: "Ничего не оплачено и не запущено." };
    case "done":
      return { kind: "ok", icon: "ok", label: "Ответы готовы", hint: "" };
    case "failed":
      return { kind: "fail", icon: "warn", label: "Не удалось", hint: "" };
    case "cancelled":
      return { kind: "neutral", icon: "off", label: "Отменено", hint: "Готовые ответы остались." };
    case "confirmed":
    case "answering":
      return runningView(s);
  }
}

function runningView(s: TicketSet): ChipView {
  if (s.stalled) {
    return { kind: "fail", icon: "warn", label: texts.stalledTitle, hint: texts.stalledHint };
  }
  const js = s.jobStatus;
  if (js === "WAITING_NETWORK") {
    return { kind: "net", icon: "off", label: "Ждёт интернет", hint: "Подбор продолжится, когда появится связь." };
  }
  if (js === "WAITING_QUOTA") {
    return { kind: "wait", icon: "clock", label: "Лимит обработки исчерпан", hint: "Подбор продолжится, когда лимит восстановится." };
  }
  if (js === "RETRY_WAIT" || js.startsWith("WAITING_")) {
    return { kind: "wait", icon: "clock", label: "Ждёт повтора", hint: "Подбор продолжится сам." };
  }
  if (js === "QUEUED" || js === "") {
    return { kind: "wait", icon: "clock", label: "В очереди", hint: "" };
  }
  return { kind: "run", icon: "wave", label: "Подбираются ответы", hint: "" };
}

/** What the set offers as moves. Whether a move is allowed is the core's call; these only hide what cannot apply. */
export interface Moves {
  /** «Подтвердить и запустить» (proposed) or «Запустить снова» (stalled). */
  confirm: "none" | "start" | "restart";
  cancel: boolean;
  undo: boolean;
}

export function moves(s: TicketSet): Moves {
  const confirm = s.status === "proposed" ? "start" : isRunning(s) && s.stalled ? "restart" : "none";
  return {
    confirm,
    cancel: s.status === "proposed" || isRunning(s),
    undo: s.status === "done" && s.materialId !== "",
  };
}

/** The arguments of Confirm: acceptCost is true only when the student ticked the consent of a run over the cap. */
export interface ConfirmGate {
  allowed: boolean;
  acceptCost: boolean;
}

export function confirmGate(s: TicketSet, consentTicked: boolean): ConfirmGate {
  if (needsCostConsent(s)) {
    return { allowed: consentTicked, acceptCost: consentTicked };
  }
  return { allowed: true, acceptCost: false };
}

/** A ticket as the coverage report shows it. */
export interface TicketRow {
  number: number;
  title: string;
  chip: ChipView;
  /** The ticket has a stored answer to open. */
  hasAnswer: boolean;
}

const coverageLooks: Readonly<Record<Exclude<Coverage, "">, ChipView>> = {
  covered: { kind: "ok", icon: "ok", label: "Покрыто", hint: "" },
  partial: { kind: "wait", icon: "warn", label: "Покрыто частично", hint: "" },
  not_covered: { kind: "fail", icon: "warn", label: "Не покрыто", hint: texts.notCoveredHint },
  failed: { kind: "fail", icon: "warn", label: "Ответ не получен", hint: "" },
};

const waitingTicket: ChipView = { kind: "neutral", icon: "clock", label: "Ждёт ответа", hint: "" };

/**
 * The coverage of one ticket. A ticket that should have an answer but has none (a recording deleted, an answer
 * purged) is «Не покрыто», never an empty or green row.
 */
export function ticketRow(t: TicketItem, s: TicketSet): TicketRow {
  const hasAnswer = t.answered && t.answerId !== "";
  let chip: ChipView;
  if (t.coverage === "covered" || t.coverage === "partial") {
    chip = hasAnswer ? coverageLooks[t.coverage] : coverageLooks.not_covered;
  } else if (t.coverage === "not_covered" || t.coverage === "failed") {
    chip = coverageLooks[t.coverage];
  } else if (s.status === "proposed" || isRunning(s)) {
    chip = waitingTicket;
  } else {
    chip = coverageLooks.not_covered;
  }
  return { number: t.number, title: t.title, chip, hasAnswer };
}

export function ticketRows(s: TicketSet): TicketRow[] {
  return s.tickets.map((t) => ticketRow(t, s));
}

/** The counts of the coverage report as separate parts of a line; zero parts are left out. */
export function reportParts(r: TicketReport): string[] {
  const parts: [number, string][] = [
    [r.covered, "покрыто"],
    [r.partial, "частично"],
    [r.notCovered, "не покрыто"],
    [r.failed, "без ответа"],
    [r.pending, "ждёт"],
  ];
  return parts.filter(([n]) => n > 0).map(([n, label]) => `${n} ${label}`);
}

/** The report is shown once at least one ticket is answered or the run ended. */
export function showReport(s: TicketSet): boolean {
  return s.status === "done" || s.status === "failed" || s.status === "cancelled" || s.done > 0;
}

/** «Файл в папке „Билеты“ предмета …»: where the finished file is. */
export function fileNote(subjectName: string): string {
  return subjectName === "" ? `Файл лежит в папке «${filedFolder}» предмета.` : `Файл лежит в папке «${filedFolder}» предмета «${subjectName}».`;
}

/** The first ticket as a short caption of a set; plain text, cut for the list. */
export function setCaption(s: TicketSet, max = 80): string {
  const first = s.tickets[0]?.title ?? "";
  const chars = Array.from(first);
  return chars.length > max ? `${chars.slice(0, max).join("")}…` : first;
}
