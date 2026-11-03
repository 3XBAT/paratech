import { useEffect, useRef, useState } from "react";
import type { LibraryApi, LibraryMaterial, LibrarySubject } from "../bindings/library.js";
import type { NotesApi } from "../bindings/notes.js";
import {
  type Asking,
  type DialogView,
  type DiscardWay,
  applyFresh,
  askingDialog,
  openAsking,
  readFresh,
  type RemovedSubject,
  restoreEffects,
  runDiscard,
  runRestore,
  settleDiscard,
  startDiscard,
  startRefresh,
  subjectRowAttr,
  withRemovedLine,
  withoutRemovedLine,
} from "../subjectRemovalView.js";
import type { RestoreCall } from "./SubjectRemoval.js";

export type { RemovedSubject };

/** The open question and the element the focus returns to. */
interface Open extends Asking {
  returnTo: HTMLElement | null;
}

/**
 * «Удалить предмет» on the page of a subject (TASK-289). When the question opens, the subject and its notes are read again
 * (the page's copy may be out of date: a folder made since bumped the version), and «Удалить» waits for that; the fresh
 * version is the one sent. The files of the subject are the page's own list. The core answers with the real counts after
 * the call. After a delete the subject leaves the page (`onGone`) and a line with «Вернуть» joins `removed`; after «Вернуть»
 * the page reads the lists again (`onReload`) and the focus goes to the row of the subject. The transitions are pure
 * functions of subjectRemovalView; this hook only holds the state and the guards.
 */
export function useSubjectRemoval(p: {
  api: Pick<LibraryApi, "discardSubject" | "restoreSubject" | "subjects">;
  notes: Pick<NotesApi, "forSubject">;
  /** The files of the subject on screen; "loading" while they are read, null when they could not be. */
  materials: readonly LibraryMaterial[] | null | "loading";
  subjects: readonly LibrarySubject[] | null;
  onGone(subjectId: string): void;
  onReload(): void;
  /** The restored subject goes into the list at once (the reload follows) and the page opens it. */
  onRestored(subject: LibrarySubject): void;
}) {
  const [asking, setAsking] = useState<Open | null>(null);
  const [removed, setRemoved] = useState<readonly RemovedSubject[]>([]);
  const [restoredId, setRestoredId] = useState("");
  const running = useRef(false); // a double click comes before the re-render that disables the button
  const current = useRef(asking);
  current.current = asking;
  const latest = useRef(p);
  latest.current = p;
  const read = useRef(0); // a late answer for a question that is gone (or asked again) is dropped

  // Reads the subject and its notes; the answer lands in the question if it is still the one asked.
  const reread = (id: string) => {
    const mine = ++read.current;
    void readFresh(latest.current.api, latest.current.notes, id).then((f) => {
      if (mine === read.current) {
        setAsking((a) => (a === null ? a : applyFresh(a, f)));
      }
    });
  };

  const open = (subject: LibrarySubject, returnTo: HTMLElement | null) => {
    if (running.current) {
      return;
    }
    setAsking({ ...openAsking(subject), returnTo });
    reread(subject.id);
  };

  const close = () => {
    read.current++;
    setAsking(null);
  };

  async function discard() {
    const a = current.current;
    if (a === null || running.current) {
      return;
    }
    const next = startDiscard(a);
    if (next === null) {
      return;
    }
    running.current = true;
    setAsking(next);
    try {
      const out = await runDiscard(latest.current.api, next.id, next.version);
      const settled = settleDiscard(next, out);
      if (settled.line !== null) {
        const line = settled.line;
        close();
        setRemoved((list) => withRemovedLine(list, line));
        latest.current.onGone(next.id);
      } else {
        setAsking(settled.asking);
      }
    } finally {
      running.current = false;
    }
  }

  // «Обновить» (the subject changed while the question was open): everything is read again in place and the question is
  // asked again with the fresh counts; the page reads its lists too (the files are counted from them).
  const refresh = () => {
    const a = current.current;
    const next = a === null ? null : startRefresh(a);
    if (next === null) {
      return;
    }
    setAsking(next);
    latest.current.onReload();
    reread(next.id);
  };

  const act = (action: DiscardWay) => {
    const a = current.current;
    if (a === null || a.busy !== "") {
      return;
    }
    if (action === "delete") {
      void discard();
    } else if (action === "refresh") {
      refresh();
    } else {
      // «Закрыть» (it was gone already): the lists are read again and the subject leaves the page.
      close();
      latest.current.onReload();
      latest.current.onGone(a.id);
    }
  };

  const dismiss = () => {
    const a = current.current;
    if (a === null || a.busy === "deleting") {
      return;
    }
    close();
    if (a.problem !== "") {
      latest.current.onReload();
    }
  };

  const restore = (r: RemovedSubject): Promise<RestoreCall> => runRestore(latest.current.api, r.id, r.version);

  const settled = (r: RemovedSubject, call: RestoreCall) => {
    const effects = restoreEffects(call);
    if (effects.restored !== null) {
      setRemoved((list) => withoutRemovedLine(list, r.id));
      setRestoredId(effects.restored.id);
      latest.current.onRestored(effects.restored);
    }
    if (effects.reload) {
      latest.current.onReload();
    }
  };

  // After «Вернуть» the focus goes to the row of the restored subject (the line that had it is gone); a row that is not
  // drawn (its semester is folded) leaves the focus on the list.
  const listed = p.subjects?.some((s) => s.id === restoredId) === true;
  useEffect(() => {
    if (restoredId === "" || !listed) {
      return;
    }
    const rows = Array.from(document.querySelectorAll<HTMLElement>(`[${subjectRowAttr}]`));
    (rows.find((r) => r.getAttribute(subjectRowAttr) === restoredId) ?? document.querySelector<HTMLElement>(".tree button"))?.focus();
    setRestoredId("");
  }, [restoredId, listed]);

  const dialog: DialogView | null = asking === null ? null : askingDialog(asking, p.materials);

  return {
    open,
    dialog,
    returnTo: asking?.returnTo ?? null,
    act,
    dismiss,
    removed,
    restore,
    settled,
    closeLine: (id: string) => setRemoved((list) => withoutRemovedLine(list, id)),
  };
}
