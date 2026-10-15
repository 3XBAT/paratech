import { useEffect, useMemo, useRef, useState } from "react";
import { callByName } from "../bindings/runtime";
import { scheduleApi, type ImportPreview } from "../bindings/schedule";
import {
  canApply,
  changeParts,
  commitErrorText,
  conflictParts,
  csvTemplate,
  errorText,
  hasChanges,
  lessonParts,
  lineParts,
  notes,
  type Part,
  resultText,
  rowErrorParts,
  summaryText,
  windowText,
} from "../scheduleView";

// Schedule import (PRD section 11.2): choose a file, see its errors or the preview, then apply it. Every text from the
// file is rendered as plain text, each piece in its own <bdi>.
export function SchedulePage() {
  const api = useMemo(() => scheduleApi(callByName), []);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
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
        <PreviewView preview={preview} busy={busy} onApply={() => void apply()} onCancel={cancel} />
      )}

      <h2>Формат файла</h2>
      <p className="muted">{notes.allDay}</p>
      <p className="muted">{notes.uid}</p>
      <pre className="template">{csvTemplate}</pre>
      <p className="muted">{notes.noNotifications}</p>
    </section>
  );
}

function PreviewView(props: { preview: ImportPreview; busy: boolean; onApply: () => void; onCancel: () => void }) {
  const p = props.preview;
  const errors = p.errors.length > 0;

  return (
    <div className="preview">
      <h2>
        Файл: <bdi>{p.fileName}</bdi>
      </h2>

      {errors ? (
        <>
          <p className="error">{notes.rowErrors}</p>
          <ul className="rows">
            {p.errors.map((e, i) => (
              <li key={i}>
                <Text parts={rowErrorParts(e)} />
              </li>
            ))}
          </ul>
        </>
      ) : (
        <>
          {p.window !== null && <p className="muted">{windowText(p.window)}</p>}
          <p>{hasChanges(p) ? summaryText(p) : notes.noChanges}</p>

          {p.added.length > 0 && (
            <>
              <h3>Добавятся ({p.added.length})</h3>
              <ul className="rows">
                {p.added.map((a, i) => (
                  <li key={i}>
                    <Text parts={lineParts(a.line, lessonParts(a))} />
                  </li>
                ))}
              </ul>
            </>
          )}
          {p.changed.length > 0 && (
            <>
              <h3>Изменятся ({p.changed.length})</h3>
              <ul className="rows">
                {p.changed.map((c, i) => (
                  <li key={i}>
                    <Text parts={changeParts(c)} />
                  </li>
                ))}
              </ul>
            </>
          )}
          {p.removed.length > 0 && (
            <>
              <h3>Удалятся ({p.removed.length})</h3>
              <ul className="rows">
                {p.removed.map((r, i) => (
                  <li key={i}>
                    <Text parts={lessonParts(r)} />
                  </li>
                ))}
              </ul>
            </>
          )}
          {p.conflicts.length > 0 && (
            <>
              <h3>Пересечения ({p.conflicts.length})</h3>
              <p className="warning">{notes.conflicts}</p>
              <ul className="rows warning">
                {p.conflicts.map((c, i) => (
                  <li key={i}>
                    <Text parts={conflictParts(c)} />
                  </li>
                ))}
              </ul>
              {p.conflictsTruncated && <p className="warning">{notes.conflictsTruncated}</p>}
            </>
          )}
        </>
      )}

      <div className="actions">
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
