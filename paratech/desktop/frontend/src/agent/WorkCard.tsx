import { useCallback, useEffect, useId, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled card.
import { type TurnkeyApi, type TurnkeyWork, turnkeyErrorText } from "../bindings/turnkey.js";
import {
  acceptWork,
  addItem,
  approvePlan,
  nextActionError,
  regenerateBlocked,
  retryWriting,
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
  canStop,
  nextStopping,
  openLabel,
  openTarget,
  openWork,
  stopWork,
  savePlanAt,
  retryOffered,
  metaLine,
  workMeta,
  type OpenView,
  workCardModel,
  workTexts,
} from "../workView.js";
import { type AgentConfirmApi, agentConfirmApi } from "../bindings/agentConfirm.js";
import { libraryApi, type LibraryApi } from "../bindings/library.js";
import { type RecordingDeleteApi, recordingDeleteApi } from "../bindings/recordingDelete.js";
import { callByName } from "../bindings/runtime.js";
import { canOfferSend } from "../sendWorkView.js";
import { WorkSend } from "./WorkSend.js";
import { triageReason, turnkeyCardModel } from "../turnkeyView.js";
import { FailedCard, HelperCard, QuestionsCard, TriageCard, UnderstandingCard } from "./TurnkeyCards.js";
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

const defaultLibrary = libraryApi(callByName);
const defaultSend = { api: agentConfirmApi(callByName), deleteApi: recordingDeleteApi(callByName) };

/** The calls that sending a ready work to the teacher needs; without them nothing about sending is shown. */
export interface SendApis {
  api: AgentConfirmApi;
  deleteApi: RecordingDeleteApi;
}

type Mode = "view" | "edit" | "revise";

/**
 * The card of a work that was loaded. It owns the typed state (plan editor, note) and maps the buttons to the core; a
 * parent re-mounts it when the step changes. No hooks outside: the tests render it with a fake API.
 */
export function WorkBody(props: {
  work: TurnkeyWork;
  api: TurnkeyApi;
  onWork(w: TurnkeyWork): void;
  /** The error of the last call and its setter live in the parent: this body is re-mounted when the step changes. */
  error?: string;
  onError?(text: string): void;
  /** «Открыть в Word / PowerPoint» needs it; without it the buttons are not shown. */
  library?: Pick<LibraryApi, "openMaterial">;
  /** A stop was requested and the job has not finished yet. */
  stopping?: boolean;
  /** The core accepted «Остановить»: the parent keeps reading the work until the job is gone. */
  onStopped?(): void;
  /** Sending a ready work to the teacher (see WorkSend); without it nothing about sending is shown. */
  send?: SendApis;
  /** Subject names by library id, for the subject line of the card. */
  subjects?: ReadonlyMap<string, string>;
}) {
  const { work, api } = props;
  const error = props.error ?? "";
  const setError = props.onError ?? (() => undefined);
  const model = workCardModel(work);
  const sections = work.writing?.sections ?? [];
  const [items, setItems] = useState<EditorItem[]>(() => editorFromSections(sections));
  const [mode, setMode] = useState<Mode>("view");
  const [reviseNumber, setReviseNumber] = useState(0);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [openView, setOpenView] = useState<OpenView | null>(null);
  // The last failure came from «Остановить»: «Повторить» (StartWriting) is no answer to it.
  const [stopFailed, setStopFailed] = useState(false);
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
      setOpenView(null);
      setStopFailed(false);
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

  // «Сохранить план»: a stale version shows the error, re-reads the card and drops the typed plan.
  const savePlan = async () => {
    const r = await savePlanAt(api, work.id, work.version, items);
    if (r.ok) {
      saved(r.work);
      return;
    }
    if (r.fresh !== null) {
      saved(r.fresh);
    }
    setError(turnkeyErrorText(r.error));
  };
  const stop = async () => {
    let r;
    try {
      r = await stopWork(api, work.id);
    } catch (e) {
      setStopFailed(true);
      throw e;
    }
    props.onWork(r.work);
    if (r.watch) {
      props.onStopped?.();
    }
  };
  const open = async () => {
    if (props.library !== undefined) {
      setOpenView(await openWork(props.library, work));
    }
  };
  const canOpen = props.library !== undefined && openTarget(work) !== "";

  const metaText = metaLine(workMeta(work, props.subjects ?? new Map()));
  const metaEl = metaText !== "" && <p className="small muted ag-text">{metaText}</p>;

  const errorLine = error !== "" && (
    <ResultLine kind="fail" role="alert">
      {error}
    </ResultLine>
  );

  // Questions, «Как понято задание» and the helper (TASK-130): works that have no writing yet.
  const early = turnkeyCardModel(work);
  if (early !== null) {
    const ctx = { work, api, busy, run, step, onWork: props.onWork, errorLine };
    switch (early.type) {
      case "triage":
        return <TriageCard ctx={ctx} caption={early.caption} line={early.line} problem={early.problem} />;
      case "failed":
        return <FailedCard caption={early.caption} text={early.text} message={triageReason(work)} />;
      case "questions":
        // A new round is a new form: the typed answers of the old one are dropped.
        return <QuestionsCard key={`${work.questionRound}:${work.version}`} ctx={ctx} />;
      case "understanding":
        return <UnderstandingCard key={work.version} ctx={ctx} />;
      case "helper":
        return <HelperCard ctx={ctx} />;
    }
  }

  switch (model.type) {
    case "none":
      return null;

    case "start":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          {metaEl}
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
          {metaEl}
          <p role="status">{model.line}</p>
          {errorLine}
          <div className="ag-actions">
            {retryOffered(error, stopFailed) && (
              <Button size="sm" variant="primary" disabled={busy} onClick={() => void run(async () => props.onWork(await retryWriting(api, work.id)))}>
                {workTexts.retry}
              </Button>
            )}
            {canStop(work) && (
              <Button size="sm" variant="secondary" disabled={busy || props.stopping === true} onClick={() => void run(stop)}>
                {props.stopping ? workTexts.stopping : workTexts.stop}
              </Button>
            )}
          </div>
        </section>
      );

    case "stopped":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          {metaEl}
          <p role="status">{model.line}</p>
          {errorLine}
          <div className="ag-actions">
            <Button size="sm" variant="primary" disabled={busy} onClick={() => void run(async () => props.onWork(await retryWriting(api, work.id)))}>
              {model.button}
            </Button>
          </div>
        </section>
      );

    case "approve":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          {metaEl}
          <p className="small muted">{model.hint}</p>
          <OutlineEditor items={items} check={check} disabled={busy} onChange={setItems} />
          {errorLine}
          <div className="ag-actions">
            <Button
              size="sm"
              variant="primary"
              disabled={busy || !check.ok}
              onClick={() => void run(() => step(approvePlan(api, work.id, work.version, items, edited)))}
            >
              {workTexts.approve}
            </Button>
            <Button size="sm" variant="secondary" disabled={busy || !check.ok || !edited} onClick={() => void run(savePlan)}>
              {workTexts.savePlan}
            </Button>
          </div>
        </section>
      );

    case "blocked":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{model.caption}</h3>
          {metaEl}
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
          {metaEl}
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
          {openView !== null && (
            <ResultLine kind={openView.kind} role={openView.kind === "fail" ? "alert" : "status"}>
              {openView.text}
            </ResultLine>
          )}

          {canOpen && mode === "view" && <p className="small muted">{workTexts.copyNote}</p>}

          <div className="ag-actions">
            {mode === "view" && (
              <>
                <Button size="sm" variant="primary" disabled={busy} onClick={() => setMode("revise")}>
                  {workTexts.revise}
                </Button>
                <Button size="sm" variant="secondary" disabled={busy} onClick={() => setMode("edit")}>
                  {workTexts.editPlan}
                </Button>
                {canOpen && (
                  <Button size="sm" variant="secondary" disabled={busy} onClick={() => void run(open)}>
                    {openLabel(work)}
                  </Button>
                )}
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
                  onClick={() => void run(() => step(rewriteWork(api, work.id, work.version, items, edited)))}
                >
                  {workTexts.rewrite}
                </Button>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setMode("view")}>
                  {workTexts.cancel}
                </Button>
              </>
            )}
          </div>
          {mode === "view" && props.send !== undefined && (
            <WorkSend api={props.send.api} deleteApi={props.send.deleteApi} workId={work.id} sendable={canOfferSend(work)} />
          )}
        </section>
      );
  }
}

