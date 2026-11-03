import { useEffect, useRef } from "react";
import { refocusAllowed } from "../subjectTabsView.js";

/**
 * Gives the focus back to the button that started «Добавить файл» (TASK-353, the follow-up of TASK-347). The button is off
 * while the call runs and may be gone afterwards: the «+» of a folder in the line «Пусто: …» is replaced by the «+» of the
 * section header once the folder has its first file. Both carry the same id (folderAddId), so the focus is looked up by id
 * when the call is over and the files have been read again (`files` is replaced), or at once when nothing was added. The
 * focus moves only if it is lost (see refocusAllowed). `failed`: the files could not be read, nothing more will come.
 */
export function useFocusBack(busy: boolean, files: unknown, failed: boolean) {
  const pending = useRef<{ id: string; reload: boolean; seen: unknown } | null>(null);

  useEffect(() => {
    const p = pending.current;
    if (p === null || busy || (p.reload && files === p.seen && !failed)) {
      return;
    }
    pending.current = null;
    const at = document.activeElement;
    if (refocusAllowed(at === null ? null : { id: at.id, isBody: at === document.body }, p.id)) {
      document.getElementById(p.id)?.focus();
    }
  }, [busy, files, failed]);

  return {
    /** The add starts from the button with this id. */
    arm: (id: string) => {
      pending.current = { id, reload: false, seen: files };
    },
    /** The file landed: the files are read again, wait for them. */
    reloading: () => {
      if (pending.current !== null) {
        pending.current.reload = true;
      }
    },
    /** Another subject: forget it. */
    cancel: () => {
      pending.current = null;
    },
  };
}
