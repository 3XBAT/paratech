import { useEffect, useId, useMemo, useRef, useState } from "react";
import { actionErrorText } from "../activityView";
import { type ActivityApi, activityApi } from "../bindings/activity";
import { type KnowledgeBaseApi, knowledgeBaseApi, type KnowledgeLesson } from "../bindings/knowledgeBase";
import { notesApi } from "../bindings/notes";
import { callByName } from "../bindings/runtime";
import {
  lectureTextApi,
  type SummaryApi,
  summaryApi,
  type SummarySaveFormat,
  type SummaryView,
} from "../bindings/summary";
import {
  errorText as knowledgeErrorText,
  isLessonTab,
  isNoteOnly,
  keepLessonOpening,
  type LessonFocus,
  lessonCrumbRoot,
  lessonCrumbs,
  lessonHeading,
  lessonMeta,
  type LessonOpening,
  lessonScreenTexts,
  type LessonTab,
  lessonTabs,
  noFocus,
  noteLectureHeading,
  noteLectureMeta,
  openingTab,
  recordingIdOfKey,
  shownBlock,
  summaryFocus,
} from "../knowledgeView";
import { lectureCopiedText } from "../lectureTextView";
import { lectureOf, readLectureNotes, visibleNotes } from "../ownNotesView";
import {
  errorText,
  nextPollMs,
  noSummaryHint,
  redoTexts,
  retryAction,
  retryClick,
  type RetryStep,
} from "../summaryView";
import { saveResultText } from "../transcriptView";
import { Breadcrumbs, type Crumb } from "../ui/Breadcrumbs";
import { Button } from "../ui/Button";
import {
  Banner,
  Dialog,
  type ResultKind,
  ResultLine,
  SegmentTabs,
  segmentPanelId,
  segmentTabId,
} from "../ui/controls";
import { RowMenu } from "../ui/RowMenu";
import { LessonInfoForm, ReadyMarks } from "./KnowledgeParts";
import { LectureTextCard } from "./LectureTextParts";
import { OwnNotes } from "./OwnNotes";
import { HomeworkCard, NoSpeechCard, SummaryCard, SummaryTools } from "./SummaryParts";
import { TranscriptPage } from "./TranscriptPage";

/** Where the crumbs «Предметы / <предмет>» lead: the window's routes (history entries). */
export interface LessonNav {
  onOpenSubjects: () => void;
  onOpenSubject: (subjectId: string) => void;
}

/** The crumbs of a lesson: the levels above it are buttons, the last one is the lesson itself. */
function crumbItems(
  l: Pick<KnowledgeLesson, "subject" | "librarySubjectId" | "librarySubjectState">,
  heading: string,
  nav: LessonNav,
): Crumb[] {
  return lessonCrumbs(l, heading).map((c) => {
    const go = c.go;
    if (go === null) {
      return { label: c.label };
    }
    return { label: c.label, onClick: go.kind === "subjects" ? nav.onOpenSubjects : () => nav.onOpenSubject(go.id) };
  });
}

