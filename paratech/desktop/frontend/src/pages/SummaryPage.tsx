import { useEffect, useMemo, useRef, useState } from "react";
import { callByName } from "../bindings/runtime";
import { type SummaryApi, summaryApi, type SummarySaveFormat, type SummaryView } from "../bindings/summary";
import {
  errorText,
  headerMeta,
  nextPollMs,
  noSummaryHint,
  pageTitle,
  type RetryStep,
} from "../summaryView";
import { saveResultText } from "../transcriptView";
import { Button } from "../ui/Button";
import { Banner, Card, ResultLine, type ResultKind, Segmented } from "../ui/controls";
import { HomeworkCard, SummaryCard } from "./SummaryParts";

export interface SummaryPageProps {
  recordingId: string;
  onBack: () => void;
  onOpenTranscript: (recordingId: string) => void;
  onOpenAssignment: (assignmentId: string) => void;
  /** TASK-086: the retry binding; the retry buttons stay hidden while it is undefined. */
  onRetry?: (recordingId: string, step: RetryStep) => Promise<void>;
}

// «Конспект» of one recording (TASK-087): the lesson, the summary with its key points, the homework found and where
// both stand, with copy and save. Works offline: everything comes from the core on this computer. The summary is model
// output and is rendered as React text only.
export function SummaryPage(props: SummaryPageProps) {
  const { recordingId: id } = props;
  const api = useMemo(() => summaryApi(callByName), []);
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
  const onRetry = props.onRetry;
  const retry = onRetry && ((step: RetryStep) => onRetry(id, step).then(() => setRefresh((n) => n + 1)));

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
          view={view}
          onOpenTranscript={props.onOpenTranscript}
          onOpenAssignment={props.onOpenAssignment}
          onRetry={retry}
        />
      )}
    </section>
  );
}

interface Message {
  kind: ResultKind;
  text: string;
}

function Viewer(props: {
  api: SummaryApi;
  view: SummaryView;
  onOpenTranscript: (recordingId: string) => void;
  onOpenAssignment: (assignmentId: string) => void;
  onRetry?: (step: RetryStep) => Promise<void>;
}) {
  const { api, view } = props;
  const id = view.recordingId;
  const mounted = useRef(true);
  const acting = useRef(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<Message | null>(null);
  const [format, setFormat] = useState<SummarySaveFormat>("txt");

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function act(key: string, action: () => Promise<Message | null>) {
    if (acting.current) {
      return;
    }
    acting.current = true;
    setBusy(key);
    setMessage(null);
    try {
      const m = await action();
      if (mounted.current) {
        setMessage(m);
      }
    } catch (err) {
      if (mounted.current) {
        setMessage({ kind: "fail", text: errorText(err) });
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

  const onRetry = props.onRetry;
  const retry =
    onRetry &&
    ((step: RetryStep) =>
      void act(`retry:${step}`, async () => {
        await onRetry(step);
        return null;
      }));
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
          {view.transcribed && (
            <Button variant="ghost" icon="doc" onClick={() => props.onOpenTranscript(id)}>
              Открыть расшифровку
            </Button>
          )}
        </div>
        {message && (
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
      />
      <HomeworkCard
        view={view}
        onOpenAssignment={props.onOpenAssignment}
        onRetry={retry}
        retrying={retrying}
      />
    </>
  );
}
