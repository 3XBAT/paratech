import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { activityApi, type RecordingItem } from "../bindings/activity.js";
import { agentConfirmApi } from "../bindings/agentConfirm.js";
import { homeworkApi } from "../bindings/homework.js";
import { libraryApi } from "../bindings/library.js";
import { recordingDeleteApi } from "../bindings/recordingDelete.js";
import { callByName } from "../bindings/runtime.js";
import { type TodayScreen, todayApi } from "../bindings/today.js";
import { requestDraft } from "../agentDraft.js";
import type { PageId } from "../nav.js";
import { useOpenSummary } from "../openSummary.js";
import { anyRecording, queueLine } from "../recordingRowView.js";
import { dayTitle, loadView, nearestDeadlineText, newestOnly, stagesChanged, stagesKey, stopProblemText, texts } from "../todayView.js";
import { Toast } from "../ui/Toast.js";
import { useNow } from "../useNow.js";
import { burningAfterDone } from "../assignmentDoneView.js";
import { AddAssignmentDialog } from "./HomeworkDialogs.js";
import { SpeechComponentNotice } from "./SpeechComponentNotice.js";
import {
  AskSection,
  BurningSection,
  LessonsSection,
  LoadError,
  RecordAction,
  StatusBar,
  StopProblem,
  SummarySection,
  WaitingSection,
} from "./TodayParts.js";
import { doneToastProps, useDoneFocus } from "./useDoneFocus.js";
import { useDoneRunner } from "./useDoneRunner.js";

const pollMs = 3000;

