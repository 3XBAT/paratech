import { useEffect, useMemo, useRef, useState } from "react";
import { actionErrorText } from "../activityView";
import { type ActivityApi, activityApi } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import {
  type LectureTextApi,
  lectureTextApi,
  type SummaryApi,
  summaryApi,
  type SummarySaveFormat,
  type SummaryView,
} from "../bindings/summary";
import { lectureCopiedText } from "../lectureTextView";
import type { SummarySection } from "../openSummary";
import {
  errorText,
  headerMeta,
  nextPollMs,
  noSummaryHint,
  pageTitle,
  redoTexts,
  retryAction,
  retryClick,
  type RetryStep,
} from "../summaryView";
import { saveResultText } from "../transcriptView";
import { Button } from "../ui/Button";
import { Banner, Card, Dialog, ResultLine, type ResultKind, Segmented } from "../ui/controls";
import { LectureTextCard } from "./LectureTextParts";
import { HomeworkCard, SummaryCard } from "./SummaryParts";

export interface SummaryPageProps {
  recordingId: string;
  /** "lecture": scroll to the lecture text once loaded (the row's «Открыть текст лекции»). */
  section?: SummarySection;
  onBack: () => void;
  onOpenTranscript: (recordingId: string) => void;
  onOpenAssignment: (assignmentId: string) => void;
}

// «Конспект» of one recording (TASK-087): the lesson, the summary with its key points, the homework found, the lecture
// text (TASK-090) and where they stand, with copy and save. Works offline: everything comes from the core on this
// computer. The summary and the lecture text are model output and are rendered as React text only.
export function SummaryPage(props: SummaryPageProps) {
  const { recordingId: id } = props;
  const api = useMemo(() => summaryApi(callByName), []);
  const activity = useMemo(() => activityApi(callByName), []);
  const lecture = useMemo(() => lectureTextApi(callByName), []);
  const [view, setView] = useState<SummaryView | null>(null);
  const [error, setError] = useState("");
  // A new value starts the polling again (after a retry the steps go on).
  const [refresh, setRefresh] = useState(0);

  // One call at a time: the next view is asked for only after the previous answer.
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let last: SummaryView | null = null;
    let errors = 0;

    const tick = async () => {
      try {
        const v = await api.view(id);
        if (stopped) {
          return;
        }
        last = v;
        errors = 0;
        setView(v);
        setError("");
      } catch (err) {
        if (stopped) {
          return;
        }
        errors++;
        setError(errorText(err));
      }
      const wait = stopped ? null : nextPollMs(last, errors);
      if (wait !== null) {
        timer = setTimeout(() => void tick(), wait);
      }
    };
    void tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [api, id, refresh]);

  const meta = view ? headerMeta(view) : "";
  // After a started retry or redo the steps go on: ask for the view again.
  const reload = () => setRefresh((n) => n + 1);

  return (
    <section className="page tr sm">
      <div className="hw-back">
        <Button variant="ghost" onClick={props.onBack}>
          Назад
        </Button>
      </div>
      <div className="head">
        <div>
          <h1>Конспект</h1>
          {view && (
            <p className="tr-sub">
              <span className="tr-title">{pageTitle(view)}</span>
              {meta !== "" && <span className="muted num"> · {meta}</span>}
            </p>
          )}
        </div>
      </div>
      {error && (
        <Banner kind="fail" role="alert">
          {error}
        </Banner>
      )}
      {view === null ? (
        !error && <p className="muted">Загрузка…</p>
      ) : (
        <Viewer
          api={api}
          lecture={lecture}
          view={view}
          section={props.section}
          onOpenTranscript={props.onOpenTranscript}
          onOpenAssignment={props.onOpenAssignment}
          activity={activity}
          onStarted={reload}
        />
      )}
    </section>
  );
}

interface Message {
  kind: ResultKind;
  text: string;
}

// The actions of the lecture card have keys "lecture:<action>" and show their result in that card.
const lecturePrefix = "lecture:";

