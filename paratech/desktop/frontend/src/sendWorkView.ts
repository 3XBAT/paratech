// «Отправить преподавателю» in the card of a finished work (TASK-193): the button only makes a proposal; the confirm
// card that follows (agentConfirmView) asks «Да», and only that click sends the email. Pure logic, no React, so the
// tests drive it with a fake API. Voice: impersonal.

import { type AgentConfirmApi, type ConfirmCard, confirmErrorText } from "./bindings/agentConfirm.js";
import type { TurnkeyWork } from "./bindings/turnkey.js";
import { formatTime } from "./activityView.js";

export const sendWorkTexts = {
  button: "Отправить преподавателю",
  again: "Отправить ещё раз",
  busy: "Подготовка…",
  hint: "Письмо уйдёт только после «Да» на следующей карточке.",
  alreadySent: "Эта работа уже отправлялась преподавателю. Повторная отправка создаст ещё одно письмо.",
  repeatWarning: "Повторная отправка создаст ещё одно письмо.",
} as const;

/** The button is offered for a work of an assignment (the core's `sendable`) that is ready. */
export function canOfferSend(w: TurnkeyWork): boolean {
  return w.sendable === true && w.writing?.step === "none" && w.writing.result !== "";
}

export interface SendWorkState {
  busy: boolean;
  /** The proposal's confirm card, once made. */
  card: ConfirmCard | null;
  error: string;
  /**
   * A send of this work reached «done» while the card was open, before the work was read again. The source of truth is
   * the work's `sentAt` (TASK-226); this only bridges the gap until the next read.
   */
  sentBefore: boolean;
}

export interface SentNote {
  /** The work was sent before: the button reads «Отправить ещё раз». */
  sent: boolean;
  /** The warning line; "" when the work was never sent. */
  text: string;
}

/** What is known about earlier sends: `sentAt` of the work first, the send just confirmed here second. */
export function sentNote(sentAt: string | undefined, sentHere: boolean): SentNote {
  if (sentAt) {
    return { sent: true, text: `Отправлено ${formatTime(sentAt)}. ${sendWorkTexts.repeatWarning}` };
  }
  return sentHere ? { sent: true, text: sendWorkTexts.alreadySent } : { sent: false, text: "" };
}

export const initialSendWork: SendWorkState = { busy: false, card: null, error: "", sentBefore: false };

/** A send that went out, or one the core reports as already confirmed. */
export function wasSent(c: ConfirmCard): boolean {
  return c.kind === "send" && (c.state === "done" || c.state === "confirmed");
}

/** The card is finished (nothing to press): a new send may be proposed. */
export function isFinished(c: ConfirmCard): boolean {
  return c.state === "done" || c.state === "declined" || c.state === "stale" || c.state === "rejected";
}

/** Several clicks in a row make one proposal. Nothing is sent here. */
export class SendWorkFlow {
  private current: SendWorkState = initialSendWork;
  private pending = false;
  private disposed = false;

  constructor(
    private readonly api: Pick<AgentConfirmApi, "proposeSendWork">,
    private readonly workId: string,
    private readonly onChange: (s: SendWorkState) => void,
  ) {}

  get state(): SendWorkState {
    return this.current;
  }

  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private set(patch: Partial<SendWorkState>): void {
    this.current = { ...this.current, ...patch };
    if (!this.disposed) {
      this.onChange(this.current);
    }
  }

  async propose(): Promise<void> {
    if (this.pending || (this.current.card !== null && !isFinished(this.current.card))) {
      return;
    }
    this.pending = true;
    this.set({ busy: true, card: null, error: "" });
    try {
      const card = await this.api.proposeSendWork(this.workId);
      this.set({ busy: false, card, sentBefore: this.current.sentBefore || wasSent(card) });
    } catch (err) {
      // Includes noAssignmentText: the core's fixed text is shown as it is.
      this.set({ busy: false, error: confirmErrorText(err) });
    } finally {
      this.pending = false;
    }
  }

  /** The confirm card changed (its own «Да» / «Нет» ran): a send that went out marks the work as sent. */
  noteCard(card: ConfirmCard): void {
    const same = this.current.card !== null && this.current.card.targetId === card.targetId;
    this.set({ sentBefore: this.current.sentBefore || wasSent(card), ...(same ? { card } : {}) });
  }
}
