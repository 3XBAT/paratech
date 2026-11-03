import { useEffect, useId, useRef, useState } from "react";
import type { RemovedName } from "../bindings/library.js";
import {
  busyRestore,
  type DialogView,
  type DiscardWay,
  idleRestore,
  removalTexts,
  type RestoreCall,
  type RestoreState,
  restoreLineView,
} from "../subjectRemovalView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";

// «Удалить предмет» and «Вернуть» (TASK-289): the confirm dialog and the inline line with «Вернуть». Names of subjects are
// typed or imported text: only text nodes here, never markup.

/** «Удалить предмет» in the head of the page of a subject: quiet (red only in the dialog it opens), last in the row. */
export function RemoveSubjectButton(props: { disabled: boolean; onClick(opener: HTMLElement): void }) {
  return (
    <Button variant="ghost" disabled={props.disabled} onClick={(e) => props.onClick(e.currentTarget)}>
      {removalTexts.action}
    </Button>
  );
}

/**
 * The question before «Удалить предмет»: a modal dialog (native <dialog>, so the page behind it is inert and Tab stays
 * inside) with the warning, «Отмена» (it takes the focus, never the red button) and «Удалить». While the call runs both
 * buttons are off and Esc does nothing. A refusal is written in the dialog with the button that leads on. The focus
 * returns to `returnTo` (the opener) when the dialog goes, if that is still on the page.
 */
export function RemoveSubjectDialog(props: {
  view: DialogView;
  returnTo: HTMLElement | null;
  onAction(action: DiscardWay): void;
  onDismiss(): void;
}) {
  const { view } = props;
  const ref = useRef<HTMLDialogElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const way = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const bodyId = useId();
  const busy = view.busy;
  const refused = view.problem !== "";

  useEffect(() => {
    const d = ref.current;
    if (d !== null && !d.open) {
      d.showModal();
      (cancel.current ?? way.current)?.focus();
    }
    const back = props.returnTo;
    return () => {
      d?.close();
      if (back !== null && back.isConnected) {
        back.focus();
      }
    };
    // Mounted once per question; the opener is fixed for it.
  }, []);

  // A refusal appeared: the button that leads on takes the focus (the one that was pressed is off and the focus is lost).
  useEffect(() => {
    if (refused && !busy) {
      way.current?.focus();
    }
  }, [refused, busy]);

  const dismiss = props.view.cancel === null ? () => props.onAction(view.confirm.action) : props.onDismiss;
  return (
    <dialog
      ref={ref}
      className="dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={bodyId}
      aria-busy={busy || undefined}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) {
          dismiss();
        }
      }}
    >
      <h2 id={titleId}>{view.title}</h2>
      <div className="dialog-body">
        <p id={bodyId}>{view.body}</p>
        <p className="small muted" role="status">
          {view.status}
        </p>
        {refused && (
          <ResultLine kind="fail" role="alert">
            {view.problem}
          </ResultLine>
        )}
      </div>
      <div className="acts">
        {view.cancel !== null && (
          <button ref={cancel} type="button" className="btn" disabled={view.cancel.disabled} onClick={props.onDismiss}>
            {view.cancel.label}
          </button>
        )}
        <button
          ref={way}
          type="button"
          className={view.confirm.danger ? "btn danger" : "btn primary"}
          disabled={view.confirm.disabled}
          onClick={() => props.onAction(view.confirm.action)}
        >
          {view.confirm.label}
        </button>
      </div>
    </dialog>
  );
}

export type { RestoreCall };

/** The call that could not be made (no way to restore here): the line says so and offers «Повторить». */
export const restoreUnavailable: RestoreCall = { state: { phase: "problem", problem: "failed" }, subject: null };

/**
 * «Предмет с таким названием удалён» with «Вернуть»: what the create form shows instead of «уже есть» when a removed
 * subject holds the name. The restored subject comes back through `onSettled`.
 */
export function RemovedNameLine(props: { removed: RemovedName; restore(removed: RemovedName): Promise<RestoreCall>; onSettled(call: RestoreCall): void }) {
  return (
    <RestoreRow
      key={props.removed.subjectId}
      idleText={removalTexts.nameRemoved}
      doneText={removalTexts.restored}
      restore={() => props.restore(props.removed)}
      onSettled={props.onSettled}
    />
  );
}

/**
 * One line with «Вернуть»: the text of its place (`idleText`), «Возврат…» while the call runs, `doneText` after it, and for a
 * refusal the reason in plain words (see restoreLineView) with «Повторить» or, for a final one, no button. Used by the
 * line after «Удалить предмет», by the create form (the name of a removed subject) and by the rows of the schedule review.
 * `onSettled` tells the page how the call ended. A status line keeps the focus when the pressed button goes.
 */
export function RestoreRow(props: {
  idleText: string;
  doneText: string;
  restore(): Promise<RestoreCall>;
  onSettled(call: RestoreCall): void;
  /** Set: «Закрыть» drops the line (the line has no other way out). */
  onClose?: () => void;
  /** The first button takes the focus when the line appears (the button that had it is gone). */
  autoFocus?: boolean;
}) {
  const [state, setState] = useState<RestoreState>(idleRestore);
  const running = useRef(false); // a double click comes before the re-render that disables the button
  const root = useRef<HTMLDivElement>(null);
  const status = useRef<HTMLDivElement>(null);
  const v = restoreLineView(props.idleText, props.doneText, state, props.onClose !== undefined);

  async function run() {
    if (running.current || state.phase === "done") {
      return;
    }
    running.current = true;
    setState(busyRestore);
    try {
      const call = await props.restore();
      setState(call.state);
      props.onSettled(call);
    } finally {
      running.current = false;
    }
  }

  // On arrival «Вернуть» takes the focus. Later than the dialog that just closed: it gives the focus back to its opener.
  useEffect(() => {
    if (!props.autoFocus) {
      return;
    }
    const t = setTimeout(() => root.current?.querySelector("button")?.focus(), 0);
    return () => clearTimeout(t);
  }, []);

  useEffect(() => {
    if (state.phase !== "problem" && state.phase !== "done") {
      return;
    }
    const at = document.activeElement;
    if (at === null || at === document.body || root.current?.contains(at) === true) {
      status.current?.focus();
    }
  }, [state.phase]);

  return (
    <div className="lrow-error" ref={root}>
      <div ref={status} tabIndex={-1} role={v.tone === "fail" ? "alert" : "status"} className={v.tone === "info" ? "muted" : undefined}>
        {v.tone === "info" ? v.text : <ResultLine kind={v.tone}>{v.text}</ResultLine>}
      </div>
      {v.button !== null && (
        <Button
          size="sm"
          busy={v.busy}
          busyLabel={removalTexts.restoring}
          disabled={v.busy}
          onClick={() => void run()}
        >
          {v.button === "retry" ? removalTexts.retry : removalTexts.restore}
        </Button>
      )}
      {v.close && props.onClose !== undefined && (
        <Button size="sm" variant="ghost" disabled={v.busy} onClick={props.onClose}>
          {removalTexts.close}
        </Button>
      )}
    </div>
  );
}
