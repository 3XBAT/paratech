import { type ReactNode, useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { activityApi, type AdviceView, type JobItem, type RecordingItem } from "../bindings/activity";
import { promptApi } from "../bindings/prompt";
import { recordingDeleteApi } from "../bindings/recordingDelete";
import { recordingImportApi } from "../bindings/recordingImport";
import { todayApi } from "../bindings/today";
import { callByName } from "../bindings/runtime";
import { actionErrorText, loadErrorText } from "../activityView";
import { CollapsibleList } from "../CollapsibleList";
import { type ListStates, stateOf, withState } from "../listView";
import { manualStartTexts, startButton, startOutcome } from "../manualStartView";
import type { PageId } from "../nav";
import {
  type DeleteOutcome,
  deleteOutcome,
  deleteTexts,
  pruneSelection,
  selectedDeleteLabel,
  selectedText,
  withSelected,
} from "../recordingDeleteView";
import { importTexts } from "../recordingImportView";
import {
  anyRecording,
  backgroundCount,
  backgroundFailedText,
  backgroundKey,
  backgroundShowFailed,
  backgroundState,
  backgroundTitle,
  backgroundVisible,
  emptyTexts,
  headActionsVisible,
  pageGroups,
  queueLine,
  tasksKey,
} from "../recordingRowView";
import { adviceKey, keepSource, switchToSuggestion, type AdvicePending } from "../sourcePicker/advice";
import { SourceNotes, SourcePicker } from "../sourcePicker/SourcePicker";
import { useSources } from "../sourcePicker/useSources";
import {
  actionErrorText as startErrorText,
  chooseSource,
  initialSelection,
  nextSelection,
  pickerModel,
  type Selection,
} from "../sourcePicker/view";
import { redoTexts } from "../summaryView";
import { Button } from "../ui/Button";
import { SpeechComponentNotice } from "./SpeechComponentNotice";
import { Banner, Dialog, ResultLine } from "../ui/controls";
import { Icon } from "../ui/Icon";
import { IconAction } from "../ui/IconAction";
import { useNow } from "../useNow";
import { useRecordingSound } from "../useRecordingSound";
import { useOpenLesson } from "../openLesson";
import { RecordingDeleteDialog } from "./RecordingDeleteDialog";
import { RecordingImportPanel } from "./RecordingImportPanel";
import { JobRow, RecordingRow } from "./RecordingRow";

const pollMs = 3000;

// The page shows what the Go core reports: one row per recording with the status of its whole chain (TASK-076). The
// Stop, Run now and link buttons appear only when the core offers them (ARCH-44). «Начать запись» starts a recording
// without the popup, from the popup's source picker (TASK-081). A draft job row opens its assignment (TASK-094).
// «Удалить» in a row's «⋯» menu and «Удалить выбранные» delete recordings after one confirmation (TASK-095).
// «Сделать конспект заново» in the menu asks with the lesson screen's confirmation: it spends quota (TASK-096).
// A row with texts opens the lesson screen on its own opening tab; «⋯» holds the named tabs (TASK-352).
// Recordings and the other jobs are separate sections on one row grid (TASK-098).
export function RecordingsPage({
  onOpen,
  onOpenAssignment,
  openStart = false,
  onStartTaken,
}: {
  onOpen?: (page: PageId) => void;
  onOpenAssignment?: (assignmentId: string) => void;
  /** «Записать пару» of «Сегодня»: the page opens with the start panel open; taken once on arrival (onStartTaken). */
  openStart?: boolean;
  onStartTaken?: () => void;
}) {
  const api = useMemo(() => activityApi(callByName), []);
  const prompts = useMemo(() => promptApi(callByName), []);
  const deletion = useMemo(() => recordingDeleteApi(callByName), []);
  const importing = useMemo(() => recordingImportApi(callByName), []);
  const today = useMemo(() => todayApi(callByName), []);
  const openLesson = useOpenLesson();
  const [recordings, setRecordings] = useState<RecordingItem[] | null>(null);
  const [jobs, setJobs] = useState<JobItem[] | null>(null);
  const [recordingsError, setRecordingsError] = useState("");
  const [jobsError, setJobsError] = useState("");
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  // Kept across the polls: a refreshed list keeps what the user folded or expanded.
  const [lists, setLists] = useState<ListStates>({});
  const loading = useRef(false);
  const again = useRef(false);
  const mounted = useRef(true);

  // One load at a time: a refresh asked for during a load runs once more after it, so a refresh after an action is
  // never answered with data read before the action finished.
  const refresh = useCallback(async () => {
    if (loading.current) {
      again.current = true;
      return;
    }
    loading.current = true;
    try {
      do {
        again.current = false;
        // Each list loads on its own: a failing one keeps its last good data and shows its own error.
        const [r, j] = await Promise.allSettled([api.listRecordings(), api.listJobs()]);
        if (mounted.current) {
          if (r.status === "fulfilled") {
            setRecordings(r.value);
            setRecordingsError("");
          } else {
            setRecordingsError(loadErrorText(r.reason, "recordings"));
          }
          if (j.status === "fulfilled") {
            setJobs(j.value);
            setJobsError("");
          } else {
            setJobsError(loadErrorText(j.reason, "jobs"));
          }
        }
      } while (again.current && mounted.current);
    } finally {
      loading.current = false;
    }
  }, [api]);

  useEffect(() => {
    mounted.current = true;
    void refresh();
    const timer = setInterval(() => void refresh(), pollMs);
    return () => {
      mounted.current = false;
      clearInterval(timer);
    };
  }, [refresh]);

  async function act(key: string, action: () => Promise<void>) {
    setBusy((b) => new Set(b).add(key));
    setActionError("");
    try {
      await action();
    } catch (err) {
      if (mounted.current) {
        setActionError(actionErrorText(err));
      }
    } finally {
      if (mounted.current) {
        setBusy((b) => {
          const next = new Set(b);
          next.delete(key);
          return next;
        });
      }
    }
    if (mounted.current) {
      await refresh();
    }
  }

  // Deletion (TASK-095): the rows checked for «Удалить выбранные», the ids of the open confirmation and the last result.
  // No audio plays here: the transcript's player is on its own screen, which is closed while this list shows.
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [deleting, setDeleting] = useState<readonly string[] | null>(null);
  const [deleted, setDeleted] = useState<DeleteOutcome | null>(null);

  useEffect(() => {
    if (recordings !== null) {
      setSelected((sel) => pruneSelection(sel, recordings));
    }
  }, [recordings]);

  function askDelete(ids: readonly string[]) {
    if (ids.length > 0) {
      setDeleted(null);
      setDeleting(ids);
    }
  }

  // The recording whose «Сделать конспект заново» waits for the confirmation; it closes when the row stops offering it.
  const [redoAsk, setRedoAsk] = useState<string | null>(null);
  const redoOffered = redoAsk !== null && (recordings ?? []).some((r) => r.id === redoAsk && r.canRedoSummary);

  useEffect(() => {
    if (redoAsk !== null && !redoOffered) {
      setRedoAsk(null);
    }
  }, [redoAsk, redoOffered]);

  function redo() {
    const id = redoAsk;
    setRedoAsk(null);
    if (id !== null && !busy.has(`redo:${id}`)) {
      void act(`redo:${id}`, () => api.redoSummary(id));
    }
  }

  // «Импортировать запись» (TASK-162): the panel; the mark of an imported row comes from the recording's origin.
  const [importOpen, setImportOpen] = useState(false);
  const loadLessons = useCallback(async () => (await today.today()).lessons, [today]);

  // The silent-source advice of the running recording (TASK-082): the click running per recording, and the problem of
  // the last click per advice (a new suggestion starts clean).
  const [advicePending, setAdvicePending] = useState<Readonly<Record<string, AdvicePending>>>({});
  const [adviceProblems, setAdviceProblems] = useState<Readonly<Record<string, string>>>({});

  async function adviceAct(a: AdviceView, kind: "switch" | "dismiss") {
    const id = a.recordingId;
    setAdvicePending((p) => ({ ...p, [id]: kind }));
    setAdviceProblems((p) => ({ ...p, [adviceKey(a)]: "" }));
    const out = kind === "switch" ? await switchToSuggestion(api, a) : await keepSource(api, a);
    if (!mounted.current) {
      return;
    }
    setAdvicePending((p) => {
      const { [id]: _done, ...rest } = p;
      return rest;
    });
    setAdviceProblems((p) => ({ ...p, [adviceKey(a)]: out.kind === "problem" ? out.text : "" }));
    await refresh();
  }

  // «Начать запись»: the panel with the picker, the choice for this start and the problem of a refused one.
  // Arrival from «Записать пару» (TASK-348): the panel is open from the first paint.
  const [startOpen, setStartOpen] = useState(openStart);
  const [starting, setStarting] = useState(false);
  const startingRef = useRef(false);
  const [startProblem, setStartProblem] = useState("");
  const [selection, setSelection] = useState<Selection>(initialSelection);
  const [listOpen, setListOpen] = useState(false);
  const { sources } = useSources(startOpen && !listOpen && !starting);
  const reasonId = useId();
  const labelId = useId();

  useEffect(() => {
    if (sources !== null) {
      setSelection((prev) => nextSelection(prev, sources));
    }
  }, [sources]);

  const startState = startButton(recordings, starting);
  const picker = pickerModel(sources, selection);
  // Each opening starts with a fresh choice, like a new popup.
  function toggleStart() {
    setStartProblem("");
    setListOpen(false);
    if (!startOpen) {
      setSelection(sources === null ? initialSelection : nextSelection(initialSelection, sources));
    }
    setStartOpen(!startOpen);
  }

  async function confirmStart() {
    if (startingRef.current || startState.disabled) {
      return;
    }
    startingRef.current = true;
    setStarting(true);
    setStartProblem("");
    setListOpen(false);
    try {
      const out = startOutcome(await prompts.startRecording(picker.chosen.app));
      if (mounted.current) {
        if (out.kind === "started") {
          setStartOpen(false);
        } else {
          setStartProblem(out.problem);
        }
      }
    } catch (err) {
      if (mounted.current) {
        setStartProblem(startErrorText(err));
      }
    } finally {
      startingRef.current = false;
      if (mounted.current) {
        setStarting(false);
        void refresh();
      }
    }
  }

  // The elapsed time of a running recording ticks every second from startedAt, independent of the 3 s poll.
  const now = useNow(anyRecording(recordings ?? []));
  // The level capsule of the running recording (TASK-099): read only while a row shows it running and the window is
  // visible (a hidden main window does not poll 4 times a second).
  const [visible, setVisible] = useState(() => document.visibilityState === "visible");
  useEffect(() => {
    const onVisibility = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, []);
  const { sound } = useRecordingSound(api.recordingSound, visible && anyRecording(recordings ?? []));

  // Two sections, «Записи» and «Фоновая обработка», each with «Идут» and the folded «Завершённые» (TASK-098); rows in progress
  // (and those with Stop or Run now) always stay in view. Only the latest draft job per assignment is listed.
  const groups = pageGroups(recordings ?? [], jobs ?? []);

  // The rows of one group, in a card on the shared row grid (.arows): status, button and «⋯» columns line up in every
  // card of both sections. CollapsibleList puts the finished ones into the same kind of card.
  const card = (rows: ReactNode) => (
    <div className="card arows" role="list">
      {rows}
    </div>
  );

  const recordingRow = (r: RecordingItem) => (
    <RecordingRow
      key={r.id}
      item={r}
      now={now}
      stopping={busy.has(`stop:${r.id}`)}
      starting={busy.has(`run:${r.actionJobId}`)}
      onStop={() => void act(`stop:${r.id}`, () => api.stopRecording(r.id))}
      onRunNow={(jobId) => void act(`run:${jobId}`, () => api.runJobNow(jobId))}
      onOpen={(page) => onOpen?.(page)}
      retrying={busy.has(`retry:${r.id}`)}
      onRetry={(work) => void act(`retry:${r.id}`, () => api.retryPostProcessing(r.id, work))}
      advicePending={advicePending[r.id] ?? null}
      adviceProblem={r.advice === null ? "" : (adviceProblems[adviceKey(r.advice)] ?? "")}
      onSwitchSource={() => r.advice && void adviceAct(r.advice, "switch")}
      onKeepSource={() => r.advice && void adviceAct(r.advice, "dismiss")}
      onOpenLesson={openLesson}
      redoing={busy.has(`redo:${r.id}`)}
      onRedoSummary={() => setRedoAsk(r.id)}
      onDelete={() => askDelete([r.id])}
      selected={selected.has(r.id)}
      onSelect={(on) => setSelected((sel) => withSelected(sel, r.id, on))}
      sound={sound}
      imported={r.origin === "imported"}
    />
  );

  const jobRow = (j: JobItem) => (
    <JobRow
      key={j.id}
      item={j}
      starting={busy.has(`run:${j.id}`)}
      onRunNow={() => void act(`run:${j.id}`, () => api.runJobNow(j.id))}
      onOpenAssignment={onOpenAssignment}
    />
  );

  // «Идут» and «Завершённые (N)» of one section; the folded state is kept per section across the polls. The hidden
  // word gives the two «Завершённые» toggles distinct names for screen readers.
  function sectionGroups<T extends { id: string }>(
    list: "recordings" | "tasks",
    name: string,
    section: { active: T[]; finished: T[] },
    row: (item: T) => ReactNode,
  ) {
    // «Записи» is the page h1, so its «Идут» is an h2 (no skipped level); the tasks sit under the h2 «Фоновая обработка».
    const Sub = list === "recordings" ? "h2" : "h3";
    return (
      <>
        {section.active.length > 0 && (
          <>
            <Sub className="arows-sub">Идут</Sub>
            {card(section.active.map(row))}
          </>
        )}
        {section.finished.length > 0 && (
          <CollapsibleList
            title={
              <>
                Завершённые<span className="vh"> · {name}</span>
              </>
            }
            items={section.finished}
            itemKey={(item) => item.id}
            renderItem={row}
            renderContainer={card}
            state={stateOf(lists, list)}
            onStateChange={(s) => setLists((l) => withState(l, list, s))}
            className="clist-card"
            classNames={{ button: "btn ghost" }}
          />
        )}
      </>
    );
  }

  const visibleJobs = [...groups.tasks.active, ...groups.tasks.finished];
  const noRecordings = recordings !== null && recordings.length === 0;
  const queue = queueLine(recordings ?? [], visibleJobs);
  const background = backgroundState(lists);
  const backgroundN = backgroundCount(groups.tasks);
  const failed = backgroundFailedText(groups.tasks, background, stateOf(lists, tasksKey));

  // With an empty list the same buttons sit in the empty state, not in the head (one primary button on the screen).
  const headActions = headActionsVisible(recordings, recordingsError !== "");

  // An opened panel replaces the empty state (it opens where the student clicked); closing it brings the state back.
  const panelOpen = startOpen || importOpen;
  const panelsRef = useRef<HTMLDivElement>(null);
  const emptyRef = useRef<HTMLDivElement>(null);
  const panelWasOpen = useRef(false);

  // The clicked button is gone with the empty state: the focus follows into the panel, and back to the state on closing.
  useEffect(() => {
    if (noRecordings && panelOpen !== panelWasOpen.current) {
      const target = panelOpen ? panelsRef.current : emptyRef.current;
      target?.querySelector<HTMLElement>("button:not(:disabled), input:not(:disabled)")?.focus();
    }
    panelWasOpen.current = panelOpen;
  }, [panelOpen, noRecordings]);

  // The clicked «Записать пару» is on another page: the focus goes into the panel. The request is taken once on arrival, so
  // the window drops it and a later visit or «Назад» does not open the panel again.
  const focusStart = useRef(openStart);
  useEffect(() => {
    if (focusStart.current) {
      focusStart.current = false;
      panelsRef.current?.querySelector<HTMLElement>("button:not(:disabled), input:not(:disabled)")?.focus();
    }
    if (openStart) {
      onStartTaken?.();
    }
  }, []);

  const importButton = (
    <Button icon="up" aria-expanded={importOpen} onClick={() => setImportOpen((open) => !open)}>
      {importTexts.open}
    </Button>
  );

  const startControl = (
    <div className="rec-start">
      <Button
        // An open panel (start or import) has its own primary button: it is the only accent then, this one is quiet.
        variant={panelOpen ? undefined : "primary"}
        icon="rec"
        // An open panel can always be closed, except while its start is on the way.
        disabled={starting || (startState.disabled && !startOpen)}
        aria-expanded={startOpen}
        title={startState.reason || undefined}
        aria-describedby={startState.reason ? reasonId : undefined}
        onClick={toggleStart}
      >
        {manualStartTexts.open}
      </Button>
      {/* The empty state shows the reason as a visible line instead. */}
      {startState.reason && (!noRecordings || panelOpen) && (
        <span id={reasonId} className="vh">
          {startState.reason}
        </span>
      )}
    </div>
  );

  return (
    <section className="page recordings">
      <div className="head">
        <h1>Записи</h1>
        {headActions && (
          <div className="head-acts">
            {importButton}
            {startControl}
          </div>
        )}
      </div>
      <div ref={panelsRef} className="rec-panels">
        {importOpen && (
          <RecordingImportPanel
            api={importing}
            loadLessons={loadLessons}
            onImported={() => void refresh()}
            onClose={() => setImportOpen(false)}
          />
        )}
        {startOpen && (
          <div className="card box rec-start-panel" role="group" aria-label={manualStartTexts.title}>
            <h2>{manualStartTexts.title}</h2>
            <span className="small muted box-desc">{manualStartTexts.explain}</span>
            <span className="label" id={labelId}>
              {manualStartTexts.sourceLabel}
            </span>
            <SourcePicker
              picker={picker}
              disabled={starting}
              open={listOpen}
              onToggle={() => setListOpen((open) => !open)}
              onClose={() => setListOpen(false)}
              onChoose={(app) => {
                setSelection(chooseSource(app));
                setListOpen(false);
              }}
              labelId={labelId}
            />
            <SourceNotes picker={picker} advice />
            {startProblem !== "" && (
              <Banner kind="fail" role="alert">
                {startProblem}
              </Banner>
            )}
            <div className="acts start">
              <Button
                variant="primary"
                icon="rec"
                busy={starting}
                busyLabel={manualStartTexts.starting}
                disabled={startState.disabled}
                title={startState.reason || undefined}
                aria-describedby={startState.reason ? reasonId : undefined}
                onClick={() => void confirmStart()}
              >
                {manualStartTexts.start}
              </Button>
              <Button variant="ghost" disabled={starting} onClick={toggleStart}>
                {manualStartTexts.cancel}
              </Button>
            </div>
          </div>
        )}
      </div>
      <SpeechComponentNotice onReady={() => void refresh()} primary={false} />
      {actionError && (
        <Banner kind="fail" role="alert">
          {actionError}
        </Banner>
      )}

      {recordingsError && (
        <Banner kind="fail" role="alert">
          {recordingsError}
        </Banner>
      )}
      {selected.size > 0 && (
        <div className="acts start rec-selected" role="group" aria-label="Выбранные записи">
          <span className="num">{selectedText(selected.size)}</span>
          {/* The trash names the count: the confirmation that follows lists the recordings. */}
          <IconAction icon="trash" variant="danger" label={selectedDeleteLabel(selected.size)} disabled={deleting !== null} onClick={() => askDelete([...selected])} />
          <IconAction icon="x" label={deleteTexts.selectedClear} disabled={deleting !== null} onClick={() => setSelected(new Set())} />
        </div>
      )}
      {deleted && (
        <ResultLine kind={deleted.kind} role={deleted.kind === "fail" ? "alert" : "status"}>
          {deleted.lines.map((l, i) => (
            <span key={i} className="rec-deleted-line">
              {l}
            </span>
          ))}
        </ResultLine>
      )}
      {recordings === null ? (
        !recordingsError && <p className="muted">Загрузка…</p>
      ) : noRecordings ? (
        // An open panel is the answer to the click: the empty state, with its second «Начать запись», waits until it closes.
        !panelOpen && (
          <div ref={emptyRef} className="rec-empty">
            <p className="empty-text">{emptyTexts.text}</p>
            <div className="acts start">
              {startControl}
              {onOpen && (
                <Button icon="cal" onClick={() => onOpen("schedule")}>
                  {emptyTexts.schedule}
                </Button>
              )}
              {importButton}
            </div>
            {startState.reason && (
              <p id={reasonId} className="small muted rec-empty-reason">
                {startState.reason}
              </p>
            )}
            <p className="small muted rec-empty-privacy">
              <Icon name="lock" />
              {emptyTexts.privacy}
            </p>
          </div>
        )
      ) : (
        sectionGroups("recordings", "записи", groups.recordings, recordingRow)
      )}

      {jobsError && (
        <Banner kind="fail" role="alert">
          {jobsError}
        </Banner>
      )}
      {backgroundVisible(visibleJobs) && (
        // Folded by default: what a recording needs shows on its own row, the rest is a count away. The header is
        // CollapsibleList's markup, drawn here because the title has no number when nothing is in progress or failed.
        <div className="clist clist-card background-block">
          <h2 className="clist-header">
            <button
              type="button"
              className="clist-toggle"
              aria-expanded={!background.collapsed}
              onClick={() => setLists((l) => withState(l, backgroundKey, { ...background, collapsed: !background.collapsed }))}
            >
              <span aria-hidden="true">{background.collapsed ? "▸ " : "▾ "}</span>
              {backgroundTitle}
              {backgroundN > 0 && <span className="clist-count"> ({backgroundN})</span>}
            </button>
          </h2>
          {failed !== "" && (
            <div className="background-failed">
              <Button size="sm" onClick={() => setLists(backgroundShowFailed)}>
                {failed}
              </Button>
            </div>
          )}
          {!background.collapsed && sectionGroups("tasks", "фоновая обработка", groups.tasks, jobRow)}
        </div>
      )}

      {deleting !== null && (
        <RecordingDeleteDialog
          api={deletion}
          ids={deleting}
          onCancel={() => {
            setDeleting(null);
            void refresh();
          }}
          onDone={(res, preview) => {
            setDeleting(null);
            setDeleted(deleteOutcome(res, preview));
            setSelected(new Set());
            void refresh();
          }}
        />
      )}

      <Dialog
        open={redoOffered}
        title={redoTexts.title}
        confirmLabel={redoTexts.confirm}
        cancelLabel={redoTexts.cancel}
        onConfirm={redo}
        onCancel={() => setRedoAsk(null)}
      >
        <p>{redoTexts.body}</p>
      </Dialog>

      {queue && (
        <>
          <div className="gap" />
          <div className="statusbar">
            <span className="num">{queue}</span>
          </div>
        </>
      )}
    </section>
  );
}