// The screen a route `lesson` shows (TASK-351). The core has no call that opens a lesson by its key, so the lesson is
// found in the window's memory of the lessons it opened (`cache`, filled by every page that opens one), or, for a
// recorded lesson whose key is `recording:<recordingId>`, read through `lesson(recordingId)`. A lecture of the student's
// notes has no recording, so once the window has forgotten it (a cold link) it cannot be found: the screen says so and
// offers the way to the subjects. A by-key binding would remove that limit (follow-up for the core).
export function LessonScreen(props: {
  lessonKey: string;
  /** Route.tab: a LessonTab, "" while the student has not chosen one. */
  tab: string;
  cache: ReadonlyMap<string, KnowledgeLesson>;
  /** The one-shot data of the opening; taken once (onOpeningTaken), then the window drops it. */
  opening: LessonOpening | null;
  onOpeningTaken: () => void;
  onTab: (tab: LessonTab) => void;
  onBack: () => void;
  onChanged: (l: KnowledgeLesson) => void;
  onOpenAssignment: (assignmentId: string) => void;
  onCheckSound: () => void;
  nav: LessonNav;
}) {
  const key = props.lessonKey;
  const api = useMemo(() => knowledgeBaseApi(callByName), []);
  const cached = props.cache.get(key);
  const recordingId = recordingIdOfKey(key);
  const [opening] = useState<LessonOpening | null>(() => keepLessonOpening(props.opening, { lesson: key }));
  const taken = props.onOpeningTaken;
  const [fetched, setFetched] = useState<KnowledgeLesson | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (opening !== null) {
      taken();
    }
  }, [opening, taken]);

  useEffect(() => {
    if (cached !== undefined || recordingId === "") {
      return;
    }
    let stopped = false;
    api.lesson(recordingId).then(
      (l) => {
        if (!stopped) {
          setFetched(l);
        }
      },
      (err) => {
        if (!stopped) {
          setError(knowledgeErrorText(err));
        }
      },
    );
    return () => {
      stopped = true;
    };
  }, [api, cached, recordingId]);

  const lesson = cached ?? fetched;
  if (lesson === null) {
    const text = recordingId === "" ? lessonScreenTexts.gone : error;
    return (
      <section className="page tr sm kb-lesson-page">
        <Breadcrumbs items={[{ label: lessonCrumbRoot, onClick: props.nav.onOpenSubjects }, { label: "Пара" }]} />
        {text === "" ? (
          <p className="muted" role="status">
            {lessonScreenTexts.loading}
          </p>
        ) : (
          <div className="kb-gone">
            <Banner kind="fail" role="alert">
              {text}
            </Banner>
            <div className="acts start">
              <Button onClick={props.nav.onOpenSubjects}>{lessonScreenTexts.toSubjects}</Button>
            </div>
          </div>
        )}
      </section>
    );
  }

  if (isNoteOnly(lesson)) {
    return <NoteLecturePage lesson={lesson} materialId={opening?.materialId} nav={props.nav} />;
  }

  return (
    <KnowledgeLessonPage
      api={api}
      lesson={lesson}
      stale={cached !== undefined}
      tab={props.tab}
      focus={opening?.focus ?? noFocus}
      materialId={opening?.materialId}
      editing={opening?.editing ?? false}
      nav={props.nav}
      onTab={props.onTab}
      onBack={props.onBack}
      onChanged={props.onChanged}
      onOpenAssignment={props.onOpenAssignment}
      onCheckSound={props.onCheckSound}
    />
  );
}

