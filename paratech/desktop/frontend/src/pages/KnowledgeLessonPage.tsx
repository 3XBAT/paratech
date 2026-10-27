import { useEffect, useMemo, useRef, useState } from "react";
import { actionErrorText } from "../activityView";
import { activityApi } from "../bindings/activity";
import type { KnowledgeBaseApi, KnowledgeLesson } from "../bindings/knowledgeBase";
import { callByName } from "../bindings/runtime";
import { lectureTextApi, summaryApi, type SummarySaveFormat, type SummaryView } from "../bindings/summary";
import {
  errorText as knowledgeErrorText,
  type LessonFocus,
  lessonHeading,
  lessonMeta,
  type LessonTab,
  lessonTabs,
  noteLectureHeading,
  noteLectureMeta,
  shownBlock,
  summaryFocus,
} from "../knowledgeView";
import { lectureCopiedText } from "../lectureTextView";
import { useOpenSummary } from "../openSummary";
import { lectureOf } from "../ownNotesView";
import { errorText, nextPollMs } from "../summaryView";
import { saveResultText } from "../transcriptView";
import { Button } from "../ui/Button";
import { Banner, type ResultKind, Segmented } from "../ui/controls";
import { LessonInfoForm, ReadyMarks } from "./KnowledgeParts";
import { LectureTextCard } from "./LectureTextParts";
import { OwnNotes } from "./OwnNotes";
import { AiNoteLabel } from "./OwnNotesParts";
import { HomeworkCard, SummaryCard } from "./SummaryParts";
import { TranscriptPage } from "./TranscriptPage";

// One lesson of «База знаний» (TASK-091): «Кратко», «Текст лекции», «Расшифровка» and «Задания» as tabs over the
// summary and transcript viewers (TASK-087/090/079), opened at the place a search found. Works offline: everything
// comes from the core on this computer. Model text is rendered by those viewers as React text only.
export function KnowledgeLessonPage(props: {
  api: KnowledgeBaseApi;
  lesson: KnowledgeLesson;
  focus: LessonFocus;
  /** The file of the student's notes a search hit opened the page at; "" for none. */
  materialId?: string;
  /** Opens with the subject and kind form (from «Без предмета»). */
  editing: boolean;
  onBack: () => void;
  /** The subject or kind was saved: the lists are out of date. */
  onChanged: (l: KnowledgeLesson) => void;
  onOpenAssignment: (assignmentId: string) => void;
}) {
  const id = props.lesson.recordingId;
  const summary = useMemo(() => summaryApi(callByName), []);
  const lecture = useMemo(() => lectureTextApi(callByName), []);
  const activity = useMemo(() => activityApi(callByName), []);
  const openSummary = useOpenSummary();
  const [lesson, setLesson] = useState(props.lesson);
  const [lessonError, setLessonError] = useState("");
  const [editing, setEditing] = useState(props.editing);
  const [tab, setTab] = useState<LessonTab>(props.focus.tab);
  const [view, setView] = useState<SummaryView | null>(null);
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  const content = useRef<HTMLDivElement>(null);
  const scrolled = useRef(false);

  // The list may be older than the lesson: read it again (a deleted recording says so).
  useEffect(() => {
    let stopped = false;
    props.api.lesson(id).then(
      (l) => {
        if (!stopped) {
          setLesson(l);
        }
      },
      (err) => {
        if (!stopped) {
          setLessonError(knowledgeErrorText(err));
        }
      },
    );
    return () => {
      stopped = true;
    };
  }, [props.api, id]);

  // The summary view as on the summary page: one call at a time, polled while a step runs.
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let last: SummaryView | null = null;
    let errors = 0;
    const tick = async () => {
      try {
        const v = await summary.view(id);
        if (stopped) {
          return;
        }
        last = v;
        errors = 0;
        setView(v);
        setError("");
      } catch (err) {
        if (stopped) {
          return;
        }
        errors++;
        setError(errorText(err));
      }
      const wait = stopped ? null : nextPollMs(last, errors);
      if (wait !== null) {
        timer = setTimeout(() => void tick(), wait);
      }
    };
    void tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [summary, id, refresh]);

  const focus = props.focus;
  const focusBlock = view && focus.tab === "lecture" ? shownBlock(view, focus.index) : -1;
  const inSummary = view && focus.tab === "summary" ? summaryFocus(view.summary, focus.index) : { section: -1, point: -1 };
  const focusSection = inSummary.section;
  const focusPoint = inSummary.point;

  // The found place is scrolled to once, when its tab first shows it (the transcript tab does it itself).
  useEffect(() => {
    if (scrolled.current || tab !== focus.tab || (focusBlock < 0 && focusSection < 0 && focusPoint < 0)) {
      return;
    }
    const el = content.current?.querySelector(".kb-focus");
    if (el) {
      scrolled.current = true;
      el.scrollIntoView({ block: "center" });
    }
  }, [tab, focus.tab, focusBlock, focusSection, focusPoint]);

  return (
    <section className="page tr sm kb-lesson-page">
      <div className="hw-back">
        <Button variant="ghost" onClick={props.onBack}>
          Назад
        </Button>
      </div>
      <div className="head">
        <div>
          <h1 className="kb-heading">{lessonHeading(lesson)}</h1>
          <p className="tr-sub">
            <span className="tr-title">{lessonMeta(lesson)}</span>
          </p>
          <ReadyMarks lesson={lesson} />
        </div>
        <div className="acts">
          <Button variant="ghost" onClick={() => setEditing((e) => !e)} aria-expanded={editing}>
            Предмет и тип
          </Button>
          <Button variant="ghost" icon="doc" onClick={() => openSummary(id)}>
            Открыть конспект
          </Button>
        </div>
      </div>
      {lessonError !== "" && (
        <Banner kind="fail" role="alert">
          {lessonError}
        </Banner>
      )}
      {editing && (
        <LessonInfoForm
          lesson={lesson}
          save={props.api.setLessonInfo}
          onSaved={(l) => {
            setLesson(l);
            setEditing(false);
            props.onChanged(l);
          }}
          onClose={() => setEditing(false)}
        />
      )}
      <div className="kb-tabs">
        <Segmented label="Раздел занятия" options={lessonTabs} value={tab} onChange={setTab} />
      </div>
      <div ref={content}>
        {tab === "transcript" ? (
          <TranscriptPage
            recordingId={id}
            embedded
            focusSegment={focus.tab === "transcript" ? focus.index : -1}
            onBack={props.onBack}
            onOpenSummary={() => setTab("summary")}
          />
        ) : (
          <>
            {error && (
              <Banner kind="fail" role="alert">
                {error}
              </Banner>
            )}
            {tab === "summary" ? (
              <>
                {view === null ? (
                  !error && <p className="muted">Загрузка…</p>
                ) : (
                  <div className="own-paratech">
                    <AiNoteLabel />
                    <SummaryCard view={view} focusSection={focusSection} focusPoint={focusPoint} />
                  </div>
                )}
                <OwnNotes
                  lecture={{ kind: "recording", recordingId: id }}
                  librarySubjectId={lesson.librarySubjectId}
                  librarySubjectState={lesson.librarySubjectState}
                  focusMaterialId={props.materialId}
                />
              </>
            ) : view === null ? (
              !error && <p className="muted">Загрузка…</p>
            ) : tab === "homework" ? (
              <HomeworkCard view={view} onOpenAssignment={props.onOpenAssignment} />
            ) : (
              <LectureTab
                view={view}
                focusBlock={focusBlock}
                copy={() => lecture.copy(id)}
                saveAs={(f) => lecture.saveAs(id, f)}
                start={() => activity.retryPostProcessing(id, "lectureText")}
                onStarted={() => setRefresh((n) => n + 1)}
                onOpenTranscript={() => setTab("transcript")}
              />
            )}
          </>
        )}
      </div>
    </section>
  );
}

