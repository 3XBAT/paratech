import { type ReactNode, useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { activityApi, type AdviceView, type JobItem, type RecordingItem } from "../bindings/activity";
import { promptApi } from "../bindings/prompt";
import { recordingDeleteApi } from "../bindings/recordingDelete";
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
  selectedText,
  withSelected,
} from "../recordingDeleteView";
import { anyRecording, pageGroups, queueLine, tasksEmptyText } from "../recordingRowView";
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
import { Banner, Dialog, EmptyState, ResultLine } from "../ui/controls";
import { Icon } from "../ui/Icon";
import { useNow } from "../useNow";
import { useRecordingSound } from "../useRecordingSound";
import { useOpenSummary } from "../openSummary";
import { useOpenTranscript } from "../openTranscript";
import { RecordingDeleteDialog } from "./RecordingDeleteDialog";
import { JobRow, RecordingRow } from "./RecordingRow";

const pollMs = 3000;

// The page shows what the Go core reports: one row per recording with the status of its whole chain (TASK-076). The
// Stop, Run now and link buttons appear only when the core offers them (ARCH-44). «Начать запись» starts a recording
// without the popup, from the popup's source picker (TASK-081). A draft job row opens its assignment (TASK-094).
// «Удалить» in a row's «⋯» menu and «Удалить выбранные» delete recordings after one confirmation (TASK-095).
// «Сделать конспект заново» in the menu asks with the summary screen's confirmation: it spends quota (TASK-096).
// Recordings and the other jobs are separate sections on one row grid (TASK-098).
export function RecordingsPage({
  onOpen,
  onOpenAssignment,
}: {
  onOpen?: (page: PageId) => void;
  onOpenAssignment?: (assignmentId: string) => void;
}) {
  const api = useMemo(() => activityApi(callByName), []);
  const prompts = useMemo(() => promptApi(callByName), []);
  const deletion = useMemo(() => recordingDeleteApi(callByName), []);
  const openTranscript = useOpenTranscript();
  const openSummary = useOpenSummary();
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
  const [startOpen, setStartOpen] = useState(false);
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
  // The level capsule of the running recording (TASK-099): read only while a row shows it running.
  const { sound } = useRecordingSound(api.recordingSound, anyRecording(recordings ?? []));

  // Two sections, «Записи» and «Задачи», each with «Идут» and the folded «Завершённые» (TASK-098); rows in progress
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
      onOpenSummary={openSummary}
      onOpenTranscript={openTranscript}
      redoing={busy.has(`redo:${r.id}`)}
      onRedoSummary={() => setRedoAsk(r.id)}
      onDelete={() => askDelete([r.id])}
      selected={selected.has(r.id)}
      onSelect={(on) => setSelected((sel) => withSelected(sel, r.id, on))}
      sound={sound}
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
    return (
      <>
        {section.active.length > 0 && (
          <>
            <h3 className="arows-sub">Идут</h3>
            {card(section.active.map(row))}
          </>
        )}
        {section.finished.length > 0 && (
          <CollapsibleList
            title={
              <>
                Завершённые<span className="vh"> {name}</span>
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

  return (
    <section className="page recordings">
      <div className="head">
        <h1>Записи и задачи</h1>
        <div className="rec-start">
          <Button
            variant="primary"
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
          {startState.reason && (
            <span id={reasonId} className="vh">
              {startState.reason}
            </span>
          )}
        </div>
      </div>
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
      <SpeechComponentNotice onReady={() => void refresh()} />
      {actionError && (
        <Banner kind="fail" role="alert">
          {actionError}
        </Banner>
      )}

      {/* The empty state has its own heading. */}
      {!noRecordings && <h2 className="group-title">Записи</h2>}
      {recordingsError && (
        <Banner kind="fail" role="alert">
          {recordingsError}
        </Banner>
      )}
      {selected.size > 0 && (
        <div className="acts start rec-selected" role="group" aria-label="Выбранные записи">
          <span className="num">{selectedText(selected.size)}</span>
          <Button variant="danger" disabled={deleting !== null} onClick={() => askDelete([...selected])}>
            {deleteTexts.selectedDelete}
          </Button>
          <Button variant="ghost" disabled={deleting !== null} onClick={() => setSelected(new Set())}>
            {deleteTexts.selectedClear}
          </Button>
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
        <EmptyState
          label="Записей пока нет"
          title="Здесь появятся записи пар"
          actions={
            onOpen && (
              <Button variant="primary" icon="up" onClick={() => onOpen("schedule")}>
                Импортировать расписание
              </Button>
            )
          }
        >
          Импортируйте расписание — в начале каждой пары появится окно с кнопкой «Начать запись».
        </EmptyState>
      ) : (
        sectionGroups("recordings", "записи", groups.recordings, recordingRow)
      )}

      <h2 className="group-title">Задачи</h2>
      {jobsError && (
        <Banner kind="fail" role="alert">
          {jobsError}
        </Banner>
      )}
      {jobs === null ? (
        !jobsError && <p className="muted">Загрузка…</p>
      ) : visibleJobs.length === 0 ? (
        <p className="muted arows-empty">{tasksEmptyText}</p>
      ) : (
        sectionGroups("tasks", "задачи", groups.tasks, jobRow)
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

      <div className="gap" />
      <div className="statusbar">
        {queue && <span className="num">{queue}</span>}
        <span className="r">
          <Icon name="lock" />
          Звук обрабатывается только на этом компьютере
        </span>
      </div>
    </section>
  );
}
