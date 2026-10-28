import { type Dispatch, type SetStateAction, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type LibraryApi, type LibraryMaterial, type LibrarySubject, libraryErrorText, genericError } from "../bindings/library.js";
import { type LibraryInboxApi, noHint, type PlacementHint } from "../bindings/libraryInbox.js";
import {
  hintTargets,
  movedToInboxNote,
  placeLabel,
  placedNote,
  removedNote,
  subjectsKey,
  type UndoBack,
  type UndoItem,
  undoDrop,
  undoFail,
  undoNextDelay,
  undoPrune,
  undoPush,
} from "../libraryInboxView.js";
import type { FileActions } from "./FileRows.js";

const emptyHints: ReadonlyMap<string, PlacementHint> = new Map();

/** Where a file lies now, as the way back for «Вернуть». */
function backOf(m: LibraryMaterial): UndoBack {
  return m.inbox || m.subjectId === "" ? { kind: "inbox" } : { kind: "place", subjectId: m.subjectId, folderId: m.folderId };
}

/**
 * The calls behind the file rows (TASK-339): open, place, back to «Входящие», delete; the hints of «Входящие» (one by
 * one, at most hintLimit files, a failed hint is silent); the undo lines with their ten seconds. The page passes its
 * lists and `refresh`, which re-reads them after every change.
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

  // ---- Undo lines ----
  const [undo, setUndo] = useState<UndoItem[]>([]);
  const [undoBusy, setUndoBusy] = useState(false);
  const seq = useRef(0);
  const undoing = useRef(false);

  useEffect(() => {
    const delay = undoNextDelay(undo, Date.now());
    if (delay === null) {
      return;
    }
    const timer = setTimeout(() => setUndo((l) => undoPrune(l, Date.now())), delay);
    return () => clearTimeout(timer);
  }, [undo]);

  const pushUndo = useCallback((materialId: string, text: string, back: UndoBack) => {
    seq.current += 1;
    const id = seq.current;
    setUndo((l) => undoPush(l, { id, materialId, text, back }, Date.now()));
  }, []);

  const undoOne = useCallback(
    async (it: UndoItem) => {
      if (undoing.current) {
        return;
      }
      undoing.current = true;
      setUndoBusy(true);
      try {
        if (it.back.kind === "inbox") {
          await inboxApi.moveToInbox(it.materialId);
        } else if (it.back.kind === "place") {
          await inboxApi.place(it.materialId, it.back.subjectId, it.back.folderId);
        } else {
          await inboxApi.restoreMaterial(it.materialId);
        }
        setUndo((l) => undoDrop(l, it.id));
        refresh();
      } catch (err) {
        setUndo((l) => undoFail(l, it.id, libraryErrorText(err)));
      } finally {
        undoing.current = false;
        setUndoBusy(false);
      }
    },
    [inboxApi, refresh],
  );

  /** «Обновить список» of an undo line that failed: the line has done its job. */
  const refreshUndo = useCallback(() => {
    setUndo((l) => l.filter((x) => x.error === ""));
    refresh();
  }, [refresh]);

  // ---- Hints of «Входящие» ----
  const key = subjectsKey(p.subjects);
  const [hintState, setHintState] = useState<{ key: string; map: ReadonlyMap<string, PlacementHint> }>({ key, map: emptyHints });
  const hints = hintState.key === key ? hintState.map : emptyHints;
  const todo = useMemo(
    () => (p.hintsOn && p.subjects !== null ? hintTargets(p.inbox, new Set(hints.keys())) : []),
    [p.hintsOn, p.subjects, p.inbox, hints],
  );
  const todoKey = todo.join("|");

  useEffect(() => {
    if (todo.length === 0) {
      return;
    }
    let live = true;
    void (async () => {
      const id = todo[0];
      let hint = noHint;
      try {
        hint = await inboxApi.placementHint(id);
      } catch {
        // Silent: the row simply has no hint.
      }
      if (live) {
        setHintState((s) => ({ key, map: new Map(s.key === key ? s.map : emptyHints).set(id, hint) }));
      }
    })();
    return () => {
      live = false;
    };
    // todo is covered by todoKey: one request per run, the next file follows when this hint is stored.
  },[todoKey, key, inboxApi]);

  const hintPending = useMemo(() => new Set(todo), [todo]);

  // ---- Changes ----
  async function change(m: LibraryMaterial, work: () => Promise<LibraryMaterial>, note: (r: LibraryMaterial) => string, back: UndoBack) {
    try {
      const r = await work();
      pushUndo(m.id, note(r), back);
      refresh();
      return "";
    } catch (err) {
      return libraryErrorText(err);
    }
  }

  const placeInto = (m: LibraryMaterial, subjectId: string, folderId: string, extra?: LibrarySubject) =>
    change(
      m,
      () => inboxApi.place(m.id, subjectId, folderId),
      (r) => placedNote(placeLabel(extra ? [...latest.current, extra] : latest.current, r.subjectId, r.folderId)),
      backOf(m),
    );

  const actions: FileActions = {
    subjects,
    hints,
    hintPending,
    open: async (m) => {
      const r = await api.openMaterial(m.id);
      return r.status === "opened" ? "" : r.message;
    },
    place: (m, subjectId, folderId) => placeInto(m, subjectId, folderId),
    placeHint: async (m, h) => {
      if (h.kind === "existing") {
        return placeInto(m, h.subjectId, h.folderId);
      }
      if (h.kind !== "create") {
        return genericError;
      }
      // A repeat after a refusal of the placing must not create the subject twice.
      let s = latest.current.find((x) => x.name === h.createName);
      if (!s) {
        try {
          s = await api.createSubject(h.createName, "");
        } catch (err) {
          return libraryErrorText(err);
        }
        const created = s;
        setSubjects((l) => [...(l ?? []), created]);
      }
      const folder = s.folders.find((f) => f.system === h.folderKind);
      return folder ? placeInto(m, s.id, folder.id, s) : genericError;
    },
    toInbox: (m) => change(m, () => inboxApi.moveToInbox(m.id), () => movedToInboxNote, backOf(m)),
    remove: (m) => change(m, () => inboxApi.removeMaterial(m.id), () => removedNote(m.name), { kind: "restore" }),
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

  return { actions, undo, undoBusy, undoOne, refreshUndo, change, placeInto };
}
