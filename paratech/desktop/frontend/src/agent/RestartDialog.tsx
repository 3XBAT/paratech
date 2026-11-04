import { type KeyboardEvent, useEffect, useId, useRef } from "react";
import { Button } from "../ui/Button.js";
import { dialogKey, type ForkFailure, type RestartBlock, restartLabel, type RestartTarget, restartTexts } from "../agentView.js";
import { IconAction } from "../ui/IconAction.js";
import { ActionButton } from "./MessageActions.js";

// «Начать заново отсюда» (TASK-249, TASK-271): a quiet action under a message of the student and the dialog it opens.

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
  /** Why «Скрыть» is unavailable; null: it is available. «Форк» does not depend on it. */
  block: RestartBlock | null;
  /** The call of the way-out button (stop, cancel the request) is on its way. */
  fixBusy: boolean;
  /** The «Форк» call is on its way: the button says so and takes no second click. */
  forkBusy: boolean;
  /** Why the last «Форк» did not work; null: it did not fail. */
  forkFailure: ForkFailure | null;
  onHide(): void;
  onFork(): void;
  /** «Новый чат» of a failure that says the chat is gone or changed: the dialog closes and a new chat starts. */
  onNewChat(): void;
  onCancel(): void;
  onFix(): void;
}

/**
 * The choice before a restart: «Скрыть» (the one accent) and «Форк», each with a line of what it does, and «Отмена». A
 * modal inside the panel: focus goes to «Отмена» (never to a button that changes something), Tab stays among the buttons,
 * Esc and a click outside take it back. The opener gets the focus back from the panel. While the current request is not
 * over «Скрыть» is off, the reason is written next to it and a button removes the cause (stop, cancel the request);
 * «Форк» is always available. A failed «Форк» is written under it: the button itself tries again, and when trying again
 * cannot help «Новый чат» stands in the same option.
 */
export function RestartDialog(props: RestartDialogProps) {
  const { block, forkFailure } = props;
  const root = useRef<HTMLDivElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const hideHintId = useId();
  const forkHintId = useId();
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

  // A button that turns itself off would drop the focus out of the dialog (and Esc with it): it goes back to «Отмена».
  const fork = () => {
    props.onFork();
    cancel.current?.focus();
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
      <div ref={root} className="ag-restart-dlg" role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={onKeyDown}>
        <h2 id={titleId} className="ag-restart-title">
          {restartTexts.title}
        </h2>
        <div className="ag-restart-opt">
          <div className="ag-actions">
            <Button size="sm" variant="primary" disabled={block !== null} aria-describedby={block === null ? hideHintId : `${hideHintId} ${reasonId}`} onClick={props.onHide}>
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
          </div>
          <p id={hideHintId} className="ag-restart-body">
            {restartTexts.hideHint}
          </p>
          {block && (
            <p id={reasonId} className="ag-restart-reason small muted" role="status">
              {block.reason}
            </p>
          )}
        </div>
        <div className="ag-restart-opt">
          <div className="ag-actions">
            <Button
              size="sm"
              variant="secondary"
              disabled={props.forkBusy}
              aria-busy={props.forkBusy || undefined}
              aria-describedby={forkHintId}
              busy={props.forkBusy}
              busyLabel={restartTexts.forking}
              onClick={fork}
            >
              {restartTexts.fork}
            </Button>
          </div>
          <p id={forkHintId} className="ag-restart-body">
            {restartTexts.forkHint}
          </p>
          {forkFailure && (
            <p className="ag-restart-reason" role="alert">
              {forkFailure.text}
            </p>
          )}
          {forkFailure?.way === "new" && (
            <div className="ag-actions">
              <Button size="sm" variant="secondary" onClick={props.onNewChat}>
                {restartTexts.forkNewChat}
              </Button>
            </div>
          )}
        </div>
        <div className="ag-actions">
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
