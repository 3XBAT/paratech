import { useCallback, useEffect, useId, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled card.
import { type TurnkeyApi, type TurnkeyWork, turnkeyErrorText } from "../bindings/turnkey.js";
import {
  acceptWork,
  addItem,
  approvePlan,
  regenerateBlocked,
  rewriteWork,
  singleFlight,
  startPolling,
  type StepResult,
  checkEditor,
  type EditorCheck,
  type EditorItem,
  editorFromSections,
  isEdited,
  isRunning,
  limits,
  moveItem,
  noteOf,
  removeItem,
  reviseOptions,
  toOutlineItems,
  workCardModel,
  workTexts,
} from "../workView.js";
import { Button, IconButton } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

// Topic, titles, paragraphs, notes and sources reach the page as React text nodes: React escapes them, so markup in
// them stays visible text. No element, link or markdown is built from them, and nothing is kept in browser storage.

function OutlineEditor(props: { items: EditorItem[]; check: EditorCheck; disabled: boolean; onChange(items: EditorItem[]): void }) {
  const { items, check, disabled } = props;
  const set = (i: number, patch: Partial<EditorItem>) => props.onChange(items.map((it, j) => (j === i ? { ...it, ...patch } : it)));
  return (
    <div className="ag-wk-editor">
      {items.map((it, i) => (
        <div key={i} className="ag-wk-row">
          <span className="ag-wk-n small muted">{i + 1}</span>
          <input
            className="ag-inp ag-wk-title"
            aria-label={workTexts.titleLabel}
            aria-invalid={check.badTitles[i] || undefined}
            value={it.title}
            maxLength={limits.titleMax}
            disabled={disabled}
            onChange={(e) => set(i, { title: e.target.value })}
          />
          <input
            className="ag-inp ag-wk-words"
            type="number"
            min={limits.minWords}
            max={limits.maxWords}
            step={1}
            aria-label={workTexts.wordsLabel}
            aria-invalid={check.badWords[i] || undefined}
            value={it.words}
            disabled={disabled}
            onChange={(e) => set(i, { words: e.target.value })}
          />
          <IconButton icon="up" label={workTexts.moveUp} disabled={disabled || i === 0} onClick={() => props.onChange(moveItem(items, i, -1))} />
          <IconButton icon="down" label={workTexts.moveDown} disabled={disabled || i === items.length - 1} onClick={() => props.onChange(moveItem(items, i, 1))} />
          <IconButton icon="x" label={workTexts.remove} disabled={disabled} onClick={() => props.onChange(removeItem(items, i))} />
        </div>
      ))}
      <div className="ag-actions">
        <Button size="sm" variant="ghost" icon="plus" disabled={disabled || items.length >= limits.maxSections} onClick={() => props.onChange(addItem(items))}>
          {workTexts.addSection}
        </Button>
      </div>
      {!check.ok && (
        <p className="small muted" role="status">
          {check.problem}
        </p>
      )}
    </div>
  );
}

type Mode = "view" | "edit" | "revise";

/**
 * The card of a work that was loaded. It owns the typed state (plan editor, note) and maps the buttons to the core; a
 * parent re-mounts it when the step changes. No hooks outside: the tests render it with a fake API.
 */
export function WorkBody(props: { work: TurnkeyWork; api: TurnkeyApi; onWork(w: TurnkeyWork): void }) {
  const { work, api } = props;
  const model = workCardModel(work);
  const sections = work.writing?.sections ?? [];
  const [items, setItems] = useState<EditorItem[]>(() => editorFromSections(sections));
  const [mode, setMode] = useState<Mode>("view");
  const [reviseNumber, setReviseNumber] = useState(0);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // One call at a time: the guard answers a second click before the state has disabled the buttons.
  const [flight] = useState(singleFlight);
  const noteId = useId();
  const selectId = useId();

  const check = checkEditor(items);
  const edited = isEdited(items, sections);

  const run = async (call: () => Promise<void>) => {
    await flight(async () => {
      setBusy(true);
      setError("");
      try {
        await call();
      } catch (e) {
        setError(turnkeyErrorText(e));
      } finally {
        setBusy(false);
      }
    });
  };
  const saved = (w: TurnkeyWork) => {
    props.onWork(w);
    setItems(editorFromSections(w.writing?.sections ?? []));
  };
  // Only the final reply is applied; after a failure the error stays on this card and the work is re-read.
  const step = async (result: Promise<StepResult>) => {
    const r = await result;
    if (r.ok) {
      props.onWork(r.work);
      return;
    }
    if (r.fresh !== null) {
      props.onWork(r.fresh);
    }
    setError(turnkeyErrorText(r.error));
  };

  const errorLine = error !== "" && (
    <ResultLine kind="fail" role="alert">
      {error}
    </ResultLine>
  );

  switch (model.type) {
    case "none":
      return null;

    case "start":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          <p>{model.hint}</p>
          {errorLine}
          <div className="ag-actions">
            <Button size="sm" variant="primary" disabled={busy} onClick={() => void run(async () => props.onWork(await api.startWriting(work.id)))}>
              {model.button}
            </Button>
          </div>
        </section>
      );

    case "running":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">
            <span className="ag-dot" aria-hidden="true" />
            {model.caption}
          </h3>
          <p role="status">{model.line}</p>
        </section>
      );

    case "approve":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          <p className="small muted">{model.hint}</p>
          <OutlineEditor items={items} check={check} disabled={busy} onChange={setItems} />
          {errorLine}
          <div className="ag-actions">
            <Button
              size="sm"
              variant="primary"
              disabled={busy || !check.ok}
              onClick={() => void run(() => step(approvePlan(api, work.id, items, edited)))}
            >
              {workTexts.approve}
            </Button>
            <Button size="sm" variant="secondary" disabled={busy || !check.ok || !edited} onClick={() => void run(async () => saved(await api.editOutline(work.id, toOutlineItems(items))))}>
              {workTexts.savePlan}
            </Button>
          </div>
        </section>
      );

    case "blocked":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          <p>{model.text}</p>
          <label className="small muted" htmlFor={noteId}>
            {model.noteLabel}
          </label>
          <input id={noteId} className="ag-inp" value={note} maxLength={limits.noteMax} disabled={busy} onChange={(e) => setNote(e.target.value)} />
          {errorLine}
          <div className="ag-actions">
            <Button size="sm" variant="primary" disabled={busy} onClick={() => void run(async () => props.onWork(await regenerateBlocked(api, work, note)))}>
              {model.button}
            </Button>
          </div>
        </section>
      );

    case "done":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          <p className="ag-file">
            <Icon name="doc" />
            <span className="ag-file-kind">{model.file.kind}</span>
            <span className="ag-file-name">{model.file.name}</span>
          </p>
          <p className="ag-note">
            <Icon name="info" />
            <span>{model.notice}</span>
          </p>
          {model.sections.map((s) => (
            <details key={s.key} className="ag-wk-sec">
              <summary>{s.heading}</summary>
              {s.paragraphs.map((p, i) => (
                <p key={i} className="ag-text">
                  {p}
                </p>
              ))}
              {s.sources.length > 0 && (
                <>
                  <p className="ag-sub">{workTexts.sources}</p>
                  <ul className="ag-list small">
                    {s.sources.map((r, i) => (
                      <li key={i}>
                        {r.text}
                        {r.verify && <span className="ag-wk-verify"> · {workTexts.verify}</span>}
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </details>
          ))}

          {mode === "revise" && (
            <div className="ag-revise">
              <label className="small muted" htmlFor={selectId}>
                {workTexts.reviseSection}
              </label>
              <select id={selectId} className="ag-inp" value={reviseNumber} disabled={busy} onChange={(e) => setReviseNumber(Number(e.target.value))}>
                {reviseOptions(sections).map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
              <label className="small muted" htmlFor={noteId}>
                {workTexts.noteLabel}
              </label>
              <input id={noteId} className="ag-inp" value={note} maxLength={limits.noteMax} disabled={busy} onChange={(e) => setNote(e.target.value)} />
            </div>
          )}
          {mode === "edit" && <OutlineEditor items={items} check={check} disabled={busy} onChange={setItems} />}
          {errorLine}

          <div className="ag-actions">
            {mode === "view" && (
              <>
                <Button size="sm" variant="primary" disabled={busy} onClick={() => setMode("revise")}>
                  {workTexts.revise}
                </Button>
                <Button size="sm" variant="secondary" disabled={busy} onClick={() => setMode("edit")}>
                  {workTexts.editPlan}
                </Button>
              </>
            )}
            {mode === "revise" && (
              <>
                <Button size="sm" variant="primary" disabled={busy} onClick={() => void run(async () => props.onWork(await api.regenerate(work.id, reviseNumber, noteOf(note))))}>
                  {workTexts.regenerate}
                </Button>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setMode("view")}>
                  {workTexts.cancel}
                </Button>
              </>
            )}
            {mode === "edit" && (
              <>
                <Button
                  size="sm"
                  variant="primary"
                  disabled={busy || !check.ok || !edited}
                  onClick={() => void run(() => step(rewriteWork(api, work.id, items, edited)))}
                >
                  {workTexts.rewrite}
                </Button>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setMode("view")}>
                  {workTexts.cancel}
                </Button>
              </>
            )}
          </div>
        </section>
      );
  }
}

/** Loads the work of a `work` card, re-reads it while a step runs, and shows the card of its step. */
export function WorkWritingCard(props: { api: TurnkeyApi; workId: string }) {
  const { api, workId } = props;
  const [work, setWork] = useState<TurnkeyWork | null>(null);
  const [loadError, setLoadError] = useState("");
  const latest = useRef<TurnkeyWork | null>(null);

  // An older reply never replaces a newer one.
  // A reply that came (a read or a call) clears an earlier read error.
  const apply = useCallback((w: TurnkeyWork) => {
    latest.current = acceptWork(latest.current, w);
    setWork(latest.current);
    setLoadError("");
  }, []);

  const running = isRunning(work);
  useEffect(
    () =>
      startPolling({
        read: () => api.work(workId),
        onWork: apply,
        onError: setLoadError,
        current: () => latest.current,
      }),
    [api, workId, running, apply],
  );

  return (
    <>
      {work !== null && <WorkBody key={`${work.writing?.step ?? "-"}:${work.writing?.generation ?? 0}`} work={work} api={api} onWork={apply} />}
      {loadError !== "" && (
        <p className="small muted" role="status">
          {loadError}
        </p>
      )}
    </>
  );
}
