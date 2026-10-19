import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { promptApi, type SourcesView } from "../bindings/prompt";
import { callByName } from "../bindings/runtime";
import { failedSources, SourcesPoller } from "./view";

const browserTimers = {
  set: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clear: (handle: unknown) => window.clearTimeout(handle as number),
};

/**
 * The open apps for a picker of the main window (TASK-081), listed again every refreshMs while active and the window
 * is visible. refresh() lists them at once (when the picker is shown again); remembered() shows a source the core has
 * just remembered without waiting for the next list.
 */
export function useSources(active: boolean): {
  sources: SourcesView | null;
  refresh: () => void;
  remembered: (app: string) => void;
} {
  const api = useMemo(() => promptApi(callByName), []);
  const [sources, setSources] = useState<SourcesView | null>(null);
  const [visible, setVisible] = useState(() => document.visibilityState === "visible");
  const mounted = useRef(true);

  const load = useCallback(async () => {
    let s: SourcesView | null = null;
    try {
      s = await api.sources();
    } catch {
      // A malformed or failed reply offers only the whole computer.
    }
    if (mounted.current) {
      setSources((prev) => s ?? failedSources(prev));
    }
  }, [api]);

  const poller = useMemo(() => new SourcesPoller(load, browserTimers), [load]);

  useEffect(() => {
    mounted.current = true;
    const onVisibility = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      mounted.current = false;
      document.removeEventListener("visibilitychange", onVisibility);
      poller.setActive(false);
    };
  }, [poller]);

  useEffect(() => {
    poller.setActive(active && visible);
  }, [active, visible, poller]);

  const refresh = useCallback(() => poller.refreshNow(), [poller]);
  const remembered = useCallback((app: string) => setSources((prev) => prev && { ...prev, remembered: app }), []);
  return { sources, refresh, remembered };
}