// «Сегодня»: the day of the student (TASK-126). The core builds the screen (TASK-125); the live recording and
// recognition status of a lesson comes from the activity binding, the same one «Записи» reads. The recordings have their
// own section since TASK-348; «Записать пару» in the head leads there (or stops the running recording).
export function TodayPage(props: {
  onOpen: (id: PageId) => void;
  onOpenAssignment: (id: string) => void;
  /** «Записать пару»: opens «Записи» with the start panel open. */
  onRecord: () => void;
}) {
  const api = useMemo(() => todayApi(callByName), []);
  const activity = useMemo(() => activityApi(callByName), []);
  const calendar = useMemo(() => ({ api: agentConfirmApi(callByName), deleteApi: recordingDeleteApi(callByName) }), []);
  const openSummary = useOpenSummary();
  const homework = useMemo(() => homeworkApi(callByName), []);
  const library = useMemo(() => libraryApi(callByName), []);
  const [adding, setAdding] = useState(false);
  const [screen, setScreen] = useState<TodayScreen | null>(null);
  const screenRef = useRef(screen);
  screenRef.current = screen;
  // A failed read says one fixed thing (LoadError), so only the fact is kept, not the text of the core.
  const [failed, setFailed] = useState(false);
  // «Повторить» was pressed and the answer has not come: the error line gives way to a small «Загрузка…».
  const [retrying, setRetrying] = useState(false);
  const [recordings, setRecordings] = useState<RecordingItem[] | null>(null);
  const mounted = useRef(true);

  const requests = useRef(newestOnly());
  const rootRef = useRef<HTMLElement>(null);

  const load = useCallback(() => {
    const token = requests.current.next();
    api.today().then(
      (s) => {
        if (mounted.current && requests.current.isNewest(token)) {
          setScreen(s);
          setFailed(false);
          setRetrying(false);
        }
      },
      () => {
        if (mounted.current && requests.current.isNewest(token)) {
          setFailed(true);
          setRetrying(false);
        }
      },
    );
  }, [api]);

  // The pressed button leaves with the error line, so the focus goes to the section first and does not fall to the body.
  const retry = useCallback(() => {
    rootRef.current?.focus();
    setFailed(false);
    setRetrying(true);
    load();
  }, [load]);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // «Выполнено» in «Сроки на неделе» (TASK-316): the row leaves at once, the screen is read again; «Отменить» brings it back.
  const focus = useDoneFocus(rootRef);
  const doneRun = useDoneRunner(homework, {
    apply: (res) => {
      // A row that leaves takes the focus with it: it goes to the next row's check.
      const cur = screenRef.current?.burning ?? [];
      const next = burningAfterDone(cur, res);
      focus.rowsLeaving(cur.filter((b) => !next.some((x) => x.assignmentId === b.assignmentId)).map((b) => b.assignmentId));
      setScreen((s) => (s === null ? s : { ...s, burning: burningAfterDone(s.burning, res) }));
    },
    refresh: load,
  });

  // Read on each visit of the section and when the window gets back the focus.
  useEffect(() => {
    load();
    window.addEventListener("focus", load);
    return () => window.removeEventListener("focus", load);
  }, [load]);

  // The recordings list for the live status of the lesson rows and for the main button (a running recording is stopped
  // from here); a failed read leaves the rows without it and the button at «Записать пару».
  const readRecordings = useCallback(() => {
    activity.listRecordings().then(
      (r) => mounted.current && setRecordings(r),
      () => undefined,
    );
  }, [activity]);
  useEffect(() => {
    const read = () => {
      if (document.visibilityState === "visible") {
        readRecordings();
      }
    };
    read();
    const timer = setInterval(read, pollMs);
    return () => clearInterval(timer);
  }, [readRecordings]);

  // The stop of the running recording: the same binding as the rows of «Записи». One call at a time; a failure says so
  // with the way out, and the list is read again either way.
  const [stopping, setStopping] = useState(false);
  const [stopProblem, setStopProblem] = useState("");
  const stoppingRef = useRef(false);
  const stop = useCallback(
    async (recordingId: string) => {
      if (stoppingRef.current) {
        return;
      }
      stoppingRef.current = true;
      setStopping(true);
      setStopProblem("");
      try {
        await activity.stopRecording(recordingId);
      } catch (err) {
        if (mounted.current) {
          setStopProblem(stopProblemText(err));
        }
      } finally {
        stoppingRef.current = false;
        if (mounted.current) {
          setStopping(false);
        }
      }
      if (mounted.current) {
        readRecordings();
      }
    },
    [activity, readRecordings],
  );
  const running = recordings?.some((r) => r.stage === "recording") ?? false;
  // The elapsed time of the button ticks every second from startedAt, independent of the 3 s poll (as on «Записи»).
  const now = useNow(recordings !== null && anyRecording(recordings));
  useEffect(() => {
    // The recording is gone (stopped here or elsewhere): an old failure line has nothing to refer to.
    if (!running) {
      setStopProblem("");
    }
  }, [running]);

  // A recording that starts, stops or finishes changes the day: the screen is read again. The first reply of the
  // list is not a change (the screen is read on entry).
  const stages = recordings === null ? null : stagesKey(recordings);
  const prevStages = useRef<string | null>(null);
  useEffect(() => {
    const changed = stagesChanged(prevStages.current, stages);
    prevStages.current = stages;
    if (changed) {
      load();
    }
  }, [stages, load]);

  const openRecordings = () => props.onOpen("recordings");
  const queue = queueLine(recordings ?? [], []);
  const nearest = screen === null ? "" : nearestDeadlineText(screen.burning, new Date());
  const view = loadView(screen !== null, failed, retrying);

  // The toast is outside .today: that is a size container, which would make it the containing block of the toast.
  return (
    <>
    <section className="page today" tabIndex={-1} ref={rootRef} aria-labelledby="today-title">
      <div className="head">
        <div>
          <span className="label num">{screen === null ? "" : dayTitle(screen.day)}</span>
          <h1 id="today-title">{texts.title}</h1>
        </div>
        <div className="acts">
          <RecordAction recordings={recordings} now={now} stopping={stopping} onStart={props.onRecord} onStop={(id) => void stop(id)} />
        </div>
      </div>
      {stopProblem !== "" && <StopProblem text={stopProblem} onOpenRecordings={openRecordings} />}
      <SpeechComponentNotice primary={false} />
      {view === "error" && <LoadError onRetry={retry} />}
      {view === "stale" && <LoadError onRetry={retry} quiet />}
      {view === "loading" && (
        <p className="muted small" role="status">
          {texts.loading}
        </p>
      )}
      {adding && (
        <AddAssignmentDialog
          api={homework}
          library={library}
          onClose={() => setAdding(false)}
          onCreated={() => {
            setAdding(false);
            load(); // the new assignment shows in «Сроки на неделе» when its deadline is near
          }}
        />
      )}
      {screen !== null && (
        <>
          <AskSection chips={screen.chips} onChip={(text) => requestDraft(text)} />
          {/* «Итоги дня» is its own grid child: last in one column, under the lessons in two (today.css). */}
          <div className="today-cols">
            <div className="today-lessons">
              <LessonsSection
                lessons={screen.lessons}
                recordings={recordings ?? []}
                day={screen.day}
                onOpenSummary={openSummary}
                onOpenRecordings={openRecordings}
              />
            </div>
            <div className="today-decide">
              <WaitingSection waiting={screen.waiting} onOpenAssignment={props.onOpenAssignment} />
              <BurningSection
                burning={screen.burning}
                onOpenAssignment={props.onOpenAssignment}
                onAdd={() => setAdding(true)}
                done={{
                  busyId: doneRun.busyId,
                  issue: doneRun.issue,
                  onMark: (id, title) => void doneRun.mark(id, true, title),
                  onDismissIssue: doneRun.dismissIssue,
                }}
              />
            </div>
            <SummarySection posts={screen.posts} done={screen.done} onOpenSummary={openSummary} calendar={calendar} />
          </div>
          <StatusBar queue={queue} nearest={nearest} onOpenQueue={openRecordings} />
        </>
      )}
    </section>
    <Toast {...doneToastProps(doneRun, focus)} />
    </>
  );
}
