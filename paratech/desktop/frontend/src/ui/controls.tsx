// Small presentational controls of the design system (docs/design/brief.md section 4). They hold no data or decisions:
// the pages pass what the core reports and get the user's input back. The .js extensions let the Node test runner load
// the compiled modules (test/list.test.mjs renders the schedule preview); TypeScript and Vite map them to the .tsx files.
import {
  type InputHTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
  type SelectHTMLAttributes,
  useEffect,
  useId,
  useRef,
} from "react";
import { Icon, type IconName } from "./Icon.js";

export interface SegmentedOption<T extends string> {
  value: T;
  label: ReactNode;
}

/** A row of mutually exclusive buttons (screen parts, filters, file format, theme); `large` for screen parts. */
export function Segmented<T extends string>(props: {
  label: string;
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
  large?: boolean;
}) {
  return (
    <div className={props.large ? "seg lg" : "seg"} role="group" aria-label={props.label}>
      {props.options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === props.value} onClick={() => props.onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** The option an arrow, Home or End key moves to from value (wrapping); null for any other key. */
export function nextSegment<T extends string>(options: readonly SegmentedOption<T>[], value: T, key: string): T | null {
  const n = options.length;
  const i = Math.max(0, options.findIndex((o) => o.value === value));
  const to =
    key === "ArrowRight" ? (i + 1) % n : key === "ArrowLeft" ? (i - 1 + n) % n : key === "Home" ? 0 : key === "End" ? n - 1 : -1;
  return n === 0 || to < 0 ? null : options[to].value;
}

export const segmentTabId = (base: string, value: string) => `${base}-tab-${value}`;
export const segmentPanelId = (base: string, value: string) => `${base}-panel-${value}`;

/**
 * The large segmented control that switches the shown part of a screen: a tablist with one tab stop; arrows, Home and
 * End move between the parts. The page renders the chosen part in a tabpanel with segmentPanelId(base, value).
 */
export function SegmentTabs<T extends string>(props: {
  label: string;
  /** The prefix of the tab and panel ids (useId). */
  base: string;
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
}) {
  const tabs = useRef(new Map<string, HTMLButtonElement>());
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const next = nextSegment(props.options, props.value, e.key);
    if (next === null) {
      return;
    }
    e.preventDefault();
    props.onChange(next);
    tabs.current.get(next)?.focus();
  };
  return (
    <div className="seg lg" role="tablist" aria-label={props.label} onKeyDown={onKey}>
      {props.options.map((o) => {
        const selected = o.value === props.value;
        return (
          <button
            key={o.value}
            ref={(el) => {
              if (el) {
                tabs.current.set(o.value, el);
              } else {
                tabs.current.delete(o.value);
              }
            }}
            type="button"
            role="tab"
            id={segmentTabId(props.base, o.value)}
            aria-selected={selected}
            aria-controls={selected ? segmentPanelId(props.base, o.value) : undefined}
            tabIndex={selected ? 0 : -1}
            onClick={() => props.onChange(o.value)}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

/** A thin progress bar; the number is repeated as text next to it, or by the caller nearby (text={false}). */
export function Progress(props: { value: number; label: string; thick?: boolean; text?: ReactNode | false }) {
  const v = Math.min(100, Math.max(0, Number.isFinite(props.value) ? props.value : 0));
  return (
    <div className="progress-wrap">
      <div
        className={props.thick ? "prog thick" : "prog"}
        role="progressbar"
        aria-label={props.label}
        aria-valuenow={v}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <b style={{ width: `${v}%` }} />
      </div>
      {props.text !== false && <span className="small muted num">{props.text ?? `${v} %`}</span>}
    </div>
  );
}

/** A labelled field: label above, hint and error below. */
export function Field(props: { label: ReactNode; hint?: ReactNode; error?: ReactNode; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="ui-field">
      <label htmlFor={id}>{props.label}</label>
      {props.children(id)}
      {props.hint && <span className="small muted">{props.hint}</span>}
      {props.error && (
        <span className="field-error small" role="alert">
          <Icon name="warn" />
          {props.error}
        </span>
      )}
    </div>
  );
}

export function Input(props: Omit<InputHTMLAttributes<HTMLInputElement>, "className">) {
  return <input className="input" {...props} />;
}

/** A native select with its own arrow. */
export function Select(props: Omit<SelectHTMLAttributes<HTMLSelectElement>, "className">) {
  return <select className="sel" {...props} />;
}

/** A checkbox shown as a switch; a disabled one says why next to it. */
export function Toggle(props: {
  label: ReactNode;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  hint?: ReactNode;
}) {
  const id = useId();
  const hintId = useId();
  return (
    <div className="set">
      <label htmlFor={id}>{props.label}</label>
      <input
        id={id}
        type="checkbox"
        role="switch"
        className="switch"
        checked={props.checked}
        disabled={props.disabled}
        aria-describedby={props.hint ? hintId : undefined}
        onChange={(e) => props.onChange(e.target.checked)}
      />
      {props.hint && (
        <span id={hintId} className="small muted">
          {props.hint}
        </span>
      )}
    </div>
  );
}

export type BannerKind = "wait" | "net" | "fail";

const bannerIcons: Readonly<Record<BannerKind, IconName>> = { wait: "warn", net: "wave", fail: "warn" };

/** A note above the content. It has no close button: it goes away when its reason does. */
export function Banner(props: { kind: BannerKind; role?: "status" | "alert"; children: ReactNode }) {
  return (
    <div className={`note s-${props.kind}`} role={props.role}>
      <Icon name={bannerIcons[props.kind]} />
      <div>{props.children}</div>
    </div>
  );
}

export type ResultKind = "ok" | "wait" | "fail";

const resultIcons: Readonly<Record<ResultKind, IconName>> = { ok: "ok", wait: "warn", fail: "warn" };

/** The outcome of an action: a line with an icon under its button, instead of a toast. */
export function ResultLine(props: { kind: ResultKind; role?: "status" | "alert"; title?: string; children: ReactNode }) {
  return (
    <p className={`result s-${props.kind}`} role={props.role} title={props.title || undefined}>
      <Icon name={resultIcons[props.kind]} />
      <span>{props.children}</span>
    </p>
  );
}

export function Card(props:{ children: ReactNode; className?: string; label?: string }) {
  return (
    <div className={props.className ? `card ${props.className}` : "card"} role={props.label ? "group" : undefined} aria-label={props.label}>
      {props.children}
    </div>
  );
}

/**
 * One line of a list: title and meta, status, actions; one column in a narrow window. Put rows in a `.card.rows` list:
 * the list owns the columns. `below` spans the whole row under them (a warning about the row).
 */
export function Row(props: {
  /** A control before the title (the selection checkbox): its own column, so the meta starts under the title. */
  lead?: ReactNode;
  title: ReactNode;
  meta?: ReactNode;
  status?: ReactNode;
  actions?: ReactNode;
  below?: ReactNode;
}) {
  return (
    <div className="row" role="listitem">
      <div className={props.lead ? "row-main row-lead" : "row-main"}>
        {props.lead && <span className="row-lead-box">{props.lead}</span>}
        <p className="t">{props.title}</p>
        {props.meta && <span className="small muted">{props.meta}</span>}
      </div>
      <div className="st">{props.status}</div>
      <div className="acts">{props.actions}</div>
      {props.below && <div className="row-below">{props.below}</div>}
    </div>
  );
}

/** First run and empty lists: a drawing on the left, the text and one or two actions on the right. */
export function EmptyState(props: { label: string; title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="empty">
      <svg viewBox="0 0 260 120" width="260" height="120" aria-hidden="true" className="empty-art">
        <rect x=".5" y=".5" width="259" height="119" rx="12" className="empty-frame" />
        <path d="M20 60h220" className="empty-axis" />
        <path d="M20 60h96l6-5 6 10 6-5h106" className="empty-wave" />
        <circle cx="226" cy="24" r="4" className="empty-dot" />
      </svg>
      <div>
        <span className="label">{props.label}</span>
        <h2>{props.title}</h2>
        <p className="empty-text">{props.children}</p>
        {props.actions && <div className="acts start">{props.actions}</div>}
      </div>
    </div>
  );
}

/** Static counters (the schedule import); a tile with zero errors shows its number in green. */
export function CountTiles(props: { label: string; tiles: readonly { key: string; label: string; count: number; className?: string }[] }) {
  return (
    <ul className="tiles" aria-label={props.label}>
      {props.tiles.map((t) => (
        <li key={t.key} className={t.className ? `tile ${t.className}` : "tile"}>
          <span className="label">{t.label}</span>
          <b className="num">{t.count}</b>
        </li>
      ))}
    </ul>
  );
}

/**
 * A modal for irreversible actions: native <dialog>, buttons named by their action, focus on the safe one.
 * Escape and the cancel button call onCancel.
 */
export function Dialog(props: {
  open: boolean;
  title: string;
  children: ReactNode;
  confirmLabel: string;
  cancelLabel: string;
  danger?: boolean;
  confirmDisabled?: boolean;
  /** While the confirmed action runs: the cancel button is off and Escape does nothing. */
  cancelDisabled?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const safe = useRef<HTMLButtonElement>(null);
  const titleId = useId();

  useEffect(() => {
    const d = ref.current;
    if (d === null) {
      return;
    }
    if (props.open && !d.open) {
      d.showModal();
      safe.current?.focus();
    } else if (!props.open && d.open) {
      d.close();
    }
  }, [props.open]);

  return (
    <dialog
      ref={ref}
      className="dialog"
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault();
        if (!props.cancelDisabled) {
          props.onCancel();
        }
      }}
    >
      <h2 id={titleId}>{props.title}</h2>
      <div className="dialog-body">{props.children}</div>
      <div className="acts">
        <button ref={safe} type="button" className="btn" disabled={props.cancelDisabled} onClick={props.onCancel}>
          {props.cancelLabel}
        </button>
        <button
          type="button"
          className={props.danger ? "btn danger" : "btn primary"}
          disabled={props.confirmDisabled}
          onClick={props.onConfirm}
        >
          {props.confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
