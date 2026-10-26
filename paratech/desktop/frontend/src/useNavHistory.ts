import { useEffect, useRef } from "react";
import type { Route } from "./nav";
import {
  currentRoute,
  type Direction,
  goBack,
  goForward,
  keyDirection,
  mouseDirection,
  newHistory,
  pushRoute,
} from "./navHistory";

/**
 * Records every route change into the history and walks it on Alt+←/Alt+→ and the mouse side buttons. A restored
 * route is already the current entry, so the effect adds nothing for it. The events are cancelled so the WebView
 * never navigates away from the app.
 */
export function useNavHistory(route: Route, setRoute: (r: Route) => void, onRestore: (r: Route) => void): void {
  const history = useRef(newHistory(route));
  const restore = useRef(onRestore);
  restore.current = onRestore;

  useEffect(() => {
    history.current = pushRoute(history.current, route);
  }, [route]);

  useEffect(() => {
    const go = (d: Direction) => {
      const next = d === "back" ? goBack(history.current) : goForward(history.current);
      if (next === history.current) {
        return;
      }
      history.current = next;
      const r = currentRoute(next);
      setRoute(r);
      restore.current(r);
    };
    const onKey = (e: KeyboardEvent) => {
      const d = keyDirection(e);
      if (d !== null) {
        e.preventDefault();
        go(d);
      }
    };
    const onMouse = (e: MouseEvent) => {
      const d = mouseDirection(e.button);
      if (d === null) {
        return;
      }
      e.preventDefault();
      if (e.type === "mouseup") {
        go(d);
      }
    };
    window.addEventListener("keydown", onKey);
    for (const t of ["mousedown", "mouseup", "auxclick"]) {
      window.addEventListener(t, onMouse as EventListener);
    }
    return () => {
      window.removeEventListener("keydown", onKey);
      for (const t of ["mousedown", "mouseup", "auxclick"]) {
        window.removeEventListener(t, onMouse as EventListener);
      }
    };
  }, [setRoute]);
}
