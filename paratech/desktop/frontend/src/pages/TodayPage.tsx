import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { activityApi, type RecordingItem } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import { type TodayScreen, todayApi, todayErrorText } from "../bindings/today";
import { requestDraft } from "../agentDraft";
import type { PageId } from "../nav";
import { useOpenSummary } from "../openSummary";
import { dayTitle, stagesKey, texts } from "../todayView";
import { Button } from "../ui/Button";
import { ResultLine } from "../ui/controls";
import { RecordingsPage } from "./RecordingsPage";
import { AskSection, BurningSection, DoneSection, LessonsSection, PostsSection, WaitingSection } from "./TodayParts";

const pollMs = 3000;

// «Сегодня»: the day of the student (TASK-126). The core builds the screen (TASK-125); the live recording and
// recognition status of a lesson comes from the activity binding, the same one «Записи и задачи» reads. The former home
// screen, the recordings and tasks, is one click away and keeps working as it was.
export function TodayPage(props: { onOpen: (id: PageId) => void; onOpenAssignment: (id: string) => void }) {
  const api = useMemo(() => todayApi(callByName), []);
  const activity = useMemo(() => activityApi(callByName), []);
  const openSummary = useOpenSummary();
  const [all, setAll] = useState(false);
  const [screen, setScreen] = useState<TodayScreen | null>(null);
  const [error, setError] = useState("");
  const [recordings, setRecordings] = useState<RecordingItem[]>([]);
  const mounted = useRef(true);

  const load = useCallback(() => {
    api.today().then(
      (s) => {
        if (mounted.current) {
          setScreen(s);
          setError("");
        }
      },
      (err: unknown) => mounted.current && setError(todayErrorText(err)),
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

  // A recording that starts, stops or finishes changes the day: the screen is read again.
  const stages = stagesKey(recordings);
  const firstStages = useRef(true);
  useEffect(() => {
    if (firstStages.current) {
      firstStages.current = false;
      return;
    }
    if (!all) {
      load();
    }
  }, [stages, all, load]);

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
      {screen === null && error === "" && <p className="muted">{texts.loading}</p>}
      {screen !== null && (
        <>
          <AskSection chips={screen.chips} onChip={(text) => requestDraft(text)} />
          <div className="today-cols">
            <div>
              <LessonsSection lessons={screen.lessons} recordings={recordings} onOpenSummary={openSummary} />
              <PostsSection posts={screen.posts} onOpenSummary={openSummary} />
              <DoneSection done={screen.done} onOpenSummary={openSummary} />
            </div>
            <div>
              <WaitingSection waiting={screen.waiting} />
              <BurningSection burning={screen.burning} onOpenAssignment={props.onOpenAssignment} />
            </div>
          </div>
        </>
      )}
    </section>
  );
}
