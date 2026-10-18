import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { activityApi, type JobItem, type RecordingItem } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import { actionErrorText, formatTime, isFinishedJob, jobTypeLabel, loadErrorText, splitFinished } from "../activityView";
import { CollapsibleList } from "../CollapsibleList";
import { type ListStates, stateOf, withState } from "../listView";
import type { PageId } from "../nav";
import { anyRecording, isFinishedRow, queueLine } from "../recordingRowView";
import { jobStatusView, showsProgress } from "../statusPresentation";
import { Button } from "../ui/Button";
import { SpeechComponentNotice } from "./SpeechComponentNotice";
import { Banner, EmptyState, Progress, Row } from "../ui/controls";
import { Icon } from "../ui/Icon";
import { useNow } from "../useNow";
import { RecordingRow, StatusLine } from "./RecordingRow";

const pollMs = 3000;

// The page shows what the Go core reports: one row per recording with the status of its whole chain (TASK-076). The
// Stop, Run now and link buttons appear only when the core offers them (ARCH-44).
export function RecordingsPage({ onOpen }: { onOpen?: (page: PageId) => void }) {
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

  // The elapsed time of a running recording ticks every second from startedAt, independent of the 3 s poll.
  const now = useNow(anyRecording(recordings ?? []));

  // Finished rows go to the one folded group; rows in progress (and those with Stop or Run now) always stay in view.
  const rec = splitFinished(recordings ?? [], isFinishedRow);
  // The jobs left in this list (homework drafts, emails) are shown only while unfinished: their results are on
  // «Домашние задания», and the page keeps a single «Завершённые» group.
  const otherJobs = (jobs ?? []).filter((j) => !isFinishedJob(j));

  // The rows of one list, in a card; CollapsibleList puts the finished ones into the same kind of card.
  const card = (rows: ReactNode) => (
    <div className="card rows" role="list">
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
    />
  );

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
          <StatusLine view={v} notes={j.lastError === "" ? [] : [j.lastError]}>
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

  const queue = queueLine(recordings ?? [], jobs ?? []);

  return (
    <section className="page recordings">
      <div className="head">
        <h1>Записи и задачи</h1>
      </div>
      <SpeechComponentNotice onReady={() => void refresh()} />
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

      {jobsError && (
        <Banner kind="fail" role="alert">
          {jobsError}
        </Banner>
      )}
      {otherJobs.length > 0 && (
        <>
          <h2 className="group-title">Другие задачи</h2>
          {card(otherJobs.map(jobRow))}
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
