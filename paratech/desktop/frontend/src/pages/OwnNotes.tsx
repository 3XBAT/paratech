import { useEffect, useMemo, useRef, useState } from "react";
import { libraryApi } from "../bindings/library.js";
import { type NoteView, notesApi, type NotesAdd, notesDropEvent, parseAdd } from "../bindings/notes.js";
import { callByName, onEventData } from "../bindings/runtime.js";
import {
  addLines,
  movedOrder,
  orderOf,
  ownNotesTexts as t,
  pickSubjectId,
  recordingTarget,
  refusalText,
  type ResultLine,
  type UndoAction,
  undoCall,
  undoneText,
  visibleNotes,
  withNote,
} from "../ownNotesView.js";
import { type OwnNotesActions, OwnNotesView } from "./OwnNotesParts.js";
import "./ownNotes.css";

// The «Мой конспект» block of one lecture (TASK-154): reads and changes the student's notes through NotesService and
// hands the view a plain model. Each change is one call at a time; the latest one can be undone.
export function OwnNotes(props: { recordingId: string; subject: string }) {
  const api = useMemo(() => notesApi(callByName), []);
  const library = useMemo(() => libraryApi(callByName), []);
  const mounted = useRef(true);
  const acting = useRef(false);
  const [notes, setNotes] = useState<NoteView[]>([]);
  const [loading, setLoading] = useState(true);
  const [subjectId, setSubjectId] = useState("");
  const [busy, setBusy] = useState(false);
  const [undo, setUndo] = useState<UndoAction | null>(null);
  const [status, setStatus] = useState<ResultLine | null>(null);
  const [lines, setLines] = useState<ResultLine[]>([]);
  const [pasteOpen, setPasteOpen] = useState(false);
  const [pasteText, setPasteText] = useState("");

  const target = recordingTarget(props.recordingId, subjectId);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    let stopped = false;
    const lecture = { recordingId: props.recordingId, lessonId: "", subjectId: "", date: "" };
    void (async () => {
      try {
        const list = await api.forLesson(lecture);
        // A library that cannot be read only leaves adding off; the notes still show.
        const subjects = await library.subjects().catch(() => []);
        if (!stopped) {
          setNotes(list);
          setSubjectId(pickSubjectId(list, subjects, props.subject));
        }
      } catch (err) {
        if (!stopped) {
          setStatus({ ok: false, text: refusalText(err) });
        }
      } finally {
        if (!stopped) {
          setLoading(false);
        }
      }
    })();
    return () => {
      stopped = true;
    };
  }, [api, library, props.recordingId, props.subject]);

  function applyAdd(add: NotesAdd) {
    if (add.note) {
      const note = add.note;
      setNotes((list) => withNote(list, note));
    }
    setUndo(null);
    setStatus(null);
    setLines(addLines(add));
  }

  // A drop on the block is imported by the core, which reports it as an event.
  useEffect(() => {
    let off: (() => void) | undefined;
    let closed = false;
    onEventData(notesDropEvent, (data) => {
      try {
        const add = parseAdd(data);
        if (add.target.recordingId === props.recordingId && mounted.current) {
          applyAdd(add);
        }
      } catch {
        if (mounted.current) {
          setStatus({ ok: false, text: refusalText(null) });
        }
      }
    }).then(
      (stop) => {
        if (closed) {
          stop();
        } else {
          off = stop;
        }
      },
      () => undefined,
    );
    return () => {
      closed = true;
      off?.();
    };
  }, [props.recordingId]);

  async function act(action: () => Promise<void>) {
    if (acting.current) {
      return;
    }
    acting.current = true;
    setBusy(true);
    try {
      await action();
    } catch (err) {
      if (mounted.current) {
        setStatus({ ok: false, text: refusalText(err) });
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  const done = (a: UndoAction | null, text: string) => {
    setUndo(a);
    setLines([]);
    setStatus({ ok: true, text });
  };

  const actions: OwnNotesActions = {
    onAddFiles: () => void act(async () => applyAdd(await api.addFiles(target))),
    onTogglePaste: () => setPasteOpen((o) => !o),
    onPasteText: setPasteText,
    onPasteSave: () =>
      void act(async () => {
        const add = await api.paste(target, pasteText);
        applyAdd(add);
        if (add.error === "" && add.files.every((f) => f.error === "")) {
          setPasteText("");
          setPasteOpen(false);
        }
      }),
    onPastePage: async (noteId, text) => {
      let stored = false;
      await act(async () => {
        const add = await api.paste(recordingTarget(props.recordingId, subjectId, noteId), text);
        applyAdd(add);
        stored = add.error === "" && add.files.every((f) => f.error === "");
      });
      return stored;
    },
    onReplace: (noteId, materialId) =>
      void act(async () => {
        const r = await api.replaceFile(noteId, materialId);
        if (r.cancelled || r.note === null) {
          setStatus({ ok: true, text: t.cancelled });
          return;
        }
        const note = r.note;
        setNotes((list) => withNote(list, note));
        const a: UndoAction = { kind: "replace", noteId, currentId: r.materialId, previousId: r.previousId };
        done(a, undoneText(a));
      }),
    onMove: (note, index, delta) =>
      void act(async () => {
        const before = orderOf(note);
        const order = movedOrder(before, index, delta);
        if (order === null) {
          return;
        }
        const next = await api.reorder(note.id, order);
        setNotes((list) => withNote(list, next));
        const a: UndoAction = { kind: "reorder", noteId: note.id, order: before };
        done(a, undoneText(a));
      }),
    onRemove: (noteId) =>
      void act(async () => {
        const next = await api.remove(noteId);
        setNotes((list) => withNote(list, next));
        const a: UndoAction = { kind: "remove", noteId };
        done(a, undoneText(a));
      }),
    onUndo: () =>
      void act(async () => {
        if (undo === null) {
          return;
        }
        const next = await undoCall(api, undo);
        if (next === null) {
          setUndo(null);
          setStatus({ ok: false, text: refusalText(null) });
          return;
        }
        setNotes((list) => withNote(list, next));
        done(null, t.undoneLine);
      }),
  };

  const undoNoteId = undo?.kind === "remove" ? undo.noteId : "";
  return (
    <OwnNotesView
      model={{
        notes: visibleNotes(notes, undoNoteId),
        loading,
        subjectId,
        target,
        busy,
        undo,
        status,
        lines,
        pasteOpen,
        pasteText,
      }}
      actions={actions}
    />
  );
}
