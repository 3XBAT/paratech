import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { activityApi, type JobItem, type RecordingItem } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import {
  actionErrorText,
  formatDuration,
  formatTime,
  jobStatusLabel,
  jobTypeLabel,
  loadErrorText,
  recordingStatusLabel,
} from "../activityView";

const pollMs = 3000;

// The page shows what the Go core reports; the Stop and Run now buttons appear only when the core offers them (ARCH-44).
export function RecordingsPage() {
  const api = useMemo(() => activityApi(callByName), []);
  const [recordings, setRecordings] = useState<RecordingItem[] | null>(null);
  const [jobs, setJobs] = useState<JobItem[] | null>(null);
  const [loadError, setLoadError] = useState("");
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
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
        try {
          const [r, j] = await Promise.all([api.listRecordings(), api.listJobs()]);
          if (mounted.current) {
            setRecordings(r);
            setJobs(j);
            setLoadError("");
          }
        } catch (err) {
          if (mounted.current) {
            setLoadError(loadErrorText(err));
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

  return (
    <section>
      <h1>Записи</h1>
      {loadError && (
        <p className="error" role="alert">
          {loadError}
        </p>
      )}
      {actionError && (
        <p className="error" role="alert">
          {actionError}
        </p>
      )}

      {recordings === null ? (
        !loadError && <p className="muted">Загрузка…</p>
      ) : recordings.length === 0 ? (
        <p className="muted">Записей пока нет.</p>
      ) : (
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
          <tbody>
            {recordings.map((r) => (
              <tr key={r.id}>
                <td>{formatTime(r.startedAt)}</td>
                <td>{recordingStatusLabel(r.status)}</td>
                <td>{r.hasDuration ? formatDuration(r.durationSeconds) : "—"}</td>
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
            ))}
          </tbody>
        </table>
      )}

      <h2>Задачи</h2>
      {jobs === null ? (
        !loadError && <p className="muted">Загрузка…</p>
      ) : jobs.length === 0 ? (
        <p className="muted">Задач пока нет.</p>
      ) : (
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
          <tbody>
            {jobs.map((j) => (
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
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
