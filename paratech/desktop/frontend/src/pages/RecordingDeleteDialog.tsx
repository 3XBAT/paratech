import { useEffect, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled dialog (test/recordingdelete.test.mjs renders it).
import type { DeletePreview, DeleteResult, RecordingDeleteApi } from "../bindings/recordingDelete.js";
import {
  type ConfirmAssignment,
  type DeleteChoice,
  type DeleteConfirmation,
  deleteConfirmation,
  deleteErrorText,
  deleteRequests,
  deleteTexts,
  initialDeleteChoice,
  withDraft,
} from "../recordingDeleteView.js";
import { Dialog, ResultLine } from "../ui/controls.js";

/**
 * The confirmation of deleting one or several recordings (TASK-095): one Preview of all ids, the user's choice of the
 * found homework, one Delete. The only call site of RecordingDeleteApi.delete. onDone gets the result; the dialog
 * cannot be closed while the deletion runs (it may take a while per recording).
 */
export function RecordingDeleteDialog(props: {
  api: RecordingDeleteApi;
  ids: readonly string[];
  onDone: (result: DeleteResult, preview: DeletePreview) => void;
  onCancel: () => void;
}) {
  const { api, ids } = props;
  const [preview, setPreview] = useState<DeletePreview | null>(null);
  const [choice, setChoice] = useState<DeleteChoice>(initialDeleteChoice);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const running = useRef(false);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // The ids are fixed for the life of the dialog: the page mounts a new one per deletion.
  const asked = useRef(ids);
  useEffect(() => {
    let stopped = false;
    api.preview(asked.current).then(
      (p) => !stopped && setPreview(p),
      (err) => !stopped && setError(deleteErrorText(err)),
    );
    return () => {
      stopped = true;
    };
  }, [api]);

  const model = deleteConfirmation(preview, ids.length);

  async function confirm() {
    if (running.current || preview === null || !model.canDelete) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
    try {
      const res = await api.delete(deleteRequests(preview, choice));
      if (mounted.current) {
        props.onDone(res, preview);
      }
    } catch (err) {
      if (mounted.current) {
        setError(deleteErrorText(err));
      }
    } finally {
      running.current = false;
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  return (
    <Dialog
      open
      title={model.title}
      confirmLabel={busy ? deleteTexts.busy : deleteTexts.confirm}
      cancelLabel={deleteTexts.cancel}
      danger
      confirmDisabled={busy || !model.canDelete}
      cancelDisabled={busy}
      onConfirm={() => void confirm()}
      onCancel={() => !running.current && props.onCancel()}
    >
      {preview === null && error === "" && <p>{deleteTexts.loading}</p>}
      {preview !== null && (
        <DeleteBody model={model} choice={choice} disabled={busy} onChoice={setChoice} />
      )}
      {error !== "" && (
        <ResultLine kind="fail" role="alert">
          {error}
        </ResultLine>
      )}
    </Dialog>
  );
}

/**
 * What the confirmation lists. Titles are imported text or model output: React renders them as text only.
 */
export function DeleteBody(props: {
  model: DeleteConfirmation;
  choice: DeleteChoice;
  disabled: boolean;
  onChoice: (c: DeleteChoice) => void;
}) {
  const { model: m, choice: c } = props;
  const several = m.recordings.length > 1;
  const from = (a: ConfirmAssignment) => (several ? <span className="muted"> · {a.recording}</span> : null);
  return (
    <div className="del-body">
      {m.recordings.map((r) => (
        <div key={r.id} className="del-rec">
          <p className="del-title">{r.title}</p>
          {r.refusal !== "" ? (
            <p className="small del-refusal">
              {deleteTexts.notDeleted} {r.refusal}
            </p>
          ) : r.deletes.length === 0 ? (
            <p className="small">{deleteTexts.nothingStored}</p>
          ) : (
            <>
              <p className="small">{deleteTexts.willDelete}</p>
              <ul className="small del-list">
                {r.deletes.map((d) => (
                  <li key={d}>{d}</li>
                ))}
              </ul>
            </>
          )}
        </div>
      ))}
      {m.missing !== "" && <p className="small">{m.missing}</p>}
      {m.canDelete && <p className="del-warn">{deleteTexts.irreversible}</p>}
      {m.plain.length > 0 && (
        <div className="del-group">
          <label className="del-check">
            <input
              type="checkbox"
              checked={c.plain}
              disabled={props.disabled}
              onChange={(e) => props.onChoice({ ...c, plain: e.target.checked })}
            />
            {m.plainLabel}
          </label>
          <ul className="small del-list">
            {m.plain.map((a) => (
              <li key={a.id}>
                {a.title}
                {from(a)}
              </li>
            ))}
          </ul>
        </div>
      )}
      {m.drafts.length > 0 && (
        <div className="del-group">
          <p className="small">{deleteTexts.drafts}</p>
          {m.drafts.map((a) => (
            <label key={a.id} className="del-check">
              <input
                type="checkbox"
                checked={c.drafts.has(a.id)}
                disabled={props.disabled}
                onChange={(e) => props.onChoice(withDraft(c, a.id, e.target.checked))}
              />
              <span>
                {a.title}
                {from(a)}
              </span>
            </label>
          ))}
        </div>
      )}
      {m.kept.length > 0 && (
        <div className="del-group">
          <p className="small">{deleteTexts.kept}</p>
          <ul className="small del-list">
            {m.kept.map((a) => (
              <li key={a.id}>
                {a.title}
                {from(a)}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
