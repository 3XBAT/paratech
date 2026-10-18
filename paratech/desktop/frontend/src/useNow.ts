import { useLayoutEffect, useMemo, useState } from "react";
import { SecondTicker } from "./clock";

const browserTimers = {
  set: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clear: (handle: unknown) => window.clearTimeout(handle as number),
};

/** The clock, re-read every second while `active` (a recording runs); the timer stops when inactive or unmounted. */
export function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  const ticker = useMemo(() => new SecondTicker(setNow, browserTimers), []);

  // A layout effect: a row that has just started never paints with a stale clock.
  useLayoutEffect(() => {
    ticker.setActive(active);
    return () => ticker.setActive(false);
  }, [ticker, active]);

  return now;
}