// A lecture known only from the student's notes (TASK-155): no recording, so no summary, transcript or tabs and no
// call that needs a recording id; only the «Мой конспект» block, opened by the lecture's ids.
export function NoteLecturePage(props: { lesson: KnowledgeLesson; materialId?: string; onBack: () => void }) {
  const l = props.lesson;
  return (
    <section className="page tr sm kb-lesson-page">
      <div className="hw-back">
        <Button variant="ghost" onClick={props.onBack}>
          Назад
        </Button>
      </div>
      <div className="head">
        <div>
          <h1 className="kb-heading">{noteLectureHeading(l)}</h1>
          <p className="tr-sub">
            <span className="tr-title">{noteLectureMeta(l)}</span>
          </p>
        </div>
      </div>
      <OwnNotes
        lecture={lectureOf(l)}
        librarySubjectId={l.librarySubjectId}
        librarySubjectState={l.librarySubjectState}
        focusMaterialId={props.materialId}
      />
    </section>
  );
}

// The lecture text card with its copy, save and start, as on the summary page.
function LectureTab(props: {
  view: SummaryView;
  focusBlock: number;
  copy: () => Promise<void>;
  saveAs: (f: SummarySaveFormat) => Promise<{ status: string; message: string; name: string }>;
  start: () => Promise<void>;
  onStarted: () => void;
  onOpenTranscript: () => void;
}) {
  const mounted = useRef(true);
  const acting = useRef(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<{ kind: ResultKind; text: string } | null>(null);
  const [format, setFormat] = useState<SummarySaveFormat>("txt");

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function act(key: string, action: () => Promise<{ kind: ResultKind; text: string } | null>, fail = errorText) {
    if (acting.current) {
      return;
    }
    acting.current = true;
    setBusy(key);
    setMessage(null);
    try {
      const m = await action();
      if (mounted.current) {
        setMessage(m);
      }
    } catch (err) {
      if (mounted.current) {
        setMessage({ kind: "fail", text: fail(err) });
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setBusy("");
      }
    }
  }

  return (
    <LectureTextCard
      view={props.view}
      busy={busy}
      disabled={false}
      format={format}
      onFormat={setFormat}
      onCopy={() =>
        void act("copy", async () => {
          await props.copy();
          return { kind: "ok", text: lectureCopiedText };
        })
      }
      onSave={() =>
        void act("save", async () => {
          const r = saveResultText(await props.saveAs(format));
          return r && { kind: r.ok ? "ok" : "fail", text: r.text };
        })
      }
      onStart={() =>
        // A refusal uses the recordings screen's texts: it is the activity binding's error. The view is asked for
        // again either way: a refusal usually means a run started elsewhere.
        void act(
          "start",
          async () => {
            try {
              await props.start();
            } finally {
              props.onStarted();
            }
            return null;
          },
          actionErrorText,
        )
      }
      onOpenTranscript={props.onOpenTranscript}
      message={message}
      focusBlock={props.focusBlock}
    />
  );
}
