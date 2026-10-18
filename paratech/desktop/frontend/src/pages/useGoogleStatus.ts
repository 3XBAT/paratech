import { useEffect, useMemo, useRef, useState } from "react";
import { calendarApi, type CalendarSyncState } from "../bindings/calendar";
import { googleApi, type GoogleState } from "../bindings/google";
import { callByName } from "../bindings/runtime";
import { calendarPollMs, calendarPolls } from "../calendarView";
import { pollStatus, statusPollMs } from "../googleView";

// The Google status for the card on the «Подключения» overview: GoogleStatus and GoogleCalendarStatus, which read the
// stored state and have no side effects. They are read again each time the overview shows (the Google page may have
// changed them), the calendar also when the account changes, and followed while a sign-in waits for the browser or a
// sync may still change its outcome.
export function useGoogleStatus(active: boolean): {
  state: GoogleState | null;
  loadFailed: boolean;
  calendar: CalendarSyncState | null;
} {
  const api = useMemo(() => googleApi(callByName), []);
  const cal = useMemo(() => calendarApi(callByName), []);
  const [state, setState] = useState<GoogleState | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [calendar, setCalendar] = useState<CalendarSyncState | null>(null);
  const mounted = useRef(true);
  // Only the latest request may set the state: a slow reply of an older one must not overwrite a newer answer.
  const seq = useRef(0);
  const calSeq = useRef(0);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (!active) {
      return;
    }
    const n = ++seq.current;
    setLoadFailed(false);
    api.status().then(
      (s) => {
        if (mounted.current && n === seq.current) {
          setState(s);
        }
      },
      () => {
        // The old state may be out of date (the Google page may have changed it): show the failure instead.
        if (mounted.current && n === seq.current) {
          setState(null);
          setLoadFailed(true);
        }
      },
    );
  }, [active, api]);

  // Read on each show and after a sign-in or a sign-out, which change what the calendar asks of the user. Before the
  // account is known the card has nothing to show it on.
  const account = state?.account ?? "";
  useEffect(() => {
    if (!active || account === "") {
      return;
    }
    const c = ++calSeq.current;
    cal.status().then(
      (s) => mounted.current && c === calSeq.current && setCalendar(s),
      // Without the calendar state the card shows the sign-in alone.
      () => mounted.current && c === calSeq.current && setCalendar(null),
    );
  }, [active, account, cal]);

  const poll = active && pollStatus(state, null);
  useEffect(() => {
    if (!poll) {
      return;
    }
    const timer = setInterval(() => {
      const n = ++seq.current;
      api.status().then(
        (s) => mounted.current && n === seq.current && setState(s),
        () => {}, // the next tick tries again
      );
    }, statusPollMs);
    return () => clearInterval(timer);
  }, [poll, api]);

  const calPoll = active && calendarPolls(account, calendar);
  useEffect(() => {
    if (!calPoll) {
      return;
    }
    const timer = setInterval(() => {
      const c = ++calSeq.current;
      cal.status().then(
        (s) => mounted.current && c === calSeq.current && setCalendar(s),
        () => {}, // the next tick tries again
      );
    }, calendarPollMs);
    return () => clearInterval(timer);
  }, [calPoll, cal]);

  return { state, loadFailed, calendar };
}
