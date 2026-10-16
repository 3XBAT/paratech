// Small presentational controls of the design system (docs/design/brief.md section 4). They hold no data or decisions:
// the pages pass what the core reports and get the user's input back.
import {
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  useEffect,
  useId,
  useRef,
} from "react";
import { Icon, type IconName } from "./Icon";

export interface SegmentedOption<T extends string> {
  value: T;
  label: ReactNode;
}

/** A row of mutually exclusive buttons (filters, theme). */
export function Segmented<T extends string>(props: {
  label: string;
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
}) {
  return (
    <div className="seg" role="group" aria-label={props.label}>
      {props.options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === props.value} onClick={() => props.onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** A thin progress bar; the number is always repeated as text next to it. */
export function Progress(props: { value: number; label: string; thick?: boolean; text?: ReactNode }) {
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
      <span className="small muted num">{props.text ?? `${v} %`}</span>
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
        onChange={(e) => props.onChange(e.target.checked)}
      />
      {props.hint && <span className="small muted">{props.hint}</span>}
    </div>
  );
}

export function Radio(props: Omit<InputHTMLAttributes<HTMLInputElement>, "className" | "type">) {
  return <input type="radio" className="radio" {...props} />;
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

export function Card(props: { children: ReactNode; className?: string; label?: string }) {
  return (
    <div className={props.className ? `card ${props.className}` : "card"} role={props.label ? "group" : undefined} aria-label={props.label}>
      {props.children}
    </div>
  );
}

/** One line of a list: title and meta, status, actions; one column in a narrow window. */
export function Row(props: { title: ReactNode; meta?: ReactNode; status?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="row" role="listitem">
      <div className="row-main">
        <p className="t">{props.title}</p>
        {props.meta && <span className="small muted">{props.meta}</span>}
      </div>
      <div className="st">{props.status}</div>
      <div className="acts">{props.actions}</div>
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
        props.onCancel();
      }}
    >
      <h2 id={titleId}>{props.title}</h2>
      <div className="dialog-body">{props.children}</div>
      <div className="acts">
        <button ref={safe} type="button" className="btn" onClick={props.onCancel}>
          {props.cancelLabel}
        </button>
        <button type="button" className={props.danger ? "btn danger" : "btn primary"} onClick={props.onConfirm}>
          {props.confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