/** Loads the work of a `work` card, re-reads it while a step runs, and shows the card of its step. */
// `initial`: the work as the call that made it replied (it may carry the problem of a reader call, which the core does not store).
export function WorkWritingCard(props: { api: TurnkeyApi; workId: string; library?: LibraryApi; send?: SendApis; initial?: TurnkeyWork }) {
  const { api, workId } = props;
  const library = props.library ?? defaultLibrary;
  const send = props.send ?? defaultSend;
  const [work, setWork] = useState<TurnkeyWork | null>(() => (props.initial !== undefined && props.initial.id === workId ? props.initial : null));
  const [stopping, setStopping] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [actionError, setActionError] = useState("");
  const latest = useRef<TurnkeyWork | null>(work);
  const [subjects, setSubjects] = useState<ReadonlyMap<string, string>>(() => new Map());

  // Subject names for the subject line: read once the work names a subject; a failed read just leaves «Входящие».
  const subjectId = work?.subjectId ?? "";
  useEffect(() => {
    if (subjectId === "") {
      return;
    }
    let live = true;
    library
      .subjects()
      .then((list) => {
        if (live) {
          setSubjects(new Map(list.map((s) => [s.id, s.name])));
        }
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [library, subjectId]);

  // An older reply never replaces a newer one.
  // A reply that came (a read or a call) clears an earlier read error.
  const apply = useCallback((w: TurnkeyWork) => {
    latest.current = acceptWork(latest.current, w);
    setWork(latest.current);
    setLoadError("");
  }, []);

  // The body reports a call: "" starts one, a text fails it; a reply of a call clears the error, a poll does not.
  const onError = useCallback((text: string) => setActionError((p) => nextActionError(p, text === "" ? { type: "start" } : { type: "fail", text })), []);
  const onWork = useCallback(
    (w: TurnkeyWork) => {
      apply(w);
      setActionError((p) => nextActionError(p, { type: "ok" }));
    },
    [apply],
  );

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

  // A stop of a running step is asynchronous, but the one poller above keeps reading while writing.active is true.
  // «Остановка…» shows only while the job is still active; it is dropped once the job is gone, and for a new work.
  useEffect(() => setStopping(false), [workId]);
  useEffect(() => setStopping((p) => nextStopping(p, work)), [work, stopping]);
  const stoppingNow = nextStopping(stopping, work);

  return (
    <>
      {work !== null && (
        <WorkBody
          key={`${work.writing?.step ?? "-"}:${work.writing?.generation ?? 0}`}
          work={work}
          api={api}
          onWork={onWork}
          error={actionError}
          onError={onError}
          library={library}
          send={send}
          subjects={subjects}
          stopping={stoppingNow}
          onStopped={() => setStopping(true)}
        />
      )}
      {loadError !== "" && (
        <p className="small muted" role="status">
          {loadError}
        </p>
      )}
    </>
  );
}
