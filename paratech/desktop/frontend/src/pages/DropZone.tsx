import { useEffect, useRef, useState } from "react";
import { onEventData } from "../bindings/runtime.js";
import {
  type DropLine,
  dropTargetAttr,
  dropTexts,
  hasFiles,
  nextDepth,
  subscribeDropped,
} from "../dropFiles.js";
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
  const depth = useRef(0);
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
    const enter =(e: DragEvent) => {
      if (hasFiles(e.dataTransfer)) {
        e.preventDefault();
        depth.current = nextDepth(depth.current, "enter");
        setOver(true);
      }
    };
    const dragover = (e: DragEvent) => {
      // Always cancelled: otherwise the drop navigates the WebView to the file.
      e.preventDefault();
    };
    const leave = () => {
      depth.current = nextDepth(depth.current, "leave");
      setOver(depth.current > 0);
    };
    const drop = (e: DragEvent) => {
      // Cancelled only so the WebView does not navigate to the file; the real drop is handled in Go and arrives as
      // the "library:dropped" event below.
      e.preventDefault();
      depth.current = nextDepth(depth.current, "reset");
      setOver(false);
    };
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragover", dragover);
    window.addEventListener("dragleave", leave);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragenter", enter);
      window.removeEventListener("dragover", dragover);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("drop", drop);
    };
  }, []);

  useEffect(() => {
    let off: (() => void) | undefined;
    let closed = false;
    void subscribeDropped(onEventData, (outcome) => {
      depth.current = nextDepth(depth.current, "reset");
      setOver(false);
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
