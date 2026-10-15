import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled page (test/list.test.mjs renders PreviewView).
import { callByName } from "../bindings/runtime.js";
import { scheduleApi, type ImportPreview } from "../bindings/schedule.js";
import { CollapsibleList } from "../CollapsibleList.js";
import { allCollapsed, type ListStates, listTexts, setCollapsed, stateOf, withState } from "../listView.js";
import {
  canApply,
  changeParts,
  commitErrorText,
  conflictParts,
  csvTemplate,
  errorText,
  groupTitles,
  hasChanges,
  lessonParts,
  lineParts,
  notes,
  type Part,
  type PreviewGroup,
  previewGroups,
  resultText,
  rowErrorParts,
  summaryChips,
  windowText,
} from "../scheduleView.js";

// Schedule import (PRD section 11.2): choose a file, see its errors or the preview, then apply it. Every text from the
// file is rendered as plain text, each piece in its own <bdi>.
export function SchedulePage() {
  const api = useMemo(() => scheduleApi(callByName), []);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  // Kept here, by group, so choosing the file again keeps what the user folded.
  const [lists, setLists] = useState<ListStates>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const mounted = useRef(true);
  const running = useRef(false); // a double click must not start a second request before busy renders
  // The id of the shown preview, discarded in the core when the page closes. An id the core has dropped already (after
  // a commit or an error) is ignored there.
  const selection = useRef("");

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      discardSelection();
    };
  }, []);

  function show(p: ImportPreview) {
    selection.current = p.selectionId;
    setPreview(p);
  }

  function discardSelection() {
    const id = selection.current;
    selection.current = "";
    if (id !== "") {
      void api.discard(id).catch(() => undefined); // nothing to report: the preview is gone either way
    }
  }

  async function run(action: () => Promise<void>) {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
    setDone("");
    try {
      await action();
    } finally {
      running.current = false;
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  const choose = () =>
    run(async () => {
      try {
        const p = await api.chooseFile();
        if (p.cancelled) {
          return; // a closed dialog keeps the current preview
        }
        if (!mounted.current) {
          void api.discard(p.selectionId).catch(() => undefined); // the page closed while the dialog was open
          return;
        }
        show(p);
      } catch (err) {
        discardSelection(); // the preview goes away with the error
        if (mounted.current) {
          setPreview(null);
          setError(errorText(err));
        }
      }
    });

  const apply = () => {
    const p = preview;
    if (p === null || !canApply(p)) {
      return;
    }
    return run(async () => {
      // The core drops the selection after any commit outcome. Forget it now, or closing the page mid-commit would send
      // a discard that can reach the core first and cancel the commit.
      selection.current = "";
      try {
        const r = await api.commit(p.selectionId);
        if (mounted.current) {
          setDone(resultText(r));
        }
      } catch (err) {
        if (mounted.current) {
          setError(commitErrorText(err));
        }
      } finally {
        // The core drops the selection after any commit, so the preview is no longer valid.
        if (mounted.current) {
          setPreview(null);
        }
      }
    });
  };

  const cancel = () => {
    discardSelection();
    setPreview(null);
    setError("");
  };

  return (
    <section>
      <h1>Расписание</h1>
      <p className="muted">{notes.formats}</p>
      <div className="actions">
        <button type="button" disabled={busy} onClick={() => void choose()}>
          Выбрать файл…
        </button>
      </div>

      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {done && <p role="status">{done}</p>}
      {preview !== null && (
        <PreviewView
          preview={preview}
          busy={busy}
          lists={lists}
          onLists={setLists}
          onApply={() => void apply()}
          onCancel={cancel}
        />
      )}

      <h2>Формат файла</h2>
      <p className="muted">{notes.allDay}</p>
      <p className="muted">{notes.uid}</p>
      <pre className="template">{csvTemplate}</pre>
      <p className="muted">{notes.noNotifications}</p>
    </section>
  );
}

export function PreviewView(props: {
  preview: ImportPreview;
  busy: boolean;
  lists: ListStates;
  onLists: (next: ListStates) => void;
  onApply: () => void;
  onCancel: () => void;
}) {
  const p = props.preview;
  const groups = previewGroups(p);
  const folded = allCollapsed(props.lists, groups);

  function group<T>(
    kind: PreviewGroup,
    items: readonly T[],
    parts: (item: T) => readonly Part[],
    extra: { more?: (hidden: number) => string; warning?: boolean; before?: ReactNode; after?: ReactNode } = {},
  ) {
    if (!groups.includes(kind)) {
      return null;
    }
    return (
      <CollapsibleList
        title={groupTitles[kind]}
        items={items}
        itemKey={(_, i) => i}
        renderItem={(item) => (
          <li>
            <Text parts={parts(item)} />
          </li>
        )}
        state={stateOf(props.lists, kind)}
        onStateChange={(s) => props.onLists(withState(props.lists, kind, s))}
        labels={{ more: extra.more }}
        before={extra.before}
        after={extra.after}
        classNames={{ body: extra.warning ? "rows warning" : "rows" }}
      />
    );
  }

  return (
    <div className="preview">
      <h2>
        Файл: <bdi>{p.fileName}</bdi>
      </h2>
      <ul className="chips">
        {summaryChips(p).map((c) => (
          <li key={c.kind} className={c.kind === "errors" && c.count > 0 ? "error" : undefined}>
            {c.text}
          </li>
        ))}
      </ul>

      {p.errors.length > 0 ? (
        <p className="error">{notes.rowErrors}</p>
      ) : (
        <>
          {p.window !== null && <p className="muted">{windowText(p.window)}</p>}
          {!hasChanges(p) && <p>{notes.noChanges}</p>}
        </>
      )}

      {groups.length > 0 && (
        <div className="actions">
          <button type="button" onClick={() => props.onLists(setCollapsed(props.lists, groups, !folded))}>
            {folded ? listTexts.expandAll : listTexts.collapseAll}
          </button>
        </div>
      )}
      {group("errors", p.errors, rowErrorParts, { more: () => listTexts.allErrors })}
      {group("added", p.added, (a) => lineParts(a.line, lessonParts(a)))}
      {group("changed", p.changed, changeParts)}
      {group("removed", p.removed, lessonParts)}
      {group("conflicts", p.conflicts, conflictParts, {
        warning: true,
        before: <p className="warning">{notes.conflicts}</p>,
        after: p.conflictsTruncated && <p className="warning">{notes.conflictsTruncated}</p>,
      })}

      {/* Pinned to the window edge: Apply stays visible below a long preview. */}
      <div className="actions sticky-actions">
        {canApply(p) && (
          <button type="button" disabled={props.busy} onClick={props.onApply}>
            Применить
          </button>
        )}
        <button type="button" disabled={props.busy} onClick={props.onCancel}>
          Отмена
        </button>
      </div>
    </div>
  );
}

// Text renders parts as plain text; a piece from the file goes into its own <bdi>, isolated from the rest of the line.
function Text(props: { parts: readonly Part[] }) {
  return (
    <>
      {props.parts.map((p, i) => (p.file ? <bdi key={i}>{p.text}</bdi> : <span key={i}>{p.text}</span>))}
    </>
  );
}
