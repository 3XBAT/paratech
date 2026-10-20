// The deletion of recordings (TASK-095): the confirmation built from the core's preview, the user's choice of found
// homework, the request list and the result. What can be deleted and what goes with it is the core's (ARCH-44).

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import type { RecordingItem } from "./bindings/activity.js";
import {
  type DeletePreview,
  type DeleteRequest,
  type DeleteResult,
  errorMessages,
} from "./bindings/recordingDelete.js";
import { plural } from "./plural.js";
import { rowTitle } from "./recordingRowView.js";

export const deleteTexts = {
  menu: "Удалить",
  summaryButton: "Удалить запись",
  confirm: "Удалить",
  busy: "Удаление…",
  cancel: "Отмена",
  loading: "Загрузка…",
  irreversible: "Удаление без возможности восстановления.",
  willDelete: "Удалится:",
  nothingStored: "Звука и результатов нет — удалится только строка в списке.",
  notDeleted: "Не будет удалена:",
  drafts: "С решениями — удалятся, только если отметить:",
  kept: "По ним отправлялись письма — останутся в «Домашних заданиях»:",
  allMissing: "Записи уже удалены. Обновите список.",
  selectedDelete: "Удалить выбранные",
  selectedClear: "Снять выбор",
  savingHint: "Запись сохраняется: подождите несколько секунд.",
} as const;

/** The checked homework: the plain ones all together (on by default), the ones with drafts one by one (off). */
export interface DeleteChoice {
  plain: boolean;
  drafts: ReadonlySet<string>;
}

export const initialDeleteChoice: DeleteChoice = { plain: true, drafts: new Set() };

export function withDraft(c: DeleteChoice, id: string, on: boolean): DeleteChoice {
  const drafts = new Set(c.drafts);
  if (on) {
    drafts.add(id);
  } else {
    drafts.delete(id);
  }
  return { ...c, drafts };
}

/** The title of a recording in the confirmation, as its row shows it. */
export function deleteTitle(r: { title: string; startedAt: string }): string {
  return rowTitle(r).text;
}

export interface ConfirmRecording {
  id: string;
  title: string;
  deletes: string[];
  /** The core's refusal; "" when it is deleted. */
  refusal: string;
}

export interface ConfirmAssignment {
  id: string;
  /** Model output: plain text only. */
  title: string;
  /** The title of its recording, for a confirmation of several. */
  recording: string;
}

export interface DeleteConfirmation {
  title: string;
  recordings: ConfirmRecording[];
  /** Plain found homework of the deletable recordings: «Удалить и найденные задания (N)». */
  plain: ConfirmAssignment[];
  plainLabel: string;
  drafts: ConfirmAssignment[];
  /** Never deleted: emails were sent. */
  kept: ConfirmAssignment[];
  /** «Уже удалены: N»; "" for none. */
  missing: string;
  /** At least one recording can go. */
  canDelete: boolean;
}

function titleFor(n: number): string {
  return n <= 1 ? "Удалить запись?" : `Удалить ${n} ${plural(n, "запись", "записи", "записей")}?`;
}

/** The confirmation; before the preview comes (null) only its title, from the number of ids asked for. */
export function deleteConfirmation(p: DeletePreview | null, asked: number): DeleteConfirmation {
  const out: DeleteConfirmation = {
    title: titleFor(asked),
    recordings: [],
    plain: [],
    plainLabel: "",
    drafts: [],
    kept: [],
    missing: "",
    canDelete: false,
  };
  if (p === null) {
    return out;
  }
  const deletable = p.recordings.filter((r) => r.deletable);
  out.title = titleFor(deletable.length > 0 ? deletable.length : p.recordings.length);
  out.canDelete = deletable.length > 0;
  for (const r of p.recordings) {
    const title = deleteTitle(r);
    out.recordings.push({
      id: r.id,
      title,
      deletes: r.deletes,
      refusal: r.deletable ? "" : r.refusalMessage || errorMessages.unavailable,
    });
    if (!r.deletable) {
      continue;
    }
    for (const a of r.assignments) {
      const item = { id: a.id, title: a.title, recording: title };
      switch (a.class) {
        case "plain":
          out.plain.push(item);
          break;
        case "draft":
          out.drafts.push(item);
          break;
        case "emails":
          out.kept.push(item);
          break;
      }
    }
  }
  out.plainLabel = `Удалить и найденные задания (${out.plain.length})`;
  if (p.missing.length > 0) {
    out.missing = p.recordings.length === 0 ? deleteTexts.allMissing : `Уже удалены: ${p.missing.length}.`;
  }
  return out;
}

