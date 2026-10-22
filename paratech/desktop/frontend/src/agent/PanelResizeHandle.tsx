import { type KeyboardEvent, type PointerEvent, useCallback, useEffect, useRef, useState } from "react";
import { dragPref, effectiveWidth, keyPref, maxWidth, type PanelPref, readPref, togglePref, writePref } from "./panelWidth.js";

function browserStore(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export interface ResizablePanel {
  pref: PanelPref;
  /** The width drawn now (the preference narrowed to the window). */
  width: number;
  viewport: number;
  dragging: boolean;
  /** Opens the panel at its last width (Ctrl+K on a closed panel). */
  reveal(): void;
  handle: {
    onPointerDown(e: PointerEvent<HTMLDivElement>): void;
    onPointerMove(e: PointerEvent<HTMLDivElement>): void;
    onPointerUp(e: PointerEvent<HTMLDivElement>): void;
    onKeyDown(e: KeyboardEvent<HTMLDivElement>): void;
    onDoubleClick(): void;
  };
}

/** Open state and width of the agent panel, kept between launches. */
export function useResizablePanel(): ResizablePanel {
  const [viewport, setViewport] = useState(() => window.innerWidth);
  const [pref, setPref] = useState<PanelPref>(() => readPref(browserStore(), window.innerWidth));
  const [dragging, setDragging] = useState(false);
  const live = useRef({ pref, viewport });
  live.current = { pref, viewport };

  useEffect(() => {
    // Only the viewport changes: the stored preference is narrowed when drawn, never rewritten.
    const onResize = () => setViewport(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  // A drag saves once, when it ends; every other change saves at once.
  useEffect(() => {
    if (!dragging) {
      writePref(browserStore(), pref);
    }
  }, [pref, dragging]);

  const reveal = useCallback(() => setPref((p) => (p.open ? p : { ...p, open: true })), []);

  const handle: ResizablePanel["handle"] = {
    onPointerDown: (e) => {
      if (e.button !== 0) {
        return;
      }
      e.currentTarget.setPointerCapture?.(e.pointerId);
      setDragging(true);
    },
    onPointerMove: (e) => {
      if (dragging) {
        setPref((p) => dragPref(p, e.clientX, live.current.viewport));
      }
    },
    onPointerUp: (e) => {
      if (dragging) {
        const el = e.currentTarget;
        if (el.hasPointerCapture?.(e.pointerId)) {
          el.releasePointerCapture?.(e.pointerId);
        }
        setDragging(false);
      }
    },
    onKeyDown: (e) => {
      const next = keyPref(live.current.pref, e.key, live.current.viewport);
      if (next !== null) {
        e.preventDefault();
        setPref(next);
      }
    },
    onDoubleClick: () => setPref(togglePref),
  };

  return { pref, width: effectiveWidth(pref, viewport), viewport, dragging, reveal, handle };
}

/** The strip on the left edge of the panel (on the window edge while closed): drag, arrow keys or double click. */
export function PanelResizeHandle(props: { panel: ResizablePanel }) {
  const { pref, viewport, handle } = props.panel;
  return (
    <div
      className="ag-resize"
      role="separator"
      aria-orientation="vertical"
      aria-label="Ширина панели помощника"
      aria-valuemin={0}
      aria-valuemax={maxWidth(viewport)}
      aria-valuenow={pref.open ? props.panel.width : 0}
      tabIndex={0}
      title="Потянуть, чтобы изменить ширину; двойной щелчок открывает и закрывает панель"
      onPointerDown={handle.onPointerDown}
      onPointerMove={handle.onPointerMove}
      onPointerUp={handle.onPointerUp}
      onPointerCancel={handle.onPointerUp}
      onLostPointerCapture={handle.onPointerUp}
      onKeyDown={handle.onKeyDown}
      onDoubleClick={handle.onDoubleClick}
    />
  );
}
