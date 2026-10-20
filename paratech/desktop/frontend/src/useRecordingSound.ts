import { useLayoutEffect, useMemo, useState } from "react";
import type { RecordingSound } from "./bindings/activity";
import { SoundPoller } from "./soundLevelView";

const browserTimers = {
  set: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clear: (handle: unknown) => window.clearTimeout(handle as number),
};

/**
 * The live sound of the running recording (TASK-099), read every 250 ms while `active`; null when none or a read
 * failed. untilIdle pauses after a reply without a recording until refresh (the popup calls it when it is shown).
 */
export function useRecordingSound(
  read: () => Promise<RecordingSound>,
  active: boolean,
  untilIdle = false,
): { sound: RecordingSound | null; refresh: () => void } {
  const [sound, setSound] = useState<RecordingSound | null>(null);
  const poller = useMemo(() => new SoundPoller(read, setSound, browserTimers, untilIdle), [read, untilIdle]);

  useLayoutEffect(() => {
    poller.setActive(active);
    return () => poller.setActive(false);
  }, [poller, active]);

  return { sound, refresh: useMemo(() => () => poller.refreshNow(), [poller]) };
}
