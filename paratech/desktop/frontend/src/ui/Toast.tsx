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

/** What the page says about the toast and the key press, as plain facts (see toastVisibleFor in the component). */
export interface UndoKeyScene {
  /** The toast element is in the document. */
  connected: boolean;
  /** It has a box (not display:none). */
  rendered: boolean;
  /** It sits under a [hidden] ancestor (a page behind an overlay). */
  insideHidden: boolean;
  /** The key was pressed inside an open dialog or a modal. */
  inModal: boolean;
}

/** Ctrl+Z undoes only a toast the student can see, and not from behind a modal dialog. */
export function undoKeyAllowed(s: UndoKeyScene): boolean {
  return s.connected && s.rendered && !s.insideHidden && !s.inModal;
}

function sceneOf(toast: HTMLElement | null, target: EventTarget | null): UndoKeyScene {
  const inModal = target instanceof Element && target.closest('dialog[open], [aria-modal="true"]') !== null;
  return {
    connected: toast?.isConnected === true,
    rendered: toast !== null && toast.getClientRects().length > 0,
    insideHidden: toast?.closest("[hidden]") != null,
    inModal,
  };
}

/** The initial «window is away» state: hidden or without focus, so a toast born in a blurred window waits. */
export function windowAway(visibility: string, hasFocus: boolean): boolean {
  return visibility === "hidden" || !hasFocus;
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
  const [away, setAway] = useState(
    () => typeof document !== "undefined" && windowAway(document.visibilityState, document.hasFocus()),
  );
  const paused = hover || focus || away;
  const box = useRef<HTMLDivElement>(null);
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
    const visibility = () => setAway(windowAway(document.visibilityState, document.hasFocus()));
    const left = () => setAway(true);
    const back = () => setAway(document.visibilityState === "hidden");
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (
        !e.defaultPrevented &&
        isUndoKey(e) &&
        !isEditableTarget(e.target as HTMLElement | null) &&
        undoKeyAllowed(sceneOf(box.current, e.target))
      ) {
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
      ref={box}
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
        aria-keyshortcuts="Control+Z"
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
