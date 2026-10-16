import { useEffect, useMemo, useRef, useState } from "react";
import { googleApi, type GoogleState } from "../bindings/google";
import { callByName } from "../bindings/runtime";
import { pollStatus, statusPollMs } from "../googleView";

// The Google status for the card on the «Подключения» overview: only GoogleStatus, which reads the stored state and
// has no side effects. It is read again each time the overview shows (the Google page may have changed it) and
// followed while a sign-in waits for the browser.
export function useGoogleStatus(active: boolean): { state: GoogleState | null; loadFailed: boolean } {
  const api = useMemo(() => googleApi(callByName), []);
  const [state, setState] = useState<GoogleState | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const mounted = useRef(true);
  // Only the latest request may set the state: a slow reply of an older one must not overwrite a newer answer.
  const seq = useRef(0);

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

  return { state, loadFailed };
}
