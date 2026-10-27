import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type KnowledgeBaseApi,
  knowledgeBaseApi,
  type KnowledgeHit,
  type KnowledgeLesson,
  type KnowledgeSearchResult,
  type KnowledgeSubject,
} from "../bindings/knowledgeBase";
import { recordingImportApi } from "../bindings/recordingImport";
import { callByName } from "../bindings/runtime";
import { todayApi } from "../bindings/today";
import { importTexts } from "../recordingImportView";
import {
  askSearch,
  checkQuery,
  errorText,
  exportResultLine,
  hitOpening,
  isNoteOnly,
  type LessonFocus,
  idleFlight,
  lecturesMeta,
  noFocus,
  orderLectures,
  type PastChoice,
  pastLesson,
  type SearchFlight,
  searchDone,
  searchPauseMs,
  searchTexts,
  subjectMeta,
} from "../knowledgeView";
import { Button } from "../ui/Button";
import { Banner, Card, EmptyState, Input, ResultLine, type ResultKind } from "../ui/controls";
import { AddPastNote, HitList, LessonList, SubjectList } from "./KnowledgeParts";
import { RecordingImportPanel } from "./RecordingImportPanel";
import { KnowledgeLessonPage, NoteLecturePage } from "./KnowledgeLessonPage";

type SearchOutcome =
  | { kind: "short" }
  | { kind: "long" }
  | { kind: "error"; text: string }
  | { kind: "ok"; result: KnowledgeSearchResult };

interface OpenLesson {
  lesson: KnowledgeLesson;
  focus: LessonFocus;
  editing: boolean;
  /** The file of the student's notes to mark (a hit in them); "" for none. */
  materialId: string;
}

