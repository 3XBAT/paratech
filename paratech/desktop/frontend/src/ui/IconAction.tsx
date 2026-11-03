import { type FocusEvent, type KeyboardEvent, type MouseEvent, type Ref, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { Icon, type IconName } from "./Icon.js";

// The one icon button for the small, secondary places of every window (TASK-368): headers, rows under messages, toolbars,
// row actions, cards. The visible words of such a button are its tooltip (hover and keyboard focus, Esc dismisses it) and
// its aria-label. Words stay on main actions and dialog buttons (Button). Look and rules: ui/iconAction.css.

export type TipPlace = "above" | "below" | "left" | "right";

export interface IconActionProps {
  icon: IconName;
  /** The accessible name and, unless `tipText` says otherwise, the tooltip: the Russian words the button used to show. */
  label: string;
  /** A tooltip other than the name (the copy button's «Скопировано»). */
  tipText?: string;
  /** Where the tooltip opens; it flips to the other side and shifts inwards when it would leave its clipping box. */
  place?: TipPlace;
  /** "danger": red on hover and focus (cancel, delete). */
  variant?: "default" | "danger";
  /** "sm": 28px for dense lists; the default is 32px. */
  size?: "sm" | "md";
  /** Keeps the tooltip open (a confirmation the user must see). */
  note?: boolean;
  /** A running call: a small spinner instead of the icon, the button is off and aria-busy. */
  busy?: boolean;
  disabled?: boolean;
  /** Said in the tooltip and to screen readers while the button is off: a disabled control always says why. */
  disabledReason?: string;
  /** A button that opens or closes something: aria-expanded. */
  expanded?: boolean;
  /** "menuitem" inside a menu. */
  role?: "menuitem";
  /** -1: not a tab stop (the field keeps the focus). */
  tabIndex?: number;
  className?: string;
  id?: string;
  /** `data-*` attributes (without the prefix) that the page's focus code finds the button by. */
  data?: Readonly<Record<string, string>>;
  ref?: Ref<HTMLButtonElement>;
  onClick(e: MouseEvent<HTMLButtonElement>): void;
  /** For a button that must not move a text selection before the click reads it (copy of the selection). */
  onMouseDown?(e: MouseEvent<HTMLButtonElement>): void;
}

interface Box {
  left: number;
  right: number;
  top: number;
  bottom: number;
}

/**
 * Where a tooltip of box `tip` goes so that it stays in `clip`: `dx` moves it sideways, `flip` opens it on the other
 * side. Pure, so the tests can check it.
 */
export function fitTip(tip: Box, clip: Box, place: TipPlace, margin = 4): { dx: number; flip: boolean } {
  if (place === "above" || place === "below") {
    let dx = 0;
    if (tip.right > clip.right - margin) {
      dx = clip.right - margin - tip.right;
    }
    if (tip.left + dx < clip.left + margin) {
      dx = clip.left + margin - tip.left;
    }
    const flip = place === "above" ? tip.top < clip.top + margin : tip.bottom > clip.bottom - margin;
    return { dx, flip };
  }
  const flip = place === "left" ? tip.left < clip.left + margin : tip.right > clip.right - margin;
  return { dx: 0, flip };
}

function dataAttributes(data: Readonly<Record<string, string>> | undefined): Record<string, string> {
  return Object.fromEntries(Object.entries(data ?? {}).map(([k, v]) => [`data-${k}`, v]));
}

/** The nearest ancestor that clips (scrolls) its content, else the window. */
function clipBox(el: HTMLElement): Box {
  const view: Box = { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
  for (let p = el.parentElement; p !== null && p !== document.body; p = p.parentElement) {
    const s = getComputedStyle(p);
    if (s.overflowX !== "visible" || s.overflowY !== "visible") {
      const r = p.getBoundingClientRect();
      return { left: Math.max(r.left, view.left), top: Math.max(r.top, view.top), right: Math.min(r.right, view.right), bottom: Math.min(r.bottom, view.bottom) };
    }
  }
  return view;
}

/** Hover opens the tooltip after this pause (a pointer passing over a row does not flash tips); keyboard focus opens it at once. */
export const TIP_DELAY_MS = 350;

export function IconAction(props: IconActionProps) {
  const place = props.place ?? "above";
  const tip = useRef<HTMLSpanElement>(null);
  const timer = useRef<number | undefined>(undefined);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const descId = useId();
  const busy = props.busy === true;
  const reason = props.disabled === true && !busy && props.disabledReason !== undefined ? props.disabledReason : "";
  // A disabled control with a reason stays focusable (aria-disabled) so the keyboard and screen reader users can read the reason.
  const soft = reason !== "";
  const off = busy || (props.disabled === true && !soft);
  // The tooltip words that differ from the name are also given to screen readers, which do not read the hidden tooltip.
  const desc = soft ? reason : props.tipText !== undefined && props.tipText !== props.label ? props.tipText : "";
  const text = soft ? `${props.label}. ${reason}` : (props.tipText ?? props.label);
  const open = !dismissed && (hovered || focused || props.note === true);

  // The tooltip is not laid out while hidden (it adds no scroll space to the lists around it); it is measured after it shows,
  // before the browser paints.
  useLayoutEffect(() => {
    const t = tip.current;
    if (!open || t === null || t.parentElement === null) {
      return;
    }
    t.removeAttribute("data-flip");
    t.style.setProperty("--ia-dx", "0px");
    const { dx, flip } = fitTip(t.getBoundingClientRect(), clipBox(t.parentElement), place);
    t.style.setProperty("--ia-dx", `${dx}px`);
    if (flip) {
      t.setAttribute("data-flip", "");
    }
  }, [open, place, text]);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const enter = () => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      setDismissed(false);
      setHovered(true);
    }, TIP_DELAY_MS);
  };
  const leave = () => {
    window.clearTimeout(timer.current);
    setHovered(false);
  };
  const focus = (e: FocusEvent<HTMLButtonElement>) => {
    // A click focuses the button too; the tooltip follows the keyboard only (the hover covers the pointer).
    if (e.currentTarget.matches(":focus-visible")) {
      setDismissed(false);
      setFocused(true);
    }
  };
  // Esc hides the tooltip without moving the focus or the pointer (WCAG 1.4.13); the key still reaches the parent.
  const key = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key === "Escape") {
      setDismissed(true);
    }
  };
  const click = (e: MouseEvent<HTMLButtonElement>) => {
    if (soft) {
      e.preventDefault();
      return;
    }
    props.onClick(e);
  };
  const down = (e: MouseEvent<HTMLButtonElement>) => {
    if (!soft) {
      props.onMouseDown?.(e);
    }
  };

  const cls = ["ia", props.variant === "danger" ? "ia-danger" : "", props.size === "sm" ? "ia-sm" : "", props.note ? "ia-note" : "", props.className ?? ""]
    .filter((c) => c !== "")
    .join(" ");
  return (
    <button
      type="button"
      id={props.id}
      ref={props.ref}
      role={props.role}
      tabIndex={props.tabIndex}
      className={cls}
      data-place={place}
      data-open={open ? "" : undefined}
      aria-label={props.label}
      aria-expanded={props.expanded}
      aria-busy={busy ? true : undefined}
      aria-disabled={soft ? true : undefined}
      aria-describedby={desc !== "" ? descId : undefined}
      {...dataAttributes(props.data)}
      disabled={off}
      onClick={click}
      onMouseDown={down}
      onMouseEnter={enter}
      onMouseLeave={leave}
      onFocus={focus}
      onBlur={() => setFocused(false)}
      onKeyDown={key}
    >
      {busy ? <span className="ia-spin" aria-hidden="true" /> : <Icon name={props.icon} />}
      <span ref={tip} className="ia-tip" aria-hidden="true">
        {text}
      </span>
      {desc !== "" && (
        <span id={descId} className="ia-vh">
          {desc}
        </span>
      )}
    </button>
  );
}
