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
    api.status().then(
      (s) => {
        if (mounted.current) {
          setState(s);
          setLoadFailed(false);
        }
      },
      () => mounted.current && setLoadFailed(true),
    );
  }, [active, api]);

  const poll = active && pollStatus(state, null);
  useEffect(() => {
    if (!poll) {
      return;
    }
    const timer = setInterval(() => {
      api.status().then(
        (s) => mounted.current && setState(s),
        () => {}, // the next tick tries again
      );
    }, statusPollMs);
    return () => clearInterval(timer);
  }, [poll, api]);

  return { state, loadFailed };
}
