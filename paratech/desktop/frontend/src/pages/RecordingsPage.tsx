import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { activityApi, type JobItem, type RecordingItem } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import {
  actionErrorText,
  formatTime,
  isFinishedJob,
  isFinishedRecording,
  jobStatusLabel,
  jobTypeLabel,
  loadErrorText,
  recordingDurationText,
  recordingStatusLabel,
  splitFinished,
} from "../activityView";
import { CollapsibleList } from "../CollapsibleList";
import { type ListStates, stateOf, withState } from "../listView";

const pollMs = 3000;

// The page shows what the Go core reports; the Stop and Run now buttons appear only when the core offers them (ARCH-44).
export function RecordingsPage() {
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

  const recordingsTable = (rows: ReactNode) => (
    <table className="list">
      <thead>
        <tr>
          <th>Начало</th>
          <th>Статус</th>
          <th>Длительность</th>
          <th>Примечание</th>
          <th />
        </tr>
      </thead>
      <tbody>{rows}</tbody>
    </table>
  );

  const recordingRow = (r: RecordingItem) => (
    <tr key={r.id}>
      <td>{formatTime(r.startedAt)}</td>
      <td>{recordingStatusLabel(r.status)}</td>
      <td>{recordingDurationText(r, now)}</td>
      <td className="problem">{r.problem}</td>
      <td>
        {r.stoppable && (
          <button
            type="button"
            disabled={busy.has(`stop:${r.id}`)}
            onClick={() => void act(`stop:${r.id}`, () => api.stopRecording(r.id))}
          >
            {busy.has(`stop:${r.id}`) ? "Остановка…" : "Остановить"}
          </button>
        )}
      </td>
    </tr>
  );

  const jobsTable = (rows: ReactNode) => (
    <table className="list">
      <thead>
        <tr>
          <th>Задача</th>
          <th>Статус</th>
          <th>Попытки</th>
          <th>Обновлена</th>
          <th>Ошибка</th>
          <th />
        </tr>
      </thead>
      <tbody>{rows}</tbody>
    </table>
  );

  const jobRow = (j: JobItem) => (
    <tr key={j.id}>
      <td>{jobTypeLabel(j.type)}</td>
      <td className={j.waiting ? "waiting" : ""}>
        {jobStatusLabel(j.status)}
        {j.status === "RUNNING" && j.progressPercent > 0 && ` (${j.progressPercent}%)`}
      </td>
      <td>
        {j.attempts}/{j.maxAttempts}
      </td>
      <td>{formatTime(j.updatedAt)}</td>
      <td className="problem">{j.lastError}</td>
      <td>
        {j.runNowOffered && (
          <button
            type="button"
            disabled={busy.has(`run:${j.id}`)}
            onClick={() => void act(`run:${j.id}`, () => api.runJobNow(j.id))}
          >
            {busy.has(`run:${j.id}`) ? "Запуск…" : "Запустить сейчас"}
          </button>
        )}
      </td>
    </tr>
  );

  return (
    <section>
      <h1>Записи</h1>
      {actionError && (
        <p className="error" role="alert">
          {actionError}
        </p>
      )}

      {recordingsError && (
        <p className="error" role="alert">
          {recordingsError}
        </p>
      )}
      {recordings === null ? (
        !recordingsError && <p className="muted">Загрузка…</p>
      ) : recordings.length === 0 ? (
        <p className="muted">Записей пока нет.</p>
      ) : (
        <>
          {rec.active.length > 0 && recordingsTable(rec.active.map(recordingRow))}
          {rec.finished.length > 0 && (
            <CollapsibleList
              title="Завершённые"
              items={rec.finished}
              itemKey={(r) => r.id}
              renderItem={recordingRow}
              renderContainer={recordingsTable}
              state={stateOf(lists, "recordings")}
              onStateChange={(s) => setLists((l) => withState(l, "recordings", s))}
            />
          )}
        </>
      )}

      <h2>Задачи</h2>
      {jobsError && (
        <p className="error" role="alert">
          {jobsError}
        </p>
      )}
      {jobs === null ? (
        !jobsError && <p className="muted">Загрузка…</p>
      ) : jobs.length === 0 ? (
        <p className="muted">Задач пока нет.</p>
      ) : (
        <>
          {jb.active.length > 0 && jobsTable(jb.active.map(jobRow))}
          {jb.finished.length > 0 && (
            <CollapsibleList
              title="Завершённые"
              items={jb.finished}
              itemKey={(j) => j.id}
              renderItem={jobRow}
              renderContainer={jobsTable}
              state={stateOf(lists, "jobs")}
              onStateChange={(s) => setLists((l) => withState(l, "jobs", s))}
            />
          )}
        </>
      )}
    </section>
  );
}
