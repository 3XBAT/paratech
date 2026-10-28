import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { Icon } from "./Icon.js";

/** The toast closes by itself after this long, unless the pointer or the focus is on it. */
export const toastMs = 10_000;

export interface ToastView {
  /** A new id is a new toast: its timer starts again. */
  id: number;
  text: string;
  /** The one button next to the text («Отменить»): a toast is always about something that can be put back. */
  actionLabel: string;
}

/**
 * One toast at the bottom of the window; a new one replaces the old. The status region is always there, so a screen
 * reader announces the text when it appears. Page level: each page owns its toast and its state.
 */
export function Toast(props: {
  toast: ToastView | null;
  onAction: () => void;
  onClose: () => void;
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
          closeLabel={props.closeLabel ?? "Закрыть"}
          ms={props.ms ?? toastMs}
        />
      )}
    </div>
  );
}

function ToastBody(props: { toast: ToastView; onAction: () => void; onClose: () => void; closeLabel: string; ms: number }) {
  const [hover, setHover] = useState(false);
  const [focus, setFocus] = useState(false);
  const paused = hover || focus;
  const close = useRef(props.onClose);
  close.current = props.onClose;

  useEffect(() => {
    if (paused) {
      return;
    }
    const timer = setTimeout(() => close.current(), props.ms);
    return () => clearTimeout(timer);
  }, [paused, props.ms]);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      props.onClose();
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
      <button type="button" className="btn ghost sm toast-act" onClick={props.onAction}>
        {props.toast.actionLabel}
      </button>
      <button
        type="button"
        className="btn icon-btn toast-x"
        aria-label={props.closeLabel}
        title={props.closeLabel}
        onClick={props.onClose}
      >
        <Icon name="x" />
      </button>
    </div>
  );
}
