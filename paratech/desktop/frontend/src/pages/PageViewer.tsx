import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import {
  ownNotesTexts as t,
  type PhotoPage,
  stepPage,
  type ViewerKey,
  viewerAnnounce,
} from "../ownNotesView.js";
import { Button } from "../ui/Button.js";

// Full-size viewer of the photo pages of one note (TASK-166). The image comes only from the page's core address.
// A modal dialog: focus moves in, Tab stays inside, Esc closes and focus goes back to where it was.

const pagingKeys: readonly string[] = ["ArrowLeft", "ArrowRight", "Home", "End"];

/** `opener` is the element that was clicked to open the viewer; focus returns to it on close. */
export function PageViewer(props: { pages: PhotoPage[]; start: number; opener?: HTMLElement | null; onClose(): void }) {
  const { pages } = props;
  const [index, setIndex] = useState(() => Math.max(0, Math.min(props.start, pages.length - 1)));
  const root = useRef<HTMLDivElement>(null);
  const opener = useRef(props.opener ?? null);

  useEffect(() => {
    root.current?.querySelector<HTMLButtonElement>("button[data-close]")?.focus();
    const from = opener.current;
    return () => {
      if (from instanceof HTMLElement && from.isConnected) {
        from.focus();
      }
    };
  }, []);

  const total = pages.length;
  const at = Math.min(index, total - 1);

  // A button that turned off under the focus (the first or the last page) hands the focus to «Закрыть».
  useEffect(() => {
    const el = root.current;
    const now = document.activeElement;
    if (el !== null && (!el.contains(now) || (now instanceof HTMLButtonElement && now.disabled))) {
      el.querySelector<HTMLButtonElement>("button[data-close]")?.focus();
    }
  }, [at]);

  const page = pages[at];
  if (page === undefined) {
    return null;
  }
  const go = (key: ViewerKey) => setIndex(stepPage(at, total, key));

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      props.onClose();
      return;
    }
    if (pagingKeys.includes(e.key)) {
      e.preventDefault();
      go(e.key as ViewerKey);
      return;
    }
    if (e.key === "Tab") {
      const items = Array.from(root.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
      if (items.length === 0) {
        e.preventDefault();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      const now = document.activeElement;
      if (e.shiftKey && (now === first || !root.current?.contains(now))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && (now === last || !root.current?.contains(now))) {
        e.preventDefault();
        first.focus();
      }
    }
  }

  return (
    <div
      ref={root}
      className="page-viewer"
      role="dialog"
      aria-modal="true"
      aria-label={t.viewerDialog}
      onKeyDown={onKeyDown}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) {
          props.onClose();
        }
      }}
    >
      <div className="page-viewer-bar">
        <span className="page-viewer-count" role="status" aria-live="polite">
          {viewerAnnounce(page.number, total)}
        </span>
        <span className="page-viewer-acts">
          <Button variant="secondary" size="sm" disabled={at === 0} onClick={() => go("ArrowLeft")}>
            {t.viewerPrev}
          </Button>
          <Button variant="secondary" size="sm" disabled={at === total - 1} onClick={() => go("ArrowRight")}>
            {t.viewerNext}
          </Button>
          <Button variant="primary" size="sm" data-close="" onClick={props.onClose}>
            {t.viewerClose}
          </Button>
        </span>
      </div>
      <div className="page-viewer-stage">
        <img className="page-viewer-img" src={page.src} alt={page.alt} />
      </div>
    </div>
  );
}
