// View logic of the confirm card (TASK-128): which lines the card shows, its wording and the flow of the buttons.
// The card says exactly what will happen, from the core's data only. «Да» is a deliberate click: nothing confirms by
// itself, and a second click while the first runs does nothing. The voice is impersonal.

import {
  type AgentConfirmApi,
  type ConfirmCard,
  confirmErrorText,
  type ConfirmTarget,
} from "./bindings/agentConfirm.js";
import { dueText, shortDate, timeText } from "./todayView.js";

export const confirmTexts = {
  caption: "Нужно подтверждение",
  yes: "Да",
  no: "Нет",
  working: "Выполняется…",
  loading: "Загрузка карточки…",
  reload: "Обновить карточку",
  deadlineTitle: "Срок в календарь",
  sendTitle: "Отправка письма",
  deleteTitle: "Удаление записи",
  deadlineNote: "Событие появится в Google Календаре только после «Да».",
  sendNote: "Письмо уйдёт только после «Да», на адрес из настроек.",
  deleteNote: "Удаление без возможности восстановления. «Да» откроет окно удаления.",
  doneDeadline: "Срок добавлен в календарь.",
  doneSend: "Письмо отправлено.",
  doneDelete: "Запись удалена.",
  declined: "Отклонено: ничего не выполнено.",
  confirmedAgain: "Подтверждено, но не выполнено. Повторное «Да» не создаст дубль.",
  noDeletable: "Удалить нельзя.",
  adHoc: "Запись",
} as const;

export interface CardRow {
  label: string;
  value: string;
}

/** What the card shows. Every value is text from a file, the settings or a model: a text node only. */
export interface ConfirmCardModel {
  title: string;
  /** What exactly happens: «кому, что, какая версия». */
  rows: CardRow[];
  /** The list of what a deletion removes. */
  items: string[];
  /** Under the lines: what «Да» does. */
  note: string;
  /** «В файле указан другой адрес — не используется»; "" when none. */
  notice: string;
  /** A final or blocking text instead of the buttons; "" while the card waits for «Да». */
  outcome: string;
  /** Which buttons: both while waiting, only «Да» when a confirmed action did not finish. */
  yes: boolean;
  no: boolean;
}

/** «12 КБ», «1,4 МБ»; "" when the size is unknown. */
export function sizeText(bytes: number): string {
  if (bytes <= 0) {
    return "";
  }
  if (bytes < 1024) {
    return `${bytes} Б`;
  }
  const kb = bytes / 1024;
  if (kb < 1024) {
    return `${Math.round(kb)} КБ`;
  }
  return `${(kb / 1024).toFixed(1).replace(".", ",")} МБ`;
}

function row(label: string, value: string): CardRow[] {
  return value === "" ? [] : [{ label, value }];
}

function attachment(c: ConfirmCard): string {
  const size = sizeText(c.attachmentSize);
  const fmt = c.attachmentFormat === "" ? "" : c.attachmentFormat.toUpperCase();
  const tail = [fmt, size].filter((x) => x !== "").join(", ");
  return tail === "" ? c.attachmentName : `${c.attachmentName} (${tail})`;
}

function deletionTitle(c: ConfirmCard): string {
  const d = c.deletion;
  if (d === null) {
    return "";
  }
  if (d.title !== "") {
    return d.title;
  }
  const when = [shortDate(d.startedAt), timeText(d.startedAt)].filter((x) => x !== "").join(" ");
  return when === "" ? confirmTexts.adHoc : `${confirmTexts.adHoc} ${when}`;
}

function outcomeOf(c: ConfirmCard): string {
  switch (c.state) {
    case "done":
      return c.kind === "deadline" ? confirmTexts.doneDeadline : c.kind === "send" ? confirmTexts.doneSend : confirmTexts.doneDelete;
    case "declined":
      return confirmTexts.declined;
    case "stale":
    case "rejected":
      return c.message !== "" ? c.message : confirmTexts.declined;
    case "confirmed":
      return c.message !== "" ? c.message : confirmTexts.confirmedAgain;
    case "waiting":
      return "";
  }
}

export function confirmCardModel(c: ConfirmCard): ConfirmCardModel {
  const waiting = c.state === "waiting";
  const open = waiting || c.state === "confirmed";
  const base = { outcome: outcomeOf(c), yes: open, no: waiting, notice: c.notice };
  switch (c.kind) {
    case "deadline":
      return {
        ...base,
        title: confirmTexts.deadlineTitle,
        rows: [
          ...row("Куда", "Google Календарь"),
          ...row("Что", c.title),
          ...row("Предмет", c.subject),
          ...row("Срок", dueText(c.due).replace(/^до /, "")),
        ],
        items: [],
        note: confirmTexts.deadlineNote,
      };
    case "send":
      return {
        ...base,
        title: confirmTexts.sendTitle,
        rows: [
          ...row("Кому", c.recipient),
          ...row("Тема", c.emailSubject),
          ...row("Задание", c.title),
          ...row("Вложение", attachment(c)),
          ...row("Версия", c.draftVersion > 0 ? `Версия ${c.draftVersion}` : ""),
        ],
        items: [],
        note: confirmTexts.sendNote,
      };
    case "delete_recording": {
      const refused = c.deletion !== null && !c.deletion.deletable;
      return {
        ...base,
        title: confirmTexts.deleteTitle,
        rows: row("Запись", deletionTitle(c)),
        items: c.deletion?.deletes ?? [],
        note: confirmTexts.deleteNote,
        outcome: refused ? c.deletion?.refusalMessage || confirmTexts.noDeletable : base.outcome,
        yes: open && !refused,
        no: waiting && !refused,
      };
    }
  }
}

