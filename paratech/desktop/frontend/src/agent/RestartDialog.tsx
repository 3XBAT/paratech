import { type KeyboardEvent, useEffect, useId, useRef } from "react";
import { Button } from "../ui/Button.js";
import { dialogKey, type RestartBlock, restartLabel, type RestartTarget, restartTexts } from "../agentView.js";

// «Начать заново отсюда» (TASK-249): a quiet action under a message of the student and the warning dialog it opens.

/** The action under the student's message: quiet, shown on hover and on keyboard focus (always present for the keyboard). */
export function RestartButton(props: { target: RestartTarget; onOpen(target: RestartTarget, opener: HTMLElement): void }) {
  const label = restartLabel(props.target);
  return (
    <button type="button" className="ag-restart" aria-label={label} title={restartTexts.action} onClick={(e) => props.onOpen(props.target, e.currentTarget)}>
      {restartTexts.action}
    </button>
  );
}

export interface RestartDialogProps {
  /** Why «Скрыть» is unavailable; null: it is available. */
  block: RestartBlock | null;
  /** The call of the way-out button (stop, cancel the request) is on its way. */
  fixBusy: boolean;
  onHide(): void;
  onCancel(): void;
  onFix(): void;
}

/**
 * The warning before the hide: «Скрыть» (the one accent) and «Отмена». A modal inside the panel: focus goes to «Отмена»
 * (never to the button that changes the chat), Tab stays among the buttons, Esc and a click outside take it back. The
 * opener gets the focus back from the panel. While the current request is not over «Скрыть» is off, the reason is written
 * next to it and a button removes the cause (stop, cancel the request).
 */
export function RestartDialog(props: RestartDialogProps) {
  const { block } = props;
  const root = useRef<HTMLDivElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const bodyId = useId();
  const reasonId = useId();

  useEffect(() => {
    cancel.current?.focus();
  }, []);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const buttons = Array.from(root.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
    const k = dialogKey({ key: e.key, shiftKey: e.shiftKey }, buttons.indexOf(document.activeElement as HTMLButtonElement), buttons.length);
    if (k === null) {
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    if (k.act === "cancel") {
      props.onCancel();
    } else {
      buttons[k.index]?.focus();
    }
  };

  return (
    <div
      className="ag-restart-scrim"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) {
          props.onCancel();
        }
      }}
    >
      <div
        ref={root}
        className="ag-restart-dlg"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={block ? `${bodyId} ${reasonId}` : bodyId}
        onKeyDown={onKeyDown}
      >
        <h2 id={titleId} className="ag-restart-title">
          {restartTexts.title}
        </h2>
        <p id={bodyId} className="ag-restart-body">
          {restartTexts.body}
        </p>
        {block && (
          <p id={reasonId} className="ag-restart-reason small muted" role="status">
            {block.reason}
          </p>
        )}
        <div className="ag-actions">
          <Button size="sm" variant="primary" disabled={block !== null} aria-describedby={block ? reasonId : undefined} onClick={props.onHide}>
            {restartTexts.hide}
          </Button>
          {block?.fix && (
            <Button
              size="sm"
              variant="secondary"
              disabled={props.fixBusy}
              onClick={() => {
                props.onFix();
                cancel.current?.focus();
              }}
            >
              {props.fixBusy && block.fix.kind === "stop" ? restartTexts.stopping : block.fix.label}
            </Button>
          )}
          <button ref={cancel} type="button" className="btn ghost sm" onClick={props.onCancel}>
            {restartTexts.cancel}
          </button>
        </div>
      </div>
    </div>
  );
}

/** The note next to the field while an edit is pending: what the send will do, how to back out, and a refusal if any. */
export function RestartNote(props: { id: string; problem: string; onCancel(): void }) {
  return (
    <div className="ag-restart-note small" id={props.id}>
      <span className="ag-restart-note-t" role="status">
        {restartTexts.note}
        {props.problem !== "" && <span className="ag-restart-problem">{props.problem}</span>}
      </span>
      <button type="button" className="ag-restart-undo" aria-label="Отменить правку" onClick={props.onCancel}>
        {restartTexts.undoNote}
      </button>
    </div>
  );
}
