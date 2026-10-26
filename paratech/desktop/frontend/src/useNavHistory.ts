import { useEffect, useRef } from "react";
import type { Route } from "./nav";
import { currentRoute, handleKey, handleMouse, type NavAction, newHistory, pushRoute } from "./navHistory";

/**
 * Records every route change into the history and walks it on Alt+←/Alt+→ and the mouse side buttons. A restored
 * route is already the current entry, so the effect adds nothing for it. The events are cancelled so the WebView
 * never navigates away from the app.
 */
export function useNavHistory(route: Route, setRoute: (r: Route) => void, onRestore: (r: Route, from: Route) => void): void {
  const history = useRef(newHistory(route));
  const restore = useRef(onRestore);
  restore.current = onRestore;

  useEffect(() => {
    history.current = pushRoute(history.current, route);
  }, [route]);

  useEffect(() => {
    const apply = (e: Event, a: NavAction) => {
      if (a.prevent) {
        e.preventDefault();
      }
      if (a.next === null) {
        return;
      }
      const from = currentRoute(history.current);
      history.current = a.next;
      const r = currentRoute(a.next);
      setRoute(r);
      restore.current(r, from);
    };
    const onKey = (e: KeyboardEvent) => apply(e, handleKey(e, history.current));
    const onMouse = (e: MouseEvent) => apply(e, handleMouse(e, history.current));
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