// «База знаний» (TASK-091): the subjects with their lessons, the search over the whole base and the export of a
// subject. The core groups, searches and writes the file; the page only shows and asks. A lesson opens here, over the
// list it came from, so «Назад» finds the list or the search as the user left it.
export function KnowledgeBasePage(props: { visit?: number; onOpenAssignment: (assignmentId: string) => void }) {
  const api = useMemo(() => knowledgeBaseApi(callByName), []);
  const [subjects, setSubjects] = useState<KnowledgeSubject[] | null>(null);
  const [listError, setListError] = useState("");
  const [reload, setReload] = useState(0);
  const [subject, setSubject] = useState<KnowledgeSubject | null>(null);
  const [open, setOpen] = useState<OpenLesson | null>(null);
  const changed = useRef(false);

  const [query, setQuery] = useState("");
  const [outcome, setOutcome] = useState<SearchOutcome | null>(null);
  const [searching, setSearching] = useState(false);
  const flight = useRef<SearchFlight>(idleFlight);
  const lastQuery = useRef("");

  useEffect(() => {
    let stopped = false;
    api.subjects().then(
      (list) => {
        if (!stopped) {
          setSubjects(list);
          setListError("");
        }
      },
      (err) => {
        if (!stopped) {
          setListError(errorText(err));
        }
      },
    );
    return () => {
      stopped = true;
    };
  }, [api, reload]);

  // Searches on Enter (force) or after a pause in typing. One call at a time: a query asked meanwhile is sent when
  // the running call returns, and only the answer to the latest query is shown.
  const runSearch = useCallback(
    async (raw: string, force: boolean) => {
      const c = checkQuery(raw);
      if (c.kind !== "ok") {
        lastQuery.current = "";
        flight.current = askSearch(flight.current, "").flight;
        setSearching(false);
        setOutcome(c.kind === "empty" ? null : { kind: c.kind });
        return;
      }
      if (!force && c.query === lastQuery.current) {
        return;
      }
      lastQuery.current = c.query;
      const asked = askSearch(flight.current, c.query);
      flight.current = asked.flight;
      setSearching(true);
      let q = asked.send;
      while (q !== null) {
        let answer: SearchOutcome;
        try {
          answer = { kind: "ok", result: await api.search(q) };
        } catch (err) {
          answer = { kind: "error", text: errorText(err) };
        }
        const done = searchDone(flight.current);
        flight.current = done.flight;
        q = done.send;
        if (q === null) {
          if (done.show) {
            if (answer.kind === "error") {
              lastQuery.current = "";
            }
            setOutcome(answer);
          }
          setSearching(false);
        }
      }
    },
    [api],
  );

  useEffect(() => {
    const t = setTimeout(() => void runSearch(query, false), searchPauseMs);
    return () => clearTimeout(t);
  }, [query, runSearch]);

  // A click on «База знаний» in the rail while here goes back to the subjects.
  const firstVisit = useRef(props.visit);
  useEffect(() => {
    if (props.visit !== firstVisit.current) {
      firstVisit.current = props.visit;
      setOpen(null);
      setSubject(null);
      setQuery("");
      void runSearch("", false);
      setReload((n) => n + 1);
    }
  }, [props.visit, runSearch]);

  const openLesson = (lesson: KnowledgeLesson, focus: LessonFocus = noFocus, editing = false, materialId = "") => {
    changed.current = false;
    setOpen({ lesson, focus, editing, materialId });
  };

  if (open !== null && isNoteOnly(open.lesson)) {
    // No recording: the page reads nothing by a recording id.
    return <NoteLecturePage key={open.lesson.key} lesson={open.lesson} materialId={open.materialId} onBack={() => setOpen(null)} />;
  }

  if (open !== null) {
    return (
      <KnowledgeLessonPage
        key={open.lesson.recordingId}
        api={api}
        lesson={open.lesson}
        focus={open.focus}
        materialId={open.materialId}
        editing={open.editing}
        onBack={() => {
          setOpen(null);
          if (changed.current) {
            // The found lessons show the old subject: ask again.
            void runSearch(query, true);
          }
        }}
        onChanged={() => {
          changed.current = true;
          setReload((n) => n + 1);
        }}
        onOpenAssignment={props.onOpenAssignment}
      />
    );
  }

  if (subject !== null) {
    return (
      <SubjectView
        api={api}
        subject={subject}
        onBack={() => {
          setSubject(null);
          setReload((n) => n + 1);
        }}
        onOpen={(l, editing) => openLesson(l, noFocus, editing)}
      />
    );
  }

  return (
    <section className="page kb">
      <div className="head">
        <div>
          <h1>База знаний</h1>
          <p className="small muted kb-sub">Занятия по предметам: кратко, текст лекции, расшифровка и задания.</p>
        </div>
      </div>
      <Card className="box kb-search">
        <div className="tr-search">
          <Input
            type="search"
            aria-label="Поиск по базе знаний"
            placeholder="Поиск по названиям, конспектам и расшифровкам"
            maxLength={200}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              // A cleared field shows the subjects at once, without the pause.
              if (e.target.value.trim() === "") {
                void runSearch("", false);
              }
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void runSearch(query, true);
              }
            }}
          />
          <Button busy={searching} busyLabel="Поиск…" onClick={() => void runSearch(query, true)}>
            Найти
          </Button>
        </div>
        <p className="small muted tr-note">{searchTexts.local}</p>
      </Card>
      {outcome !== null ? (
        <SearchResults
          outcome={outcome}
          searching={searching}
          onOpen={(l, hit: KnowledgeHit | null) => {
            if (hit === null) {
              openLesson(l);
              return;
            }
            const o = hitOpening(hit);
            openLesson(o.lesson, o.focus, false, o.materialId);
          }}
        />
      ) : (
        <>
          {listError !== "" && (
            <Banner kind="fail" role="alert">
              {listError}
            </Banner>
          )}
          {subjects === null ? (
            listError === "" && <p className="muted">Загрузка…</p>
          ) : subjects.length === 0 ? (
            <EmptyState label="База знаний" title="Пока пусто">
              Здесь появятся занятия после первой записи: по предметам, с кратким содержанием, текстом лекции и
              расшифровкой.
            </EmptyState>
          ) : (
            <>
              <h2 className="group-title">Предметы</h2>
              <SubjectList subjects={subjects} onOpen={setSubject} />
            </>
          )}
        </>
      )}
    </section>
  );
}

function SearchResults(props: {
  outcome: SearchOutcome;
  searching: boolean;
  onOpen: (lesson: KnowledgeLesson, hit: KnowledgeHit | null) => void;
}) {
  const o = props.outcome;
  switch (o.kind) {
    case "short":
      return <p className="small muted">{searchTexts.short}</p>;
    case "long":
      return (
        <ResultLine kind="fail" role="alert">
          {searchTexts.long}
        </ResultLine>
      );
    case "error":
      return (
        <ResultLine kind="fail" role="alert">
          {o.text}
        </ResultLine>
      );
    case "ok":
      return (
        <div className={props.searching ? "kb-results kb-stale" : "kb-results"}>
          {o.result.hits.length === 0 ? (
            <p className="muted" role="status">
              {searchTexts.none}
            </p>
          ) : (
            <HitList result={o.result} onOpen={props.onOpen} />
          )}
        </div>
      );
  }
}

