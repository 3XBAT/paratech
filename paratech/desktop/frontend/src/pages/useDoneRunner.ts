import { useEffect, useMemo, useRef, useState } from "react";
import type { ActiveIssue } from "../assignmentDoneView.js";
import type { DoneResult, HomeworkApi } from "../bindings/homework.js";
import { DoneRunner, type DoneToast } from "../doneRunner.js";

/**
 * «Выполнено» of a page (list, assignment, «Сегодня»): the state around DoneRunner (src/doneRunner.ts), where the
 * sequence lives and is tested. The page gives apply (its rows after the result, at once) and refresh (read the screen
 * again).
 */
export function useDoneRunner(
  api: Pick<HomeworkApi, "markDone" | "restoreDone">,
  hooks: { apply: (res: DoneResult) => void; refresh: () => void | Promise<void> },
) {
  const mounted = useRef(true);
  const hooksRef = useRef(hooks);
  hooksRef.current = hooks;
  const [busyId, setBusyId] = useState("");
  const [issue, setIssue] = useState<ActiveIssue | null>(null);
  const [toast, setToast] = useState<DoneToast | null>(null);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const runner = useMemo(
    () =>
      new DoneRunner(api, {
        busy: setBusyId,
        issue: setIssue,
        toast: setToast,
        apply: (res) => hooksRef.current.apply(res),
        refresh: () => hooksRef.current.refresh(),
        alive: () => mounted.current,
      }),
    [api],
  );

  return {
    busyId,
    issue,
    toast,
    mark: (id: string, done: boolean, title = "") => runner.mark(id, done, title),
    undo: () => runner.undo(),
    closeToast: () => runner.closeToast(),
    dismissIssue: () => runner.dismissIssue(),
  };
}
