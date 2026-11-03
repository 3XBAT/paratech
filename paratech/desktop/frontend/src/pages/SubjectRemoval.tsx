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
  restoreAriaLabel,
  restoreLineView,
} from "../subjectRemovalView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";

// «Удалить предмет» and «Вернуть» (TASK-289): the confirm dialog and the inline line with «Вернуть». Names of subjects are
// typed or imported text: only text nodes here, never markup.

/**
 * The question before «Удалить предмет»: a modal dialog (native <dialog>, so the page behind it is inert and Tab stays
 * inside) with the warning, «Отмена» (it takes the focus, never the red button) and «Удалить». While the call runs both
 * buttons are off and Esc does nothing. A refusal is written in the dialog with the button that leads on («Обновить»
 * asks the question again in place). The focus returns to `returnTo` (the opener) when the dialog goes, if that is still
 * on the page.
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
  // The question came back («Обновить» read it again): «Отмена» takes the focus again, never the red button.
  useEffect(() => {
    if (refused && !busy) {
      way.current?.focus();
    } else if (!refused) {
      cancel.current?.focus();
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
        <div className="small muted" role="status">
          {view.status}
        </div>
        {refused && (
          <ResultLine kind="fail" role="alert">
            {view.problem}
          </ResultLine>
        )}
      </div>
      {/* The Windows order (TASK-347, as Dialog in controls.tsx): the action, then «Отмена» at the right edge; the focus
          still starts on «Отмена», never on the red action. */}
      <div className="acts">
        <button
          ref={way}
          type="button"
          className={view.confirm.danger ? "btn danger" : "btn primary"}
          disabled={view.confirm.disabled}
          onClick={() => props.onAction(view.confirm.action)}
        >
          {view.confirm.label}
        </button>
        {view.cancel !== null && (
          <button ref={cancel} type="button" className="btn" disabled={view.cancel.disabled} onClick={props.onDismiss}>
            {view.cancel.label}
          </button>
        )}
      </div>
    </dialog>
  );
}

export type { RestoreCall };

/** The call that could not be made (no way to restore here): the line says so and offers «Повторить». */
export const restoreUnavailable: RestoreCall = { state: { phase: "problem", problem: "failed" }, subject: null };

/**
 * «Предмет с таким названием удалён» with «Вернуть»: what the create form shows instead of «уже есть» when a removed
 * subject holds the name. The restored subject comes back through `onSettled`. The text itself is the live region of
 * NameForm (the line is `quiet`), so it is announced once. `onChanged` runs the create again for a fresh version after
 * `changed`; `onDismiss` tells the form that a dead line («Закрыть») is gone. `name` is the typed name, for the button's label.
 */
export function RemovedNameLine(props: {
  removed: RemovedName;
  name?: string;
  restore(removed: RemovedName): Promise<RestoreCall>;
  onSettled(call: RestoreCall): void;
  onChanged?: () => void;
  onDismiss?: () => void;
}) {
  return (
    <RestoreRow
      key={`${props.removed.subjectId}:${props.removed.version}`}
      idleText={removalTexts.nameRemoved}
      doneText={removalTexts.restored}
      name={props.name}
      quiet
      retype
      restore={() => props.restore(props.removed)}
      onSettled={props.onSettled}
      onChanged={props.onChanged}
      onDismiss={props.onDismiss}
    />
  );
}

/** Moves the focus out of a line that is about to go: to the input of its form, else to the panel it sits in. */
function focusAround(root: HTMLElement | null) {
  const host = root?.parentElement?.closest<HTMLElement>("form, [tabindex='-1']") ?? null;
  if (host === null) {
    return;
  }
  (host.tagName === "FORM" ? host.querySelector<HTMLElement>("input") : host)?.focus();
}

/**
 * One line with «Вернуть»: the text of its place (`idleText`), «Возврат…» while the call runs, `doneText` after it, and for a
 * refusal the reason in plain words (see restoreLineView) with «Повторить» or «Закрыть». Used by the line after «Удалить
 * предмет», by the create form (the name of a removed subject) and by the rows of the schedule review. `onSettled` tells
 * the page how the call ended. A status line keeps the focus when the pressed button goes.
 */
export function RestoreRow(props: {
  idleText: string;
  doneText: string;
  restore(): Promise<RestoreCall>;
  onSettled(call: RestoreCall): void;
  /** Set: «Закрыть» is offered on the live line too and drops it (the line has no other way out). */
  onClose?: () => void;
  /** After `changed`: «Повторить» fetches a fresh version in the place's own way (the row can do it only if this is set). */
  onChanged?: () => void;
  /** A dead line (the subject is gone for good) was closed: the place forgets it. */
  onDismiss?: () => void;
  /** The name of the subject, when known: the accessible name of «Вернуть». */
  name?: string;
  /** The idle text is shown elsewhere (a live region of the place): the line shows only its buttons until a refusal. */
  quiet?: boolean;
  /** Typing the name again leads on in this place (the create form). */
  retype?: boolean;
  /** The first button takes the focus when the line appears (the button that had it is gone). */
  autoFocus?: boolean;
}) {
  const [state, setState] = useState<RestoreState>(idleRestore);
  const [closed, setClosed] = useState(false);
  const running = useRef(false); // a double click comes before the re-render that disables the button
  const root = useRef<HTMLDivElement>(null);
  const status = useRef<HTMLDivElement>(null);
  const v = restoreLineView(props.idleText, props.doneText, state, {
    canClose: props.onClose !== undefined,
    refetch: props.onChanged !== undefined,
    retype: props.retype === true,
  });

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

  function close() {
    focusAround(root.current);
    setClosed(true);
    props.onClose?.();
    props.onDismiss?.();
  }

  // On arrival «Вернуть» takes the focus when asked to, or when the button that had it is gone (focus on the page body).
  // Later than the dialog that just closed: it gives the focus back to its opener.
  useEffect(() => {
    const t = setTimeout(() => {
      const at = document.activeElement;
      if (props.autoFocus === true || at === null || at === document.body) {
        root.current?.querySelector("button")?.focus();
      }
    }, 0);
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

  if (closed) {
    return null;
  }
  const showText = !(props.quiet === true && v.tone === "info" && (state.phase === "idle" || state.phase === "busy"));
  return (
    <div className="lrow-error" ref={root}>
      {showText && (
        <div ref={status} tabIndex={-1} role={v.tone === "fail" ? "alert" : "status"} className={v.tone === "info" ? "muted" : undefined}>
          {v.tone === "info" ? v.text : <ResultLine kind={v.tone}>{v.text}</ResultLine>}
        </div>
      )}
      {v.button !== null && (
        <Button
          size="sm"
          busy={v.busy}
          busyLabel={removalTexts.restoring}
          disabled={v.busy}
          aria-label={v.button === "restore" ? restoreAriaLabel(props.name ?? "") : undefined}
          onClick={() => (v.button === "refetch" ? props.onChanged?.() : void run())}
        >
          {v.button === "restore" ? removalTexts.restore : removalTexts.retry}
        </Button>
      )}
      {v.close && (
        <Button size="sm" variant="ghost" disabled={v.busy} onClick={close}>
          {removalTexts.close}
        </Button>
      )}
    </div>
  );
}