/**
 * One request per deletable recording with the checked homework: the plain ones when the box is on, the drafts the
 * user checked. Assignments with emails are never sent.
 */
export function deleteRequests(p: DeletePreview, c: DeleteChoice): DeleteRequest[] {
  return p.recordings
    .filter((r) => r.deletable)
    .map((r) => ({
      recordingId: r.id,
      assignmentIds: r.assignments
        .filter((a) => (a.class === "plain" && c.plain) || (a.class === "draft" && c.drafts.has(a.id)))
        .map((a) => a.id),
    }));
}

export interface DeleteOutcome {
  kind: "ok" | "wait" | "fail";
  lines: string[];
}

/** The result line: «Удалено N» and the recordings that were not deleted, by their titles in the preview. */
export function deleteOutcome(res: DeleteResult, p: DeletePreview | null): DeleteOutcome {
  const titles = new Map((p?.recordings ?? []).map((r) => [r.id, deleteTitle(r)]));
  const lines: string[] = [];
  if (res.removed > 0 || res.failures.length === 0) {
    lines.push(res.message);
  }
  if (res.keptAssignments.length > 0) {
    lines.push(`Найденные задания остались в «Домашних заданиях»: ${res.keptAssignments.length}.`);
  }
  for (const f of res.failures) {
    lines.push(`${titles.get(f.recordingId) ?? "Запись"}: ${f.message}`);
  }
  let kind: DeleteOutcome["kind"] = res.pending > 0 ? "wait" : "ok";
  if (res.failures.length > 0) {
    kind = res.removed > 0 ? "wait" : "fail";
  }
  return { kind, lines };
}

const knownErrors: ReadonlySet<string> = new Set(Object.values(errorMessages));

/** The core's text of a failed call; any other error gets the generic text (raw text is not shown). */
export function deleteErrorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return knownErrors.has(msg) ? msg : errorMessages.unavailable;
}

/**
 * Whether a row offers «Удалить», from the stage the core reports: not while it records or is being saved. Only a hint
 * for the menu and the checkbox; the core's preview decides.
 */
export function rowDeleteOffer(r: Pick<RecordingItem, "stage" | "status">): { disabled: boolean; hint: string } {
  if (r.stage === "recording") {
    return { disabled: true, hint: errorMessages.inProgress };
  }
  if (r.stage === "saving" && r.status !== "RECOVERY_REQUIRED") {
    return { disabled: true, hint: deleteTexts.savingHint };
  }
  return { disabled: false, hint: "" };
}

/** The selection with id checked or not. */
export function withSelected(sel: ReadonlySet<string>, id: string, on: boolean): ReadonlySet<string> {
  const next = new Set(sel);
  if (on) {
    next.add(id);
  } else {
    next.delete(id);
  }
  return next;
}

/** The selection without the rows that are gone or cannot be deleted now; the same set when nothing changes. */
export function pruneSelection(
  sel: ReadonlySet<string>,
  rows: readonly Pick<RecordingItem, "id" | "stage" | "status">[],
): ReadonlySet<string> {
  const ok = new Set(rows.filter((r) => !rowDeleteOffer(r).disabled).map((r) => r.id));
  const next = new Set([...sel].filter((id) => ok.has(id)));
  return next.size === sel.size ? sel : next;
}

/** «Выбрано 3». */
export function selectedText(n: number): string {
  return `Выбрано ${n}`;
}
