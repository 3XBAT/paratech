import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { Icon } from "./Icon.js";

/** The toast closes by itself after this long, unless the pointer or the focus is on it or the window is away. */
export const toastMs = 10_000;

export interface ToastView {
  /** A new id is a new toast: its timer starts again. */
  id: number;
  text: string;
  /** The one button next to the text («Отменить»): a toast is always about something that can be put back. */
  actionLabel: string;
}

/** The tooltip of the action: Ctrl+Z does the same while the toast is shown. */
export const toastActionTitle = (label: string) => `${label} (Ctrl+Z)`;

/** Ctrl+Z (Cmd+Z), by the physical key too, so the Russian layout works. */
export function isUndoKey(e: { key: string; code: string; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean; repeat?: boolean }): boolean {
  if (!(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey || e.repeat === true) {
    return false;
  }
  return e.code === "KeyZ" || e.key.toLowerCase() === "z";
}

/** A text field has its own undo: Ctrl+Z there is not for the toast. */
export function isEditableTarget(t: { tagName?: string; isContentEditable?: boolean } | null): boolean {
  if (t === null) {
    return false;
  }
  const tag = (t.tagName ?? "").toUpperCase();
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || t.isContentEditable === true;
}

/**
 * One toast at the bottom of the content column; a new one replaces the old. The status region is always there, so a
 * screen reader announces the text when it appears. Page level: each page owns its toast and its state.
 * onClose: the toast is over (the timer or the student). onDismiss: the student closed it from inside (× or Esc), so
 * the page can put the focus somewhere sensible.
 */
export function Toast(props: {
  toast: ToastView | null;
  onAction: () => void;
  onClose: () => void;
  onDismiss?: () => void;
  closeLabel?: string;
  ms?: number;
}) {
  return (
    <div className="toast-host" role="status" aria-live="polite">
      {props.toast !== null && (
        <ToastBody
          key={props.toast.id}
          toast={props.toast}
          onAction={props.onAction}
          onClose={props.onClose}
          onDismiss={props.onDismiss}
          closeLabel={props.closeLabel ?? "Закрыть"}
          ms={props.ms ?? toastMs}
        />
      )}
    </div>
  );
}

function ToastBody(props: {
  toast: ToastView;
  onAction: () => void;
  onClose: () => void;
  onDismiss?: () => void;
  closeLabel: string;
  ms: number;
}) {
  const [hover, setHover] = useState(false);
  const [focus, setFocus] = useState(false);
  // Alt-Tab must not eat the undo: the timer waits while the window is hidden or not the active one.
  const [away, setAway] = useState(() => typeof document !== "undefined" && document.visibilityState === "hidden");
  const paused = hover || focus || away;
  const close = useRef(props.onClose);
  close.current = props.onClose;
  const act = useRef(props.onAction);
  act.current = props.onAction;

  useEffect(() => {
    if (paused) {
      return;
    }
    const timer = setTimeout(() => close.current(), props.ms);
    return () => clearTimeout(timer);
  }, [paused, props.ms]);

  useEffect(() => {
    const visibility = () => setAway(document.visibilityState === "hidden");
    const left = () => setAway(true);
    const back = () => setAway(document.visibilityState === "hidden");
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (!e.defaultPrevented && isUndoKey(e) && !isEditableTarget(e.target as HTMLElement | null)) {
        e.preventDefault();
        act.current();
      }
    };
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("blur", left);
    window.addEventListener("focus", back);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("blur", left);
      window.removeEventListener("focus", back);
      document.removeEventListener("keydown", onKey);
    };
  }, []);

  const dismiss = () => {
    props.onClose();
    props.onDismiss?.();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      dismiss();
    }
  };

  return (
    <div
      className="toast"
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      onFocus={() => setFocus(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) {
          setFocus(false);
        }
      }}
      onKeyDown={onKeyDown}
    >
      <span className="toast-text">{props.toast.text}</span>
      <button
        type="button"
        className="toast-act"
        title={toastActionTitle(props.toast.actionLabel)}
        onClick={props.onAction}
      >
        {props.toast.actionLabel}
      </button>
      <button type="button" className="toast-x" aria-label={props.closeLabel} title={props.closeLabel} onClick={dismiss}>
        <Icon name="x" />
      </button>
    </div>
  );
}
