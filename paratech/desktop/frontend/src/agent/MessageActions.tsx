import { type MouseEvent, type ReactNode, type Ref, useEffect, useRef, useState } from "react";
import { copyable, copyNote, copyNoteMs, type CopyState, copyOutcome, messageActionTexts } from "../agentView.js";
import { Icon, type IconName } from "../ui/Icon.js";

// The quiet actions under a message (TASK-365): icons only, the name is a tooltip on hover and on keyboard focus (the
// same look as the rail's) and the aria-label. Never on service cards.

/** One icon button of the row. `note` keeps the tooltip open (a confirmation the user must see). */
export function ActionButton(props: {
  icon: IconName;
  label: string;
  tip?: string;
  className?: string;
  note?: boolean;
  busy?: boolean;
  disabled?: boolean;
  /** "menuitem" inside the chat list menu. */
  role?: "menuitem";
  ref?: Ref<HTMLButtonElement>;
  onClick(e: MouseEvent<HTMLButtonElement>): void;
}) {
  return (
    <button
      type="button"
      ref={props.ref}
      role={props.role}
      className={["ag-ib", props.className, props.note ? "ag-ib-note" : ""].filter((c) => c !== undefined && c !== "").join(" ")}
      aria-label={props.label}
      aria-busy={props.busy || undefined}
      disabled={props.busy || props.disabled}
      onClick={props.onClick}
    >
      <Icon name={props.icon} />
      <span className="ag-ib-tip" aria-hidden="true">
        {props.tip ?? props.label}
      </span>
    </button>
  );
}

/**
 * «Копировать»: the text goes to the clipboard through the shell (`copy`); the icon turns into a check mark for a moment
 * and the tooltip says «Скопировано» (or «Не удалось скопировать»), also announced politely. Nothing else is shown.
 */
export function CopyAction(props: { text: string; copy(text: string): Promise<void> }) {
  const [state, setState] = useState<CopyState>("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const alive = useRef(true);
  useEffect(
    () => () => {
      alive.current = false;
      if (timer.current !== null) {
        clearTimeout(timer.current);
      }
    },
    [],
  );

  const press = () => {
    void copyOutcome(props.copy, props.text).then((outcome) => {
      if (!alive.current) {
        return;
      }
      setState(outcome);
      if (timer.current !== null) {
        clearTimeout(timer.current);
      }
      timer.current = setTimeout(() => setState("idle"), copyNoteMs);
    });
  };

  const icon: IconName = state === "copied" ? "ok" : state === "failed" ? "error" : "copy";
  return (
    <>
      <ActionButton icon={icon} label={messageActionTexts.copy} tip={copyNote(state)} note={state !== "idle"} onClick={press} />
      <span className="ag-vh" role="status" aria-live="polite" aria-atomic="true">
        {state === "idle" ? "" : copyNote(state)}
      </span>
    </>
  );
}

function Row(props: { side: "user" | "agent"; children: ReactNode }) {
  return (
    <div className={`ag-acts ag-acts-${props.side}`}>{props.children}</div>
  );
}

/** Under the student's message: «Копировать» (when the message has text and the panel can copy) and what the caller adds. */
export function UserActions(props: { text: string; copy?: (text: string) => Promise<void>; children?: ReactNode }) {
  const canCopy = props.copy !== undefined && copyable(props.text);
  if (!canCopy && !props.children) {
    return null;
  }
  return (
    <Row side="user">
      {canCopy && props.copy && <CopyAction text={props.text} copy={props.copy} />}
      {props.children}
    </Row>
  );
}

/** Under the agent's reply: «Копировать» (the reply text) and «Повторить ответ» (when offered). */
export function AgentActions(props: { text: string; copy?: (text: string) => Promise<void>; rerun: boolean; busy: boolean; onRerun(): void }) {
  const canCopy = props.copy !== undefined && copyable(props.text);
  if (!canCopy && !props.rerun) {
    return null;
  }
  return (
    <Row side="agent">
      {canCopy && props.copy && <CopyAction text={props.text} copy={props.copy} />}
      {props.rerun && <ActionButton icon="retry" label={messageActionTexts.rerun} className="ag-ib-rerun" busy={props.busy} onClick={props.onRerun} />}
    </Row>
  );
}
