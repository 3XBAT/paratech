import { type Dispatch, type SetStateAction, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { type LibraryApi, type LibraryMaterial, type LibrarySubject, libraryErrorText } from "../bindings/library.js";
import type { LibraryInboxApi } from "../bindings/libraryInbox.js";
import { subjectsKey, type UndoItem } from "../libraryInboxView.js";
import { type HintSnapshot, HintScheduler, MaterialFlow } from "../materialFlow.js";
import type { FileActions } from "./FileRows.js";

const emptyHints: HintSnapshot = { hints: new Map(), pending: new Set() };

/**
 * The calls behind the file rows (TASK-339): open, place, back to «Входящие», delete; the hints of «Входящие»; the undo
 * lines with their ten seconds. The logic is in materialFlow.ts (testable without React); this only wires it to the page.
 * The page passes its lists and `refresh`, which re-reads them after every change.
 */
export function useMaterialActions(p: {
  api: LibraryApi;
  inboxApi: LibraryInboxApi;
  subjects: LibrarySubject[] | null;
  setSubjects: Dispatch<SetStateAction<LibrarySubject[] | null>>;
  /** The files of «Входящие». */
  inbox: readonly LibraryMaterial[];
  /** Hints are asked only on the list page, where «Входящие» is shown. */
  hintsOn: boolean;
  refresh(): void;
}) {
  const { api, inboxApi, setSubjects, refresh } = p;
  const subjects = p.subjects ?? [];
  const latest = useRef<readonly LibrarySubject[]>(subjects);
  latest.current = subjects;

  const flow = useMemo(
    () =>
      new MaterialFlow({
        api,
        inboxApi,
        subjects: () => latest.current,
        addSubject: (s) => setSubjects((l) => [...(l ?? []), s]),
        refresh,
      }),
    [api, inboxApi, setSubjects, refresh],
  );
  useEffect(() => {
    flow.attach();
    return () => flow.dispose();
  }, [flow]);
  const { undo, undoBusy } = useSyncExternalStore(flow.subscribe, flow.getState, flow.getState);

  const [hintState, setHintState] = useState<HintSnapshot>(emptyHints);
  const scheduler = useMemo(() => new HintScheduler(inboxApi, setHintState), [inboxApi]);
  const key = subjectsKey(p.subjects);
  const on = p.hintsOn && p.subjects !== null;
  useEffect(() => {
    scheduler.attach();
    return () => scheduler.dispose();
  }, [scheduler]);
  useEffect(() => scheduler.update({ key, inbox: p.inbox, on }), [scheduler, key, p.inbox, on]);

  const actions: FileActions = {
    subjects,
    hints: hintState.hints,
    hintPending: hintState.pending,
    open: async (m) => {
      const r = await api.openMaterial(m.id);
      return r.status === "opened" ? "" : r.message;
    },
    place: (m, subjectId, folderId) => flow.placeInto(m, subjectId, folderId),
    placeHint: (m, h) => flow.placeHint(m, h),
    toInbox: (m) => flow.toInbox(m),
    remove: (m) => flow.remove(m),
    createSubject: async (name) => {
      try {
        const s = await api.createSubject(name, "");
        setSubjects((l) => [...(l ?? []), s]);
        return "";
      } catch (err) {
        return libraryErrorText(err);
      }
    },
    refresh,
  };

  return {
    actions,
    undo,
    undoBusy,
    undoOne: (it: UndoItem) => flow.undoOne(it),
    refreshUndo: () => flow.refreshUndo(),
    placeInto: (m: LibraryMaterial, subjectId: string, folderId: string) => flow.placeInto(m, subjectId, folderId),
  };
}
