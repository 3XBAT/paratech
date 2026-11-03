import { useEffect, useMemo, useRef, useState } from "react";
import {
  type KnowledgeBaseApi,
  type KnowledgeLesson,
  type KnowledgeSubject,
} from "../bindings/knowledgeBase.js";
import type { LibrarySubject } from "../bindings/library.js";
import { recordingImportApi } from "../bindings/recordingImport.js";
import { callByName } from "../bindings/runtime.js";
import { todayApi } from "../bindings/today.js";
import {
  errorText,
  exportResultLine,
  isNoteOnly,
  type LessonFocus,
  noFocus,
  orderLectures,
  type PastChoice,
  pastLesson,
} from "../knowledgeView.js";
import { importTexts } from "../recordingImportView.js";
import { exportItem, knowledgeEntryFor, pairsSubject, pairsTexts, tabTexts } from "../subjectTabsView.js";
import { Button } from "../ui/Button.js";
import { ResultLine, type ResultKind } from "../ui/controls.js";
import { RowMenu, type RowMenuItem } from "../ui/RowMenu.js";
import { ErrorLine } from "./FileRows.js";
import { AddPastNote, LessonList } from "./KnowledgeParts.js";
import { RecordingImportPanel } from "./RecordingImportPanel.js";

// The tab «Пары» of the subject page (TASK-353): the lessons of the subject from the knowledge base. Titles and notes are
// recorded, imported or typed text: text nodes only.

/** What the tab shows: the lessons of the subject by date, or why it cannot. */
export interface PairsData {
  status: "loading" | "ready" | "failed";
  /** The group of the subject in the knowledge base; null when it has no lessons and no notes yet. */
  entry: KnowledgeSubject | null;
  lessons: KnowledgeLesson[];
  error: string;
}

export const loadingPairs: PairsData = { status: "loading", entry: null, lessons: [], error: "" };

/**
 * Reads the lessons of a library subject: the knowledge-base group with that `subjectId` gives the key, the key gives the
 * lectures (recorded lessons and the student's notes), newest first. No group means no lessons and no second call. A
 * failed call is the Russian text of the refusal.
 */
export async function readPairs(api: Pick<KnowledgeBaseApi, "subjects" | "lectures">, subjectId: string): Promise<PairsData> {
  try {
    const entry = knowledgeEntryFor(await api.subjects(), subjectId);
    const lessons = entry === null ? [] : orderLectures(await api.lectures(entry.key));
    return { status: "ready", entry, lessons, error: "" };
  } catch (err) {
    return { status: "failed", entry: null, lessons: [], error: errorText(err) };
  }
}

/**
 * Keeps the lessons of a subject read (see readPairs). `rereads` changes when the list may be out of date (a lesson screen
 * was left or corrected, a recording was imported); the old list stays on screen meanwhile. `retry` after a failure
 * shows «Загрузка…» again.
 */
export function useSubjectPairs(api: Pick<KnowledgeBaseApi, "subjects" | "lectures">, subjectId: string, rereads: number) {
  const [state, setState] = useState<{ id: string; data: PairsData } | null>(null);
  const [tries, setTries] = useState(0);

  useEffect(() => {
    let stopped = false;
    void readPairs(api, subjectId).then((data) => {
      if (!stopped) {
        setState({ id: subjectId, data });
      }
    });
    return () => {
      stopped = true;
    };
  }, [api, subjectId, rereads, tries]);

  const data = state !== null && state.id === subjectId ? state.data : loadingPairs;
  const retry = () => {
    setState(null);
    setTries((n) => n + 1);
  };
  return { data, retry };
}

/** What a lesson opens with besides the lesson itself (the window keeps it for the lesson screen). */
export interface LessonOpening {
  focus: LessonFocus;
  materialId: string;
  editing: boolean;
}

export function SubjectPairsTab(props: {
  api: Pick<KnowledgeBaseApi, "exportSubject">;
  subject: Pick<LibrarySubject, "id" | "name">;
  data: PairsData;
  onRetry: () => void;
  /** A recording was imported: the lessons are read again. */
  onImported: () => void;
  /** A row (or a past pair chosen in «Добавить конспект прошедшей пары») opens the lesson screen. */
  onOpenLesson: (lesson: KnowledgeLesson, opening: LessonOpening) => void;
}) {
  const { data, subject } = props;
  const [exporting, setExporting] = useState(false);
  const [message, setMessage] = useState<{ kind: ResultKind; text: string } | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const acting = useRef(false);
  const mounted = useRef(true);
  const importing = useMemo(() => recordingImportApi(callByName), []);
  const today = useMemo(() => todayApi(callByName), []);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  if (data.status === "loading") {
    return (
      <p className="muted small" role="status">
        {tabTexts.loading}
      </p>
    );
  }
  if (data.status === "failed") {
    return <ErrorLine text={data.error} onRefresh={props.onRetry} onRetry={props.onRetry} />;
  }

  const kbSubject = pairsSubject(data.entry, subject);
  const open = (lesson: KnowledgeLesson, editing = false) => props.onOpenLesson(lesson, { focus: noFocus, materialId: "", editing });
  const empty = data.lessons.length === 0;
  const nothingToExport = data.lessons.every(isNoteOnly);
  const exportState = exportItem({ loaded: true, hasEntry: data.entry !== null, nothingToExport, exporting });
  const items: readonly RowMenuItem<"export">[] = [
    { key: "export", label: exporting ? pairsTexts.exporting : pairsTexts.export, disabled: exportState.disabled, hint: exportState.hint },
  ];

  async function onExport() {
    if (acting.current || data.entry === null) {
      return;
    }
    acting.current = true;
    setExporting(true);
    setMessage(null);
    try {
      const r = await props.api.exportSubject(data.entry.key);
      if (mounted.current) {
        setMessage(exportResultLine(r));
      }
    } catch (err) {
      if (mounted.current) {
        setMessage({ kind: "fail", text: errorText(err) });
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setExporting(false);
      }
    }
  }

  // The order of the siblings is fixed (the text only appears in the first slot), so an open form or panel survives the
  // first lesson that an import brings.
  return (
    <>
      {empty && <p className="muted">{pairsTexts.empty}</p>}
      <div className="tab-acts">
        <Button variant={importOpen ? undefined : "primary"} icon="up" aria-expanded={importOpen} onClick={() => setImportOpen((o) => !o)}>
          {importTexts.open}
        </Button>
        <AddPastNote subject={kbSubject} lectures={data.lessons} onChoose={(c: PastChoice) => open(pastLesson(kbSubject, c))} />
        <RowMenu label={pairsTexts.more} title="Ещё" items={items} onPick={() => void onExport()} />
      </div>
      {importOpen && (
        <RecordingImportPanel
          api={importing}
          loadLessons={async () => (await today.today()).lessons}
          subject={subject.name}
          onImported={props.onImported}
          onClose={() => setImportOpen(false)}
        />
      )}
      {message && (
        <ResultLine kind={message.kind} role={message.kind === "fail" ? "alert" : "status"}>
          {message.text}
        </ResultLine>
      )}
      {!empty && <LessonList lessons={data.lessons} onOpen={(l) => open(l)} />}
    </>
  );
}