// ---- Flow ----

export interface FlowState {
  card: ConfirmCard | null;
  /** A call runs: the buttons are off. */
  busy: "" | "confirm" | "decline";
  /** A fixed Russian text of a refused call; "" when none. */
  error: string;
  /** The deletion window is open (a deletion is confirmed there). */
  deleteOpen: boolean;
}

export const initialFlow: FlowState = { card: null, busy: "", error: "", deleteOpen: false };

/**
 * The buttons of one card. Keeps a guard outside the rendering: a second click arriving before the page re-rendered
 * (a double click) finds the first call still running and does nothing, so one click is one action.
 */
export class ConfirmFlow {
  private current: FlowState = initialFlow;
  private pending = false;
  private disposed = false;

  constructor(
    private readonly api: AgentConfirmApi,
    private readonly onChange: (s: FlowState) => void,
  ) {}

  get state(): FlowState {
    return this.current;
  }

  /** The page shows the card again (a strict-mode remount re-attaches the same flow). */
  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private set(patch: Partial<FlowState>): void {
    this.current = { ...this.current, ...patch };
    if (!this.disposed) {
      this.onChange(this.current);
    }
  }

  /** Shows a card the page already has (a proposal just made). */
  show(card: ConfirmCard): void {
    this.set({ card, error: "" });
  }

  /** Reads the card of a target of the agent's confirm card. */
  async load(target: ConfirmTarget): Promise<void> {
    try {
      this.set({ card: await this.api.card(target), error: "" });
    } catch (err) {
      this.set({ error: confirmErrorText(err) });
    }
  }

  /** «Да». A send or a deadline goes to the core; a deletion opens the deletion window. */
  async yes(): Promise<void> {
    const card = this.current.card;
    if (this.pending || card === null || this.current.deleteOpen) {
      return;
    }
    const model = confirmCardModel(card);
    if (!model.yes) {
      return;
    }
    if (card.kind === "delete_recording") {
      this.set({ deleteOpen: true, error: "" });
      return;
    }
    this.pending = true;
    this.set({ busy: "confirm", error: "" });
    try {
      this.set({ card: await this.api.confirm(card), busy: "" });
    } catch (err) {
      this.set({ error: confirmErrorText(err), busy: "" });
    } finally {
      this.pending = false;
    }
  }

  /** «Нет»: nothing runs; a preview only changes the card. */
  async no(): Promise<void> {
    const card = this.current.card;
    if (this.pending || card === null || card.state !== "waiting") {
      return;
    }
    this.pending = true;
    this.set({ busy: "decline", error: "" });
    try {
      this.set({ card: await this.api.decline({ kind: card.targetKind as ConfirmTarget["kind"], id: card.targetId }), busy: "" });
    } catch (err) {
      this.set({ error: confirmErrorText(err), busy: "" });
    } finally {
      this.pending = false;
    }
  }

  /** The deletion window closed without deleting. */
  closeDelete(): void {
    this.set({ deleteOpen: false });
  }

  /** The deletion window deleted the recording. */
  deleted(): void {
    const card = this.current.card;
    this.set({ deleteOpen: false, card: card === null ? null : { ...card, state: "done", message: "" } });
  }
}

// ---- «Да, в календарь» on «Сегодня» ----

export const calendarTexts = {
  button: "Да, в календарь",
  busy: "Подготовка…",
  kind: "create_deadline_event",
} as const;

export interface ProposeState {
  busy: boolean;
  card: ConfirmCard | null;
  error: string;
}

export const initialPropose: ProposeState = { busy: false, card: null, error: "" };

/** Whether a post's calendar action is the deadline event the button offers; any other kind is not shown. */
export function isDeadlineAction(kind: string, assignmentId: string): boolean {
  return kind === calendarTexts.kind && assignmentId !== "";
}

/**
 * «Да, в календарь» makes only a proposal (nothing goes to the calendar); the card it returns then asks «Да».
 * Several clicks in a row make one call.
 */
export class ProposeFlow {
  private current: ProposeState = initialPropose;
  private pending = false;
  private disposed = false;

  constructor(
    private readonly api: AgentConfirmApi,
    private readonly assignmentId: string,
    private readonly onChange: (s: ProposeState) => void,
  ) {}

  get state(): ProposeState {
    return this.current;
  }

  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private set(next: ProposeState): void {
    this.current = next;
    if (!this.disposed) {
      this.onChange(next);
    }
  }

  async propose(): Promise<void> {
    if (this.pending || this.current.card !== null) {
      return;
    }
    this.pending = true;
    this.set({ busy: true, card: null, error: "" });
    try {
      this.set({ busy: false, card: await this.api.proposeDeadline(this.assignmentId), error: "" });
    } catch (err) {
      this.set({ busy: false, card: null, error: confirmErrorText(err) });
    } finally {
      this.pending = false;
    }
  }
}
