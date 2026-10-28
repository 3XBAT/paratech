import { useEffect, useRef, useState } from "react";
import { onEventData } from "../bindings/runtime.js";
import { type DropLine, dropTargetAttr, dropTexts, subscribeDropped, windowDragHandlers } from "../dropFiles.js";
import { Icon } from "../ui/Icon.js";

/** The report of a drop: fixed texts, one line per file. */
export function DropReport(props: { lines: readonly DropLine[]; onClose(): void }) {
  if (props.lines.length === 0) {
    return null;
  }
  return (
    <div className="drop-report" role="status">
      <ul>
        {props.lines.map((l, i) => (
          <li key={i} className={l.ok ? "ok" : "fail"}>
            <Icon name={l.ok ? "ok" : "warn"} />
            <span>{l.text}</span>
          </li>
        ))}
      </ul>
      <button type="button" className="btn ghost sm" onClick={props.onClose}>
        {dropTexts.close}
      </button>
    </div>
  );
}

/**
 * A drop zone over the whole window: it highlights while files are dragged over, and a drop imports them. The page
 * never opens a dropped file itself (the WebView would navigate to it), so every drop is cancelled here.
 */
export function DropZone(props: { onImported?: () => void }) {
  const [over, setOver] = useState(false);
  const [lines, setLines] = useState<DropLine[]>([]);
  const drag = useRef<ReturnType<typeof windowDragHandlers> | null>(null);
  const done = useRef(props.onImported);
  done.current = props.onImported;

  useEffect(() => {
    // Wails hit-tests the pointer with elementFromPoint(..).closest("[data-file-drop-target]"): the target must be
    // always mounted and contain everything hit-testable, so it is body (html, body and #root fill the window), not
    // the pointer-events:none overlay that exists only during a drag.
    document.body.setAttribute(dropTargetAttr, "");
    return () => document.body.removeAttribute(dropTargetAttr);
  }, []);

  useEffect(() => {
    const h = windowDragHandlers(setOver);
    drag.current = h;
    window.addEventListener("dragenter", h.enter);
    window.addEventListener("dragover", h.dragover);
    window.addEventListener("dragleave", h.leave);
    window.addEventListener("drop", h.drop);
    return () => {
      window.removeEventListener("dragenter", h.enter);
      window.removeEventListener("dragover", h.dragover);
      window.removeEventListener("dragleave", h.leave);
      window.removeEventListener("drop", h.drop);
    };
  }, []);

  useEffect(() => {
    let off: (() => void) | undefined;
    let closed = false;
    void subscribeDropped(onEventData, (outcome) => {
      drag.current?.reset();
      setLines(outcome.lines);
      if (outcome.imported) {
        done.current?.();
      }
    }).then((stop) => {
      if (closed) {
        stop();
      } else {
        off = stop;
      }
    });
    return () => {
      closed = true;
      off?.();
    };
  }, []);

  return (
    <>
      {over && (
        <div className="drop-overlay" aria-hidden="true">
          <div className="drop-box">
            <Icon name="down" />
            <p>{dropTexts.overlay}</p>
          </div>
        </div>
      )}
      <DropReport lines={lines} onClose={() => setLines([])} />
    </>
  );
}
