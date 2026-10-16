import { useEffect, useMemo, useRef, useState } from "react";
import { calendarApi, type CalendarSyncState } from "../bindings/calendar";
import { callByName } from "../bindings/runtime";
import { calendarPollMs, calendarPolls, calendarSyncView, calendarTexts } from "../calendarView";
import { Button } from "../ui/Button";
import { ResultLine } from "../ui/controls";

interface Message {
  text: string;
  ok: boolean;
}

// The calendar sync part of the «Google Календарь» block: Go's status line, the last sync time and «Синхронизировать».
// It follows the account of the block: the status is read again after a sign-in or a sign-out.
export function CalendarSync(props: { account: string }) {
  const api = useMemo(() => calendarApi(callByName), []);
  const [status, setStatus] = useState<CalendarSyncState | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [message, setMessage] = useState<Message | null>(null);
  const mounted = useRef(true);

  // A double click must not start a second call before the disabled button renders.
  const running = useRef(false);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    setMessage(null);
    api.status().then(
      (s) => {
        if (mounted.current) {
          setStatus(s);
          setLoadFailed(false);
        }
      },
      () => mounted.current && setLoadFailed(true),
    );
  }, [api, props.account]);

  const poll = calendarPolls(props.account, status);
  useEffect(() => {
    if (!poll) {
      return;
    }
    const timer = setInterval(() => {
      api.status().then(
        (s) => mounted.current && setStatus(s),
        () => {}, // the next tick tries again
      );
    }, calendarPollMs);
    return () => clearInterval(timer);
  }, [poll, api]);

  async function sync() {
    if (running.current) {
      return;
    }
    running.current = true;
    setSyncing(true);
    setMessage(null);
    try {
      const r = await api.sync();
      if (mounted.current) {
        setStatus(r.state);
        setMessage({ text: r.message, ok: r.ok });
      }
    } catch {
      if (mounted.current) {
        setMessage({ text: calendarTexts.actionFailed, ok: false });
      }
    } finally {
      running.current = false;
      if (mounted.current) {
        setSyncing(false);
      }
    }
  }

  const v = calendarSyncView({ account: props.account, status, loadFailed, inFlight: syncing });
  if (!v.shown) {
    return null;
  }

  return (
    <>
      <hr className="sep" />
      <h3>{calendarTexts.title}</h3>
      <p className="small" role="status">
        {v.message}
      </p>
      {v.lastSynced !== "" && <p className="small muted">{v.lastSynced}</p>}
      {v.syncShown && (
        <div className="acts start">
          <Button
            disabled={v.syncDisabled}
            disabledReason={v.syncDisabledReason || undefined}
            busy={syncing}
            busyLabel={calendarTexts.syncing}
            onClick={() => void sync()}
          >
            {calendarTexts.sync}
          </Button>
        </div>
      )}
      {message !== null && (
        <ResultLine kind={message.ok ? "ok" : "fail"} role={message.ok ? "status" : "alert"}>
          {message.text}
        </ResultLine>
      )}
    </>
  );
}
