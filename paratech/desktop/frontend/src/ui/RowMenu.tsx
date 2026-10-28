import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
// The .js extension lets the Node test runner load the compiled menu (test/recordingrow.test.mjs renders it).
import { menuKey, menuShown } from "./menu.js";

export interface RowMenuItem<K extends string> {
  key: K;
  label: string;
  disabled?: boolean;
  /** Why a disabled item is off, shown under it. */
  hint?: string;
}

// The «⋯» menu of a row: Enter/Space or ArrowDown opens it, arrows move, Escape closes it and focuses the button again,
// Tab or a click outside closes it. Without items the cell stays, so the columns keep aligned.
export function RowMenu<K extends string>(props: {
  /** The accessible name: «Ещё действия: <row>». */
  label: string;
  /** The tooltip of the «⋯» button. */
  title: string;
  items: readonly RowMenuItem<K>[];
  onPick: (key: K) => void;
  /** Opens the menu each time the number grows (a right click on the row); 0 or undefined never does. */
  openRequest?: number;
}) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const menuId = useId();
  const count = props.items.length;
  const shown = menuShown(open, count);

  const enabled = () =>
    Array.from(wrap.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? []);

  useEffect(() => {
    if (count === 0) {
      setOpen(false);
    }
  }, [count]);

  const request = props.openRequest ?? 0;
  useEffect(() => {
    if (request > 0) {
      setOpen(true);
    }
  }, [request]);

  useEffect(() => {
    if (!shown) {
      return;
    }
    enabled()[0]?.focus();
    const outside = (e: PointerEvent) => {
      if (wrap.current !== null && !wrap.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [shown]);

  if (count === 0) {
    return <div className="mmenu" />;
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!open) {
      if (e.key === "ArrowDown" && e.target === button.current) {
        e.preventDefault();
        setOpen(true);
      }
      return;
    }
    const list = enabled();
    const next = menuKey(e.key, list.indexOf(document.activeElement as HTMLButtonElement), list.length);
    if (next === null) {
      return;
    }
    if (next === "leave") {
      setOpen(false); // the focus moves on by itself
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    if (next === "close") {
      setOpen(false);
      button.current?.focus();
      return;
    }
    list[next]?.focus();
  };

  return (
    <div className="mmenu" ref={wrap} onKeyDown={onKeyDown}>
      <button
        ref={button}
        type="button"
        className="btn ghost mmenu-btn"
        aria-label={props.label}
        title={props.title}
        aria-haspopup="menu"
        aria-expanded={shown}
        aria-controls={shown ? menuId : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        ⋯
      </button>
      {shown && (
        <div className="mmenu-list" id={menuId} role="menu" aria-label={props.label}>
          {props.items.map((i) => (
            <MenuEntry
              key={i.key}
              item={i}
              onPick={() => {
                setOpen(false);
                button.current?.focus();
                props.onPick(i.key);
              }}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function MenuEntry<K extends string>(props: { item: RowMenuItem<K>; onPick: () => void }) {
  const i = props.item;
  const hintId = useId();
  const hint = i.disabled && i.hint ? i.hint : "";
  return (
    <>
      <button
        type="button"
        role="menuitem"
        className="mmenu-item"
        tabIndex={-1}
        disabled={i.disabled}
        aria-describedby={hint ? hintId : undefined}
        onClick={props.onPick}
      >
        {i.label}
      </button>
      {hint && (
        <span id={hintId} className="small muted mmenu-hint">
          {hint}
        </span>
      )}
    </>
  );
}