// The lessons of one subject by date, with the export of the whole subject into one .md.
function SubjectView(props: {
  api: KnowledgeBaseApi;
  subject: KnowledgeSubject;
  onBack: () => void;
  onOpen: (l: KnowledgeLesson, editing: boolean) => void;
}) {
  const { api, subject } = props;
  const [lessons, setLessons] = useState<KnowledgeLesson[] | null>(null);
  const [error, setError] = useState("");
  const [exporting, setExporting] = useState(false);
  const [message, setMessage] = useState<{ kind: ResultKind; text: string } | null>(null);
  const mounted = useRef(true);
  const acting = useRef(false);
  // «Импортировать запись» (TASK-162): the panel; the lessons are read again after an import.
  const importing = useMemo(() => recordingImportApi(callByName), []);
  const today = useMemo(() => todayApi(callByName), []);
  const [importOpen, setImportOpen] = useState(false);
  const [imports, setImports] = useState(0);

  useEffect(() => {
    mounted.current = true;
    let stopped = false;
    // Recorded and note-only lectures (the student's notes): a subject known only from notes loads the same way.
    api.lectures(subject.key).then(
      (list) => {
        if (!stopped) {
          setLessons(orderLectures(list));
        }
      },
      (err) => {
        if (!stopped) {
          setError(errorText(err));
        }
      },
    );
    return () => {
      stopped = true;
      mounted.current = false;
    };
  }, [api, subject.key, imports]);

  async function onExport() {
    if (acting.current) {
      return;
    }
    acting.current = true;
    setExporting(true);
    setMessage(null);
    try {
      const r = await api.exportSubject(subject.key);
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

  const count = lessons === null ? subject : lecturesMeta(lessons);
  return (
    <section className="page kb">
      <div className="hw-back">
        <Button variant="ghost" onClick={props.onBack}>
          Назад
        </Button>
      </div>
      <div className="head">
        <div>
          <h1 className="kb-heading">{subject.name}</h1>
          <p className="small muted num kb-sub">{subjectMeta(count)}</p>
        </div>
        <div className="head-acts">
          <Button icon="up" aria-expanded={importOpen} onClick={() => setImportOpen((open) => !open)}>
            {importTexts.open}
          </Button>
          <Button
            icon="down"
            busy={exporting}
            busyLabel="Сохранение…"
            disabled={exporting || lessons === null || lessons.every(isNoteOnly)}
            onClick={() => void onExport()}
          >
            Экспорт в .md
          </Button>
        </div>
      </div>
      {importOpen && (
        <RecordingImportPanel
          api={importing}
          loadLessons={async () => (await today.today()).lessons}
          subject={subject.noSubject ? "" : subject.name}
          onImported={() => setImports((n) => n + 1)}
          onClose={() => setImportOpen(false)}
        />
      )}
      {message && (
        <ResultLine kind={message.kind} role={message.kind === "fail" ? "alert" : "status"}>
          {message.text}
        </ResultLine>
      )}
      <p className="small muted kb-sub">
        Экспорт — один файл .md: занятия по датам, с кратким содержанием и текстом лекции. Сохраняется только по кнопке.
      </p>
      {error !== "" && (
        <Banner kind="fail" role="alert">
          {error}
        </Banner>
      )}
      {lessons !== null && (
        <AddPastNote
          subject={subject}
          lectures={lessons}
          onChoose={(c: PastChoice) => props.onOpen(pastLesson(subject, c), false)}
        />
      )}
      {lessons === null ? (
        error === "" && <p className="muted">Загрузка…</p>
      ) : lessons.length === 0 ? (
        <p className="muted">В этом предмете больше нет занятий.</p>
      ) : (
        <LessonList
          lessons={lessons}
          onOpen={(l) => props.onOpen(l, false)}
          onEdit={subject.noSubject ? (l) => props.onOpen(l, true) : undefined}
        />
      )}
    </section>
  );
}
