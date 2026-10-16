import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { activityApi, type JobItem, type RecordingItem } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import {
  actionErrorText,
  formatTime,
  isFinishedJob,
  isFinishedRecording,
  jobTypeLabel,
  loadErrorText,
  recordingDurationText,
  splitFinished,
} from "../activityView";
import { CollapsibleList } from "../CollapsibleList";
import { type ListStates, stateOf, withState } from "../listView";
import { jobStatusView, queueText, recordingStatusView, showsProgress, type StatusView } from "../statusPresentation";
import { Button } from "../ui/Button";
import { Banner, EmptyState, Progress, Row } from "../ui/controls";
import { Icon } from "../ui/Icon";
import { StatusChip } from "../ui/StatusChip";

const pollMs = 3000;

// The page shows what the Go core reports; the Stop and Run now buttons appear only when the core offers them (ARCH-44).
export function RecordingsPage({ onOpenSchedule }: { onOpenSchedule?: () => void }) {
  const api = useMemo(() => activityApi(callByName), []);
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

  // Read on every render; the 3 s poll re-renders, so the elapsed time of a running recording advances with it.
  const now = Date.now();

  // Finished rows go to a folded list; rows in progress (and those with Stop or Run now) always stay in view.
  const rec = splitFinished(recordings ?? [], isFinishedRecording);
  const jb = splitFinished(jobs ?? [], isFinishedJob);

  // The rows of one list, in a card; CollapsibleList puts the finished ones into the same kind of card.
  const card = (rows: ReactNode) => (
    <div className="card" role="list">
      {rows}
    </div>
  );

  const recordingRow = (r: RecordingItem) => {
    const v = recordingStatusView(r, now);
    const stopping = busy.has(`stop:${r.id}`);
    return (
      <Row
        key={r.id}
        title={<span className="num">{formatTime(r.startedAt)}</span>}
        meta={v.value === "" && <span className="num">Длительность {recordingDurationText(r, now)}</span>}
        status={<StatusLine view={v} problem={r.problem} />}
        actions={
          r.stoppable && (
            <Button
              variant="danger"
              icon="stop"
              busy={stopping}
              busyLabel="Остановка…"
              disabled={stopping}
              onClick={() => void act(`stop:${r.id}`, () => api.stopRecording(r.id))}
            >
              Остановить
            </Button>
          )
        }
      />
    );
  };

  const jobRow = (j: JobItem) => {
    const v = jobStatusView(j);
    const starting = busy.has(`run:${j.id}`);
    return (
      <Row
        key={j.id}
        title={jobTypeLabel(j.type)}
        meta={
          <span className="num">
            Обновлена {formatTime(j.updatedAt)} · попыток: {j.attempts} из {j.maxAttempts}
          </span>
        }
        status={
          <StatusLine view={v} problem={j.lastError}>
            {showsProgress(j) && <Progress value={j.progressPercent} label={v.label} text={false} />}
          </StatusLine>
        }
        actions={
          j.runNowOffered && (
            <Button
              icon="play"
              busy={starting}
              busyLabel="Запуск…"
              disabled={starting}
              onClick={() => void act(`run:${j.id}`, () => api.runJobNow(j.id))}
            >
              Запустить сейчас
            </Button>
          )
        }
      />
    );
  };

  const queue = queueText(jobs ?? []);

  return (
    <section className="page">
      <div className="head">
        <h1>Записи и задачи</h1>
      </div>
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
      {recordings === null ? (
        !recordingsError && <p className="muted">Загрузка…</p>
      ) : recordings.length === 0 ? (
        <EmptyState
          label="Записей пока нет"
          title="Здесь появятся записи пар"
          actions={
            onOpenSchedule && (
              <Button variant="primary" icon="up" onClick={onOpenSchedule}>
                Импортировать расписание
              </Button>
            )
          }
        >
          Импортируйте расписание — перед каждой парой появится окно с кнопкой «Начать запись».
        </EmptyState>
      ) : (
        <>
          <h2 className="group-title">Записи</h2>
          {rec.active.length > 0 && card(rec.active.map(recordingRow))}
          {rec.finished.length > 0 && (
            <CollapsibleList
              title="Завершённые"
              items={rec.finished}
              itemKey={(r) => r.id}
              renderItem={recordingRow}
              renderContainer={card}
              state={stateOf(lists, "recordings")}
              onStateChange={(s) => setLists((l) => withState(l, "recordings", s))}
              className="clist-card"
              classNames={{ button: "btn ghost" }}
            />
          )}
        </>
      )}

      <h2 className="group-title">Задачи</h2>
      {jobsError && (
        <Banner kind="fail" role="alert">
          {jobsError}
        </Banner>
      )}
      {jobs === null ? (
        !jobsError && <p className="muted">Загрузка…</p>
      ) : jobs.length === 0 ? (
        <p className="muted">Задач пока нет.</p>
      ) : (
        <>
          {jb.active.length > 0 && card(jb.active.map(jobRow))}
          {jb.finished.length > 0 && (
            <CollapsibleList
              title="Завершённые"
              items={jb.finished}
              itemKey={(j) => j.id}
              renderItem={jobRow}
              renderContainer={card}
              state={stateOf(lists, "jobs")}
              onStateChange={(s) => setLists((l) => withState(l, "jobs", s))}
              className="clist-card"
              classNames={{ button: "btn ghost" }}
            />
          )}
        </>
      )}

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

// The chip, its hint and the core's note about the row (a problem or the last error).
function StatusLine(props: { view: StatusView; problem: string; children?: ReactNode }) {
  const v = props.view;
  return (
    <>
      <StatusChip kind={v.kind} icon={v.icon}>
        {v.label}
        {v.value !== "" && <span className="num"> · {v.value}</span>}
      </StatusChip>
      {props.children}
      {v.hint !== "" && <span className="small muted">{v.hint}</span>}
      {props.problem !== "" && <span className="small row-problem">{props.problem}</span>}
    </>
  );
}
