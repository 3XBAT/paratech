// The press of an offered page button (TASK-324, ADR-0033 §3): an offer_button card, or the button an action line offers
// («Начать запись» under a stopped recording). The press does what the page's own button does: the same binding call where
// the button is one call, otherwise the navigation to the page where the button is (a native dialog, a settings page, a
// choice the student must make). Nothing here comes from model text: the action is of a closed set, the id is typed.
// The reasons are plain Russian beside the button; a failed press leaves the button usable.

import type { OfferView } from "./bindings/agent.js";
import type { CalendarApi } from "./bindings/calendar.js";
import type { LibraryApi } from "./bindings/library.js";
import type { TurnkeyApi } from "./bindings/turnkey.js";
import { calendarTexts } from "./calendarView.js";
import type { PageId } from "./nav.js";
import { offerModel, offerSpecs, offerUnavailableText, texts } from "./agentView.js";
import { openWork } from "./workView.js";

export const offerReasons = {
  /** The offer names no object the button could open (a made-up or an older core's card). */
  noTarget: "Кнопка больше не действует: объект не указан.",
  unavailable: offerUnavailableText,
  nothingToUndo: "Отменять нечего: в этом чате нет действий, которые ещё можно отменить.",
  workFailed: "Не удалось открыть работу. Попробуйте ещё раз.",
  workNotReady: "Файл работы ещё не готов.",
  failed: "Не удалось выполнить. Попробуйте ещё раз.",
} as const;

/** What the press did: `ok` false shows `text` next to the button; `once` the job is done and the button goes off. */
export interface OfferResult {
  ok: boolean;
  /** A short plain line; "" says nothing (a page opened). */
  text: string;
  once?: boolean;
}

/** What a press means, by action. The table is the contract of the panel with the pages (see offerStep). */
export type OfferStep =
  | { type: "openMaterial"; id: string }
  | { type: "openSummary"; id: string; section?: "lecture" }
  | { type: "openTranscript"; id: string }
  | { type: "openWork"; id: string }
  | { type: "calendarSync" }
  | { type: "page"; page: PageId }
  | { type: "startRecording" }
  | { type: "undoLatest" }
  | { type: "unavailable" }
  | { type: "noTarget" };

const pageOf: Readonly<Record<string, PageId>> = {
  // The block of «Подключения» where «Войти через Google» is (the sign-in needs a stored client there).
  google_sign_in: "connections",
  // «Модели речи»: the student picks the model; the page's «Скачать» asks about size and runs the download.
  download_model: "models",
  // «Диагностика»: «Проверить звук» of a recording without speech leads there too (the student has to listen).
  sound_check: "diagnostics",
  // The only help the app has is the calendar guide, entered from «Подключения».
  open_help: "connections",
};

/** The step of an offer; an offer the panel draws no button for (unknown action, missing id) is `noTarget`. */
export function offerStep(offer: OfferView): OfferStep {
  // No button is drawn for these; a press that arrives anyway says why nothing happens.
  if (Object.hasOwn(offerSpecs, offer.action) && offerSpecs[offer.action].unavailable === true) {
    return { type: "unavailable" };
  }
  if (offerModel(offer) === null) {
    return { type: "noTarget" };
  }
  const id = offer.target?.id ?? "";
  switch (offer.action) {
    case "open_material":
      return { type: "openMaterial", id };
    case "open_summary":
      return { type: "openSummary", id };
    case "open_lecture_text":
      return { type: "openSummary", id, section: "lecture" };
    case "open_transcript":
      return { type: "openTranscript", id };
    case "save_as":
      return { type: "openWork", id };
    case "google_sync":
      return { type: "calendarSync" };
    case "start_recording":
      return { type: "startRecording" };
    case "undo_action":
      return { type: "undoLatest" };
    default:
      return Object.hasOwn(pageOf, offer.action) ? { type: "page", page: pageOf[offer.action] } : { type: "noTarget" };
  }
}

/** Everything a press may touch; the app wires the real ones, the tests fakes. */
export interface OfferDeps {
  library: Pick<LibraryApi, "openMaterial">;
  turnkey: Pick<TurnkeyApi, "work">;
  calendar: Pick<CalendarApi, "sync">;
  nav: {
    openLessonSummary(recordingId: string, section?: "lecture"): void;
    openLessonTranscript(recordingId: string): void;
    openPage(page: PageId): void;
    /** «Записи» with the start panel open: the source of the sound is chosen there. */
    startRecording(): void;
  };
  /**
   * «Отменить» of the latest action of the chat that can still be undone; null when there is none. `what` is the wording
   * of the action that was undone («Отмечено выполненным: 3 задания»).
   */
  undoLatest(): Promise<{ ok: boolean; text: string; what?: string } | null>;
}

/** Where the page buttons of an offer lead (the window's navigation). */
export type OfferNav = OfferDeps["nav"];

const opened: OfferResult = { ok: true, text: "" };

/** Runs a press; never throws. */
export async function runOffer(offer: OfferView, deps: OfferDeps): Promise<OfferResult> {
  const step = offerStep(offer);
  try {
    switch (step.type) {
      case "noTarget":
        return { ok: false, text: offerReasons.noTarget };
      case "unavailable":
        return { ok: false, text: offerReasons.unavailable };
      case "openMaterial": {
        // The page's own call (useMaterialActions.open): the status and the fixed text of the core.
        const r = await deps.library.openMaterial(step.id);
        return r.status === "opened" ? opened : { ok: false, text: r.message };
      }
      case "openSummary":
        deps.nav.openLessonSummary(step.id, step.section);
        return opened;
      case "openTranscript":
        deps.nav.openLessonTranscript(step.id);
        return opened;
      case "openWork": {
        // The work card's «Открыть в Word / PowerPoint»: a copy of the ready file opens in the default program.
        const view = await openWork(deps.library, await deps.turnkey.work(step.id));
        return view === null ? { ok: false, text: offerReasons.workNotReady } : { ok: view.kind === "ok", text: view.kind === "ok" ? "" : view.text };
      }
      case "calendarSync": {
        // «Синхронизировать» of the block «Google Календарь»: Go's own message says how it went.
        const r = await deps.calendar.sync();
        return { ok: r.ok, text: r.message };
      }
      case "page":
        deps.nav.openPage(step.page);
        return opened;
      case "startRecording":
        deps.nav.startRecording();
        return opened;
      case "undoLatest": {
        const r = await deps.undoLatest();
        if (r === null) {
          return { ok: false, text: offerReasons.nothingToUndo };
        }
        // «Отменено: Отмечено выполненным: 3 задания»: the student sees what went back.
        return r.ok ? { ok: true, text: r.what ? `${texts.undoDone}: ${r.what}` : "", once: true } : { ok: false, text: r.text };
      }
    }
  } catch {
    return { ok: false, text: step.type === "calendarSync" ? calendarTexts.actionFailed : step.type === "openWork" ? offerReasons.workFailed : offerReasons.failed };
  }
}

/** The labels the table holds, in catalog order (for the tests that pin them to the pages). */
export const offerActionList: readonly string[] = Object.keys(offerSpecs);
