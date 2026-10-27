import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { activityApi, type RecordingItem } from "../bindings/activity.js";
import { agentConfirmApi } from "../bindings/agentConfirm.js";
import { homeworkApi } from "../bindings/homework.js";
import { libraryApi } from "../bindings/library.js";
import { recordingDeleteApi } from "../bindings/recordingDelete.js";
import { callByName } from "../bindings/runtime.js";
import { type TodayScreen, todayApi, todayErrorText } from "../bindings/today.js";
import { requestDraft } from "../agentDraft.js";
import type { PageId } from "../nav.js";
import { useOpenSummary } from "../openSummary.js";
import { queueLine } from "../recordingRowView.js";
import { dayTitle, nearestDeadlineText, newestOnly, stagesChanged, stagesKey, statusTexts, texts } from "../todayView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { AddAssignmentDialog } from "./HomeworkDialogs.js";
import { RecordingsPage } from "./RecordingsPage.js";
import { AskSection, BurningSection, DoneSection, LessonsSection, PostsSection, WaitingSection } from "./TodayParts.js";

const pollMs = 3000;

// «Сегодня»: the day of the student (TASK-126). The core builds the screen (TASK-125); the live recording and
// recognition status of a lesson comes from the activity binding, the same one «Записи и задачи» reads. The former home
// screen, the recordings and tasks, is one click away and keeps working as it was.
export function TodayPage(props: { onOpen: (id: PageId) => void; onOpenAssignment: (id: string) => void }) {
  const api = useMemo(() => todayApi(callByName), []);
  const activity = useMemo(() => activityApi(callByName), []);
  const calendar = useMemo(() => ({ api: agentConfirmApi(callByName), deleteApi: recordingDeleteApi(callByName) }), []);
  const openSummary = useOpenSummary();
  const homework = useMemo(() => homeworkApi(callByName), []);
  const library = useMemo(() => libraryApi(callByName), []);
  const [adding, setAdding] = useState(false);
  const [all, setAll] = useState(false);
  const [screen, setScreen] = useState<TodayScreen | null>(null);
  const [error, setError] = useState("");
  const [recordings, setRecordings] = useState<RecordingItem[] | null>(null);
  const mounted = useRef(true);

  const requests = useRef(newestOnly());

  const load = useCallback(() => {
    const token = requests.current.next();
    api.today().then(
      (s) => {
        if (mounted.current && requests.current.isNewest(token)) {
          setScreen(s);
          setError("");
        }
      },
      (err: unknown) => mounted.current && requests.current.isNewest(token) && setError(todayErrorText(err)),
    );
  }, [api]);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // Read on each visit of the section and when the window gets back the focus.
  useEffect(() => {
    if (all) {
      return;
    }
    load();
    window.addEventListener("focus", load);
    return () => window.removeEventListener("focus", load);
  }, [all, load]);

  // The recordings list only for the live status of the lesson rows; a failed read leaves the rows without it.
  useEffect(() => {
    if (all) {
      return;
    }
    const read = () => {
      if (document.visibilityState === "visible") {
        activity.listRecordings().then(
          (r) => mounted.current && setRecordings(r),
          () => undefined,
        );
      }
    };
    read();
    const timer = setInterval(read, pollMs);
    return () => clearInterval(timer);
  }, [all, activity]);

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

  if (all) {
    return (
      <>
        <div className="today-back">
          <Button variant="ghost" icon="back" onClick={() => setAll(false)}>
            {texts.back}
          </Button>
        </div>
        <RecordingsPage onOpen={props.onOpen} onOpenAssignment={props.onOpenAssignment} />
      </>
    );
  }

  const queue = queueLine(recordings ?? [], []);
  const nearest = screen === null ? "" : nearestDeadlineText(screen.burning);

  return (
    <section className="page today">
      <div className="head">
        <div>
          <span className="label num">{screen === null ? "" : dayTitle(screen.day)}</span>
          <h1>{texts.title}</h1>
        </div>
        <div className="acts">
          <Button icon="retry" onClick={load}>
            {texts.refresh}
          </Button>
          <Button icon="rec" onClick={() => setAll(true)}>
            {texts.allRecords}
          </Button>
        </div>
      </div>
      {error !== "" && (
        <ResultLine kind="fail" role="alert">
          {error}
        </ResultLine>
      )}
      {adding && (
        <AddAssignmentDialog
          api={homework}
          library={library}
          onClose={() => setAdding(false)}
          onCreated={() => {
            setAdding(false);
            load(); // the new assignment shows in «Горит на 7 дней» when its deadline is near
          }}
        />
      )}
      {screen === null && error === "" && <p className="muted">{texts.loading}</p>}
      {screen !== null && (
        <>
          <AskSection chips={screen.chips} onChip={(text) => requestDraft(text)} />
          <div className="today-cols">
            <div>
              <LessonsSection
                lessons={screen.lessons}
                recordings={recordings ?? []}
                day={screen.day}
                onOpenSummary={openSummary}
                onOpenAll={() => setAll(true)}
              />
              <PostsSection posts={screen.posts} onOpenSummary={openSummary} calendar={calendar} />
              <DoneSection done={screen.done} onOpenSummary={openSummary} />
            </div>
            <div>
              <WaitingSection waiting={screen.waiting} onOpenAssignment={props.onOpenAssignment} />
              <BurningSection
                burning={screen.burning}
                onOpenAssignment={props.onOpenAssignment}
                onAdd={() => setAdding(true)}
              />
            </div>
          </div>
          <div className="statusbar">
            {queue !== "" && <span className="num">{queue}</span>}
            {nearest !== "" && <span className="num">{nearest}</span>}
            <span className="r">
              <Icon name="lock" />
              {statusTexts.local}
            </span>
          </div>
        </>
      )}
    </section>
  );
}
