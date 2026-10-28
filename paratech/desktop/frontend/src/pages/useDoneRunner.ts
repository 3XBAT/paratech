import { useCallback, useEffect, useRef, useState } from "react";
import { type ActiveIssue, type DoneAction, doneOutcome, doneTexts, type ToastModel } from "../assignmentDoneView.js";
import type { DoneResult, HomeworkApi } from "../bindings/homework.js";
import { flightGuard } from "../homeworkView.js";
import type { ToastView } from "../ui/Toast.js";

type Run = (action: DoneAction, ids: string[], call: () => Promise<DoneResult>) => Promise<void>;

/**
 * «Выполнено» of a page (list, assignment, «Сегодня»): the calls, their guard, the one toast and the refusal line.
 * The page gives apply (its rows after the result, at once) and refresh (read the screen again). The decisions are in
 * assignmentDoneView.ts; this is only the state around them. «Отменить» sends back exactly what the call returned.
 */
export function useDoneRunner(
  api: Pick<HomeworkApi, "markDone" | "restoreDone">,
  hooks: { apply: (res: DoneResult) => void; refresh: () => void | Promise<void> },
) {
  const guard = useRef(flightGuard());
  const mounted = useRef(true);
  const hooksRef = useRef(hooks);
  hooksRef.current = hooks;
  const seq = useRef(0);
  const [busyId, setBusyId] = useState("");
  const [issue, setIssue] = useState<ActiveIssue | null>(null);
  const [toast, setToast] = useState<(ToastModel & ToastView) | null>(null);
  const toastRef = useRef(toast);
  toastRef.current = toast;

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const run: Run = useCallback(
    async (action, ids, call) => {
      const key = ids.join(",");
      if (guard.current.has(key)) {
        return; // a second click while the first call runs
      }
      setIssue(null);
      setBusyId(key);
      try {
        // The guard holds through the re-read too, so a click while the row is refreshed starts nothing.
        await guard.current.run(key, async () => {
          const res = await call();
          if (!mounted.current) {
            return;
          }
          const out = doneOutcome(action, res);
          hooksRef.current.apply(res);
          setIssue(out.issue === null ? null : { ...out.issue, ids, retry: () => void run(action, ids, call) });
          if (out.toast !== null) {
            setToast({ ...out.toast, id: ++seq.current, actionLabel: doneTexts.undo });
          }
          if (out.refresh) {
            await hooksRef.current.refresh();
          }
        });
      } finally {
        if (mounted.current) {
          setBusyId((cur) => (cur === key ? "" : cur));
        }
      }
    },
    [api],
  );

  const mark = useCallback(
    (id: string, done: boolean) => run(done ? "mark" : "unmark", [id], () => api.markDone([id], done)),
    [api, run],
  );

  // The toast closes while the undo runs; the call gets what the previous one returned, never a rebuilt list.
  const undo = useCallback(() => {
    const t = toastRef.current;
    if (t === null) {
      return Promise.resolve();
    }
    setToast(null);
    return run("undo", t.undo.prior.map((p) => p.id), () => api.restoreDone(t.undo.priorDone, t.undo.prior));
  }, [api, run]);

  const closeToast = useCallback(() => setToast(null), []);

  return { busyId, issue, toast, mark, undo, closeToast };
}