function Viewer(props: {
  api: SummaryApi;
  lecture: LectureTextApi;
  view: SummaryView;
  section?: SummarySection;
  onOpenTranscript: (recordingId: string) => void;
  onOpenAssignment: (assignmentId: string) => void;
  activity: ActivityApi;
  onStarted: () => void;
}) {
  const { api, view } = props;
  const id = view.recordingId;
  const mounted = useRef(true);
  const acting = useRef(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<(Message & { lecture: boolean }) | null>(null);
  const [format, setFormat] = useState<SummarySaveFormat>("txt");
  const [lectureFormat, setLectureFormat] = useState<SummarySaveFormat>("txt");
  const [askingRedo, setAskingRedo] = useState(false);
  const lectureRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // Opened for the lecture text: scroll to it once, when the first view is shown.
  useEffect(() => {
    if (props.section === "lecture") {
      lectureRef.current?.scrollIntoView({ block: "start" });
    }
  }, [props.section]);

  async function act(key: string, action: () => Promise<Message | null>) {
    if (acting.current) {
      return;
    }
    acting.current = true;
    const lecture = key.startsWith(lecturePrefix);
    setBusy(key);
    setMessage(null);
    try {
      const m = await action();
      if (mounted.current) {
        setMessage(m && { ...m, lecture });
      }
    } catch (err) {
      if (mounted.current) {
        setMessage({ kind: "fail", text: errorText(err), lecture });
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setBusy("");
      }
    }
  }

  const copy = () =>
    act("copy", async () => {
      await api.copyAll(id);
      return { kind: "ok", text: "Конспект скопирован в буфер обмена." };
    });

  const save = () =>
    act("save", async () => {
      const r = saveResultText(await api.saveAs(id, format));
      return r && { kind: r.ok ? "ok" : "fail", text: r.text };
    });

  const copyLecture = () =>
    act(`${lecturePrefix}copy`, async () => {
      await props.lecture.copy(id);
      return { kind: "ok", text: lectureCopiedText };
    });

  const saveLecture = () =>
    act(`${lecturePrefix}save`, async () => {
      const r = saveResultText(await props.lecture.saveAs(id, lectureFormat));
      return r && { kind: r.ok ? "ok" : "fail", text: r.text };
    });

  // Retry and redo report failures with the recordings screen's texts: they are the activity binding's errors.
  const start = (step: RetryStep | "lectureText", call: () => Promise<void>) =>
    act(step === "lectureText" ? `${lecturePrefix}start` : `retry:${step}`, async () => {
      let refused: Message | null = null;
      try {
        await call();
      } catch (err) {
        refused = { kind: "fail", text: actionErrorText(err) };
      }
      // Reload on a refusal too: it usually means a run started elsewhere (the row) and the polling has stopped.
      props.onStarted();
      return refused;
    });

  const retry = (step: RetryStep) =>
    retryClick(view, step, {
      retry: (work) => void start(work, () => props.activity.retryPostProcessing(id, work)),
      askRedo: () => setAskingRedo(true),
    });

  const redo = () => {
    setAskingRedo(false);
    void start("summary", () => props.activity.redoSummary(id));
  };

  // The dialog closes by itself once the summary is no longer one to make again (a new run started meanwhile), and
  // does not come back on its own when that run is done.
  const redoOffered = retryAction(view, "summary")?.kind === "redo";
  useEffect(() => {
    if (!redoOffered) {
      setAskingRedo(false);
    }
  }, [redoOffered]);

  const retrying: RetryStep | "" = busy === "retry:summary" ? "summary" : busy === "retry:homework" ? "homework" : "";

  const ready = view.summary.ready;
  const idle = busy === "";
  const noText = ready ? undefined : noSummaryHint;

  return (
    <>
      <Card className="box tr-tools">
        <div className="tr-share">
          <Button
            busy={busy === "copy"}
            busyLabel="Копирование…"
            disabled={!idle || !ready}
            disabledReason={idle ? noText : undefined}
            onClick={() => void copy()}
          >
            Скопировать конспект
          </Button>
          <Segmented
            label="Формат файла"
            options={[
              { value: "txt", label: ".txt" },
              { value: "md", label: ".md" },
            ]}
            value={format}
            onChange={setFormat}
          />
          <Button
            icon="down"
            busy={busy === "save"}
            busyLabel="Сохранение…"
            disabled={!idle || !ready}
            disabledReason={idle ? noText : undefined}
            onClick={() => void save()}
          >
            Сохранить как…
          </Button>
          {/* With a lecture text the transcript is only its source: the small link in the lecture card. */}
          {view.transcribed && !view.lectureText.ready && (
            <Button variant="ghost" icon="doc" onClick={() => props.onOpenTranscript(id)}>
              Открыть расшифровку
            </Button>
          )}
        </div>
        {message && !message.lecture && (
          <ResultLine kind={message.kind} role={message.kind === "fail" ? "alert" : "status"}>
            {message.text}
          </ResultLine>
        )}
        <p className="small muted tr-note">
          Конспект хранится на этом компьютере. Копирование и сохранение — только по кнопке, в сеть ничего не уходит.
        </p>
      </Card>

      <SummaryCard
        view={view}
        onRetry={retry}
        retrying={retrying}
        retryDisabled={!idle}
      />
      <HomeworkCard
        view={view}
        onOpenAssignment={props.onOpenAssignment}
        onRetry={retry}
        retrying={retrying}
        retryDisabled={!idle}
      />
      <div ref={lectureRef}>
        <LectureTextCard
          view={view}
          busy={busy.startsWith(lecturePrefix) ? busy.slice(lecturePrefix.length) : ""}
          disabled={!idle}
          format={lectureFormat}
          onFormat={setLectureFormat}
          onCopy={() => void copyLecture()}
          onSave={() => void saveLecture()}
          onStart={() => void start("lectureText", () => props.activity.retryPostProcessing(id, "lectureText"))}
          onOpenTranscript={() => props.onOpenTranscript(id)}
          message={message?.lecture ? message : null}
        />
      </div>
      <Dialog
        open={askingRedo && redoOffered}
        title={redoTexts.title}
        confirmLabel={redoTexts.confirm}
        cancelLabel={redoTexts.cancel}
        confirmDisabled={!idle}
        onConfirm={redo}
        onCancel={() => setAskingRedo(false)}
      >
        <p>{redoTexts.body}</p>
      </Dialog>
    </>
  );
}
