import { useEffect, useMemo, useRef, useState } from "react";
import { callByName } from "../bindings/runtime";
import { scheduleApi, type ImportPreview } from "../bindings/schedule";
import {
  canApply,
  commitErrorText,
  conflictText,
  csvTemplate,
  errorText,
  hasChanges,
  lessonText,
  lineText,
  notes,
  resultText,
  rowErrorText,
  summaryText,
  windowText,
} from "../scheduleView";

// Schedule import (PRD section 11.2): choose a file, see its errors or the preview, then apply it. Every text from the
// file is rendered as plain text.
export function SchedulePage() {
  const api = useMemo(() => scheduleApi(callByName), []);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const mounted = useRef(true);
  const running = useRef(false); // a double click must not start a second request before busy renders

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function run(action: () => Promise<void>) {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
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
        if (!mounted.current || p.cancelled) {
          return; // a closed dialog keeps the current preview
        }
        setPreview(p);
        setDone("");
      } catch (err) {
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
    if (preview !== null) {
      void api.discard(preview.selectionId).catch(() => undefined); // nothing to report: the preview is gone either way
    }
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
      <h2>Файл: {p.fileName}</h2>

      {errors ? (
        <>
          <p className="error">{notes.rowErrors}</p>
          <ul className="rows">
            {p.errors.map((e, i) => (
              <li key={i}>{rowErrorText(e)}</li>
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
                  <li key={i}>{lineText(a.line, lessonText(a))}</li>
                ))}
              </ul>
            </>
          )}
          {p.changed.length > 0 && (
            <>
              <h3>Изменятся ({p.changed.length})</h3>
              <ul className="rows">
                {p.changed.map((c, i) => (
                  <li key={i}>{lineText(c.line, `${lessonText(c.before)} → ${lessonText(c.after)}`)}</li>
                ))}
              </ul>
            </>
          )}
          {p.removed.length > 0 && (
            <>
              <h3>Удалятся ({p.removed.length})</h3>
              <ul className="rows">
                {p.removed.map((r, i) => (
                  <li key={i}>{lessonText(r)}</li>
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
                  <li key={i}>{conflictText(c)}</li>
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
