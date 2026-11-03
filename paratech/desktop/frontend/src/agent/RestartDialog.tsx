import { type KeyboardEvent, useEffect, useId, useRef } from "react";
import { Button } from "../ui/Button.js";
import { dialogKey, type RestartBlock, restartLabel, type RestartTarget, restartTexts } from "../agentView.js";
import { IconAction } from "../ui/IconAction.js";
import { ActionButton } from "./MessageActions.js";

// «Начать заново отсюда» (TASK-249): a quiet action under a message of the student and the warning dialog it opens.

/**
 * The action under the student's message, an icon of the row (agent/MessageActions): always visible but quiet, full on
 * hover and keyboard focus. The tooltip says «Начать заново отсюда»; the accessible name adds the start of the message.
 */
export function RestartButton(props: { target: RestartTarget; onOpen(target: RestartTarget, opener: HTMLElement): void }) {
  return (
    <ActionButton
      icon="retry"
      label={restartLabel(props.target)}
      tip={restartTexts.action}
      className="ag-ib-restart"
      onClick={(e) => props.onOpen(props.target, e.currentTarget)}
    />
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
        aria-describedby={bodyId}
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
          <Button size="sm" variant="primary" disabled={block !== null} onClick={props.onHide}>
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
              {props.fixBusy ? (block.fix.kind === "stop" ? restartTexts.stopping : restartTexts.cancelling) : block.fix.label}
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
export function RestartNote(props: { id: string; problem: string; onCancel(): void; onRestoreText?: (() => void) | null }) {
  return (
    <div className="ag-restart-note small" id={props.id}>
      <span className="ag-restart-note-t" role="status">
        {restartTexts.note}
        {props.problem !== "" && <span className="ag-restart-problem">{props.problem}</span>}
      </span>
      <span className="ag-restart-acts">
        {/* «insert» (an arrow into a field) puts the text back into the box; «undo» takes the whole restart back. */}
        {props.onRestoreText && <IconAction icon="insert" place="below" label={restartTexts.restoreText} onClick={props.onRestoreText} />}
        <IconAction icon="undo" place="below" label={restartTexts.undoLabel} onClick={props.onCancel} />
      </span>
    </div>
  );
}
