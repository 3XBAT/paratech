import { useEffect, useRef, useState } from "react";
import type { DiscardProblem, LibraryApi, LibraryMaterial, LibrarySubject } from "../bindings/library.js";
import type { NoteView, NotesApi } from "../bindings/notes.js";
import {
  type DialogView,
  type DiscardWay,
  dialogView,
  knownCounts,
  restoreNeedsReload,
  runDiscard,
  runRestore,
  subjectRowAttr,
} from "../subjectRemovalView.js";
import type { RestoreCall } from "./SubjectRemoval.js";

/** A subject the student removed on this visit: its «Вернуть» line stays until closed or the page is left. */
export interface RemovedSubject {
  id: string;
  name: string;
  /** The version of the removed subject, for restoreSubject (from the reply of the delete). */
  version: number;
}

interface Asking {
  id: string;
  name: string;
  version: number;
  busy: boolean;
  problem: Exclude<DiscardProblem, ""> | "";
  returnTo: HTMLElement | null;
}

/**
 * «Удалить предмет» on the page of a subject (TASK-289). The dialog is asked with the counts the page knows (the notes of
 * the subject are read when it opens, its files are the page's own list); the core answers with the real ones after the
 * call. After a delete the subject leaves the page (`onGone`) and a line with «Вернуть» joins `removed`; after «Вернуть»
 * the page reads the lists again (`onReload`) and the focus goes to the row of the subject.
 */
export function useSubjectRemoval(p: {
  api: Pick<LibraryApi, "discardSubject" | "restoreSubject">;
  notes: Pick<NotesApi, "forSubject">;
  /** The files of the subject on screen; null while unknown. */
  materials: readonly LibraryMaterial[] | null;
  subjects: readonly LibrarySubject[] | null;
  onGone(subjectId: string): void;
  onReload(): void;
  /** The restored subject goes into the list at once (the reload follows). */
  onRestored(subject: LibrarySubject): void;
}) {
  const [asking, setAsking] = useState<Asking | null>(null);
  const [notes, setNotes] = useState<readonly NoteView[] | null | "loading">("loading");
  const [removed, setRemoved] = useState<readonly RemovedSubject[]>([]);
  const [restoredId, setRestoredId] = useState("");
  const running = useRef(false); // a double click comes before the re-render that disables the button
  const current = useRef(asking);
  current.current = asking;
  const latest = useRef(p);
  latest.current = p;
  const read = useRef(0); // a late answer for the notes of a question that is gone is dropped

  const open = (subject: LibrarySubject, returnTo: HTMLElement | null) => {
    if (running.current) {
      return;
    }
    setAsking({ id: subject.id, name: subject.name, version: subject.version, busy: false, problem: "", returnTo });
    setNotes("loading");
    const mine = ++read.current;
    p.notes.forSubject(subject.id).then(
      (n) => mine === read.current && setNotes(n),
      () => mine === read.current && setNotes(null),
    );
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
    running.current = true;
    setAsking({ ...a, busy: true, problem: "" });
    try {
      const out = await runDiscard(latest.current.api, a.id, a.version);
      if (out.kind === "done") {
        close();
        setRemoved((list) => [...list.filter((r) => r.id !== a.id), { id: a.id, name: a.name, version: out.discard.version }]);
        latest.current.onGone(a.id);
      } else {
        setAsking({ ...a, busy: false, problem: out.problem });
      }
    } finally {
      running.current = false;
    }
  }

  const act = (action: DiscardWay) => {
    const a = current.current;
    if (a === null || a.busy) {
      return;
    }
    if (action === "delete") {
      void discard();
      return;
    }
    // «Обновить» (the subject changed) and «Закрыть» (it was gone already): the lists are read again; a subject that is gone
    // leaves the page.
    close();
    latest.current.onReload();
    if (action === "close") {
      latest.current.onGone(a.id);
    }
  };

  const dismiss = () => {
    const a = current.current;
    if (a === null || a.busy) {
      return;
    }
    close();
    if (a.problem !== "") {
      latest.current.onReload();
    }
  };

  const restore = (r: RemovedSubject): Promise<RestoreCall> => runRestore(latest.current.api, r.id, r.version);

  const settled = (r: RemovedSubject, call: RestoreCall) => {
    if (call.state.phase === "done" && call.subject !== null) {
      setRemoved((list) => list.filter((x) => x.id !== r.id));
      setRestoredId(call.subject.id);
      latest.current.onRestored(call.subject);
      latest.current.onReload();
    } else if (restoreNeedsReload(call.state)) {
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

  const dialog: DialogView | null =
    asking === null
      ? null
      : dialogView({
          name: asking.name,
          counts: notes === "loading" ? "loading" : knownCounts(notes, p.materials),
          busy: asking.busy,
          problem: asking.problem,
        });

  return {
    open,
    dialog,
    returnTo: asking?.returnTo ?? null,
    act,
    dismiss,
    removed,
    restore,
    settled,
    closeLine: (id: string) => setRemoved((list) => list.filter((r) => r.id !== id)),
  };
}
