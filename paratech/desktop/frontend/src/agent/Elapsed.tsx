import { useEffect, useRef, useState } from "react";
import { elapsedLabel, elapsedSeconds } from "../agentElapsed.js";

/** One tick per second; the label is computed from the timestamps, so a late tick never drifts it. */
const tickMs = 1000;

/**
 * The time a request has been running, muted, beside the waiting dots. Only this small element re-renders each second.
 * It is hidden from screen readers and is no live region: the dots' own «Агент отвечает…» is the one accessible status,
 * so nothing is announced every second. While the window is hidden nothing is re-rendered; on return it catches up at once.
 */
export function Elapsed(props: { since: number; clock?: () => number }) {
  const clock = useRef(props.clock ?? Date.now);
  clock.current = props.clock ?? Date.now;
  const [now, setNow] = useState(() => clock.current());
  useEffect(() => {
    const tick = () => {
      if (!document.hidden) {
        setNow(clock.current());
      }
    };
    tick();
    const id = window.setInterval(tick, tickMs);
    document.addEventListener("visibilitychange", tick);
    return () => {
      window.clearInterval(id);
      document.removeEventListener("visibilitychange", tick);
    };
  }, []);
  // Nothing for the first second: «0с» would flash and vanish; the first value read is «1с».
  if (elapsedSeconds(props.since, now) < 1) {
    return null;
  }
  return (
    <span className="ag-elapsed small muted" aria-hidden="true">
      {elapsedLabel(props.since, now)}
    </span>
  );
}