// One lesson of «База знаний» (TASK-091): «Мой конспект» (first), «Конспект», «Текст лекции», «Расшифровка» and «Задания»
// as tabs over the summary and transcript viewers (TASK-087/090/079), opened at the place a search found. Works
// offline: everything comes from the core on this computer. Model text is rendered by those viewers as React text only.
export function KnowledgeLessonPage(props: {
  api: KnowledgeBaseApi;
  lesson: KnowledgeLesson;
  /** The lesson comes from the window's memory and may be older than the core's: it is read again. */
  stale: boolean;
  /** Route.tab: a LessonTab once the student chose or a search named one; "" for the opening tab of openingTab. */
  tab: string;
  focus: LessonFocus;
  /** The file of the student's notes a search hit opened the page at; "" for none. */
  materialId?: string;
  /** Opens with the subject and kind form (from «Без предмета»). */
  editing: boolean;
  nav: LessonNav;
  /** The student chose a tab: the route changes without a new entry of the history. */
  onTab: (tab: LessonTab) => void;
  /** Where the embedded transcript steps up to (the section under the lesson). */
  onBack: () => void;
  /** The subject or kind was saved: the lists are out of date. */
  onChanged: (l: KnowledgeLesson) => void;
  onOpenAssignment: (assignmentId: string) => void;
  onCheckSound: () => void;
}) {
  const id = props.lesson.recordingId;
  const summary = useMemo(() => summaryApi(callByName), []);
  const lecture = useMemo(() => lectureTextApi(callByName), []);
  const activity = useMemo(() => activityApi(callByName), []);
  const notes = useMemo(() => notesApi(callByName), []);
  const tabsId = useId();
  const [lesson, setLesson] = useState(props.lesson);
  const [lessonError, setLessonError] = useState("");
  const [fresh, setFresh] = useState(!props.stale);
  const [editing, setEditing] = useState(props.editing);
  const [hasNotes, setHasNotes] = useState<boolean | null>(null);
  const [auto, setAuto] = useState<LessonTab | null>(null);
  const [view, setView] = useState<SummaryView | null>(null);
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  const content = useRef<HTMLDivElement>(null);
  const scrolled = useRef(false);
  const needsAuto = useRef(!isLessonTab(props.tab));

  // The list may be older than the lesson: read it again (a deleted recording says so).
  useEffect(() => {
    if (!props.stale) {
      return;
    }
    let stopped = false;
    props.api.lesson(id).then(
      (l) => {
        if (!stopped) {
          setLesson(l);
          setFresh(true);
        }
      },
      (err) => {
        if (!stopped) {
          setLessonError(knowledgeErrorText(err));
          setFresh(true);
        }
      },
    );
    return () => {
      stopped = true;
    };
  }, [props.api, props.stale, id]);

  // The opening tab depends on whether the student has notes here: read once, only when no tab was asked for. A failed
  // read counts as no notes (the notes tab shows its own refusal if it is the one left).
  useEffect(() => {
    if (!needsAuto.current) {
      return;
    }
    let stopped = false;
    readLectureNotes(notes, { kind: "recording", recordingId: id }).then(
      (list) => {
        if (!stopped) {
          setHasNotes(visibleNotes(list, "").length > 0);
        }
      },
      () => {
        if (!stopped) {
          setHasNotes(false);
        }
      },
    );
    return () => {
      stopped = true;
    };
  }, [notes, id]);

  // Decided once, when both the notes and the fresh lesson are known; a tab the student chose is never replaced.
  useEffect(() => {
    if (needsAuto.current && auto === null && hasNotes !== null && fresh) {
      setAuto(openingTab({ routeTab: "", hasNotes, lesson }));
    }
  }, [auto, hasNotes, fresh, lesson]);

  const tab: LessonTab | null = isLessonTab(props.tab) ? props.tab : auto;

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
      <Breadcrumbs items={crumbItems(lesson, lessonHeading(lesson), props.nav)} />
      <div className="head">
        <div>
          <h1 className="kb-heading">{lessonHeading(lesson)}</h1>
          <p className="tr-sub">
            <span className="tr-title">{lessonMeta(lesson)}</span>
          </p>
          <ReadyMarks lesson={lesson} />
        </div>
        <div className="acts">
          <RowMenu
            label="Ещё действия с парой"
            title="Ещё"
            items={[{ key: "edit", label: "Исправить предмет или тип пары" }]}
            onPick={() => setEditing((e) => !e)}
          />
        </div>
      </div>
      {lessonError !== "" && (
        <div className="kb-gone">
          <Banner kind="fail" role="alert">
            {lessonError}
          </Banner>
          <div className="acts start">
            <Button onClick={props.nav.onOpenSubjects}>{lessonScreenTexts.toSubjects}</Button>
          </div>
        </div>
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
      {tab === null ? (
        <p className="muted" role="status">
          {lessonScreenTexts.loading}
        </p>
      ) : (
        <>
          <div className="kb-tabs">
            <SegmentTabs label="Раздел пары" base={tabsId} options={lessonTabs} value={tab} onChange={props.onTab} />
          </div>
          <div ref={content} role="tabpanel" id={segmentPanelId(tabsId, tab)} aria-labelledby={segmentTabId(tabsId, tab)}>
            {tab === "transcript" ? (
              <TranscriptPage
                recordingId={id}
                embedded
                focusSegment={focus.tab === "transcript" ? focus.index : -1}
                onBack={props.onBack}
                onOpenSummary={() => props.onTab("summary")}
              />
            ) : tab === "notes" ? (
              // The student's own notes do not depend on the summary: no «Загрузка…» and no summary error here.
              <OwnNotes
                lecture={{ kind: "recording", recordingId: id }}
                librarySubjectId={lesson.librarySubjectId}
                librarySubjectState={lesson.librarySubjectState}
                focusMaterialId={props.materialId}
              />
            ) : (
              <>
                {error && (
                  <Banner kind="fail" role="alert">
                    {error}
                  </Banner>
                )}
                {view === null ? (
                  !error && <p className="muted">{lessonScreenTexts.loading}</p>
                ) : view.noSpeech ? (
                  <NoSpeechCard view={view} onCheckSound={props.onCheckSound} />
                ) : tab === "lecture" ? (
                  <LectureTab
                    view={view}
                    focusBlock={focusBlock}
                    copy={() => lecture.copy(id)}
                    saveAs={(f) => lecture.saveAs(id, f)}
                    start={() => activity.retryPostProcessing(id, "lectureText")}
                    onStarted={() => setRefresh((n) => n + 1)}
                    onOpenTranscript={() => props.onTab("transcript")}
                  />
                ) : (
                  <SummaryTab
                    key={tab}
                    tab={tab}
                    view={view}
                    api={summary}
                    activity={activity}
                    focusSection={focusSection}
                    focusPoint={focusPoint}
                    onStarted={() => setRefresh((n) => n + 1)}
                    onOpenAssignment={props.onOpenAssignment}
                  />
                )}
              </>
            )}
          </div>
        </>
      )}
    </section>
  );
}

// A lecture known only from the student's notes (TASK-155): no recording, so no summary, transcript or tabs and no
// call that needs a recording id; only the «Мой конспект» block, opened by the lecture's ids.
export function NoteLecturePage(props: { lesson: KnowledgeLesson; materialId?: string; nav: LessonNav }) {
  const l = props.lesson;
  return (
    <section className="page tr sm kb-lesson-page">
      <Breadcrumbs items={crumbItems(l, noteLectureHeading(l), props.nav)} />
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

interface Message {
  kind: ResultKind;
  text: string;
}

// The «Конспект» and «Задания» tabs: the summary with its tools, the homework found, and the retry of a step that
// failed (or the redo of a ready summary, after a confirmation), as on the old summary page. One action at a time.
function SummaryTab(props: {
  tab: "summary" | "homework";
  view: SummaryView;
  api: SummaryApi;
  activity: ActivityApi;
  focusSection: number;
  focusPoint: number;
  onStarted: () => void;
  onOpenAssignment: (assignmentId: string) => void;
}) {
  const { api, view } = props;
  const id = view.recordingId;
  const mounted = useRef(true);
  const acting = useRef(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<Message | null>(null);
  const [askingRedo, setAskingRedo] = useState(false);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function act(key: string, action: () => Promise<Message | null>) {
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
        setMessage({ kind: "fail", text: errorText(err) });
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setBusy("");
      }
    }
  }

  const copy = () =>
    act("copy", async () => {
      await api.copyAll(id);
      return { kind: "ok", text: "Конспект скопирован в буфер обмена." };
    });

  const save = (format: SummarySaveFormat) =>
    act("save", async () => {
      const r = saveResultText(await api.saveAs(id, format));
      return r && { kind: r.ok ? "ok" : "fail", text: r.text };
    });

  // Retry and redo report failures with the recordings screen's texts: they are the activity binding's errors.
  const start = (step: RetryStep, call: () => Promise<void>) =>
    act(`retry:${step}`, async () => {
      let refused: Message | null = null;
      try {
        await call();
      } catch (err) {
        refused = { kind: "fail", text: actionErrorText(err) };
      }
      // Reload on a refusal too: it usually means a run started elsewhere (the row) and the polling has stopped.
      props.onStarted();
      return refused;
    });

  const retry = (step: RetryStep) =>
    retryClick(view, step, {
      retry: (work) => void start(work, () => props.activity.retryPostProcessing(id, work)),
      askRedo: () => setAskingRedo(true),
    });

  const redo = () => {
    setAskingRedo(false);
    void start("summary", () => props.activity.redoSummary(id));
  };

  // The dialog closes by itself once the summary is no longer one to make again, and does not come back on its own.
  const redoOffered = retryAction(view, "summary")?.kind === "redo";
  useEffect(() => {
    if (!redoOffered) {
      setAskingRedo(false);
    }
  }, [redoOffered]);

  const retrying: RetryStep | "" = busy === "retry:summary" ? "summary" : busy === "retry:homework" ? "homework" : "";
  const idle = busy === "";

  return (
    <>
      {props.tab === "summary" ? (
        <>
          <SummaryTools
            ready={view.summary.ready}
            idle={idle}
            notReady={noSummaryHint}
            busy={busy}
            message={message}
            onCopy={() => void copy()}
            onSave={(f) => void save(f)}
          />
          <SummaryCard
            view={view}
            focusSection={props.focusSection}
            focusPoint={props.focusPoint}
            onRetry={retry}
            retrying={retrying}
            retryDisabled={!idle}
          />
        </>
      ) : (
        <>
          {message && (
            <ResultLine kind={message.kind} role={message.kind === "fail" ? "alert" : "status"}>
              {message.text}
            </ResultLine>
          )}
          <HomeworkCard
            view={view}
            onOpenAssignment={props.onOpenAssignment}
            onRetry={retry}
            retrying={retrying}
            retryDisabled={!idle}
          />
        </>
      )}
      <Dialog
        open={askingRedo && redoOffered}
        title={redoTexts.title}
        confirmLabel={redoTexts.confirm}
        cancelLabel={redoTexts.cancel}
        confirmDisabled={!idle}
        onConfirm={redo}
        onCancel={() => setAskingRedo(false)}
      >
        <p>{redoTexts.body}</p>
      </Dialog>
    </>
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
