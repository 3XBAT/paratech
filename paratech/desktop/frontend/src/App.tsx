import { type CSSProperties, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  closeLesson,
  initialRoute,
  type PageId,
  railItemOf,
  routeToAssignment,
  routeToLesson,
  routeToLessonTab,
  routeToOpenedAssignment,
  routeToPage,
  routeToSubject,
} from "./nav";
import { AgentPanel } from "./agent/AgentPanel";
import { mentionOpening } from "./agentMentions";
import { PanelResizeHandle, useResizablePanel } from "./agent/PanelResizeHandle";
import type { KnowledgeLesson } from "./bindings/knowledgeBase";
import {
  type CitationOpening,
  keepLessonOpening,
  type LessonOpening,
  lessonScreenKey,
  type LessonTab,
  openingRouteTab,
  recordingLessonKey,
  recordingLessonOpening,
} from "./knowledgeView";
import { keepStartPanel } from "./recordStartView";
import { keepScheduleReview } from "./scheduleSubjectsView";
import { OpenLessonContext } from "./openLesson";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { HomeworkPage } from "./pages/HomeworkPage";
import { KnowledgeBasePage, type LessonOpeningRequest } from "./pages/KnowledgeBasePage";
import { LessonScreen } from "./pages/KnowledgeLessonPage";
import { ModelsPage } from "./pages/ModelsPage";
import { DropZone } from "./pages/DropZone";
import { JournalPage } from "./pages/JournalPage";
import { SchedulePage } from "./pages/SchedulePage";
import { SettingsPage } from "./pages/SettingsPage";
import { SettingsHead, SettingsTabs } from "./pages/SettingsTabs";
import { SubjectsPage } from "./pages/SubjectsPage";
import { TicketsPage } from "./pages/TicketsPage";
import { RecordingsPage } from "./pages/RecordingsPage";
import { TodayPage } from "./pages/TodayPage";
import { needsRemount } from "./navHistory";
import { keepAssignmentEdit, type RecipientNav, RecipientNavContext } from "./recipientNav";
import { keepTeacherFocus, type TeacherFocus } from "./teachersView";
import { useNavHistory } from "./useNavHistory";
import { IconSprite } from "./ui/Icon";
import { Rail } from "./ui/Rail";

function PageView(props: {
  id: PageId;
  visit: number;
  assignment: string;
  subject: string;
  /** Counts the files added elsewhere (a drop on the window): the library pages read again. */
  library: number;
  onOpen: (id: PageId) => void;
  onOpenAssignment: (id: string) => void;
  /** Shows a card of «Задания» or a subject of «Предметы» ("" the list): an entry of the history. */
  onSelectAssignment: (id: string) => void;
  onSelectSubject: (id: string) => void;
  /** «Указать почту» of a refused send: the form of a teacher to open on the subject page, taken once. */
  teacherFocus: TeacherFocus | null;
  onTeacherFocusDone: () => void;
  /** «Завести предметы из расписания» of the schedule page: «Предметы» opens with the review open, taken once. */
  scheduleReview: boolean;
  onOpenScheduleReview: () => void;
  onScheduleReviewTaken: () => void;
  /** «Записать пару» of «Сегодня»: «Записи» opens with the start panel open, taken once. */
  startPanel: boolean;
  onOpenStartPanel: () => void;
  onStartPanelTaken: () => void;
  /** The assignment whose «Изменить» opens on arrival ("" none), taken once. */
  assignmentEdit: string;
  onAssignmentEditDone: () => void;
  /** The place of an answer's source «База знаний» opens on arrival; cleared once taken. */
  sourceJump?: CitationOpening;
  onSourceJumpDone: () => void;
  /** A lesson of «База знаний» opens as a screen of its own over the page (route.lesson, TASK-351). */
  onOpenLesson: (lesson: KnowledgeLesson, opening: LessonOpeningRequest) => void;
  /** A recorded lesson of a subject's «Пары» opens by its recording (TASK-352). */
  onOpenRecording: (recordingId: string) => void;
  /** Count the lesson screens left and the corrections saved there: «База знаний» reads and searches again. */
  lessonsSeen: number;
  lessonsEdited: number;
}) {
  switch (props.id) {
    case "today":
      return <TodayPage onOpen={props.onOpen} onOpenAssignment={props.onOpenAssignment} onRecord={props.onOpenStartPanel} />;
    case "recordings":
      return (
        <RecordingsPage
          onOpen={props.onOpen}
          onOpenAssignment={props.onOpenAssignment}
          openStart={props.startPanel}
          onStartTaken={props.onStartPanelTaken}
        />
      );
    case "subjects":
      return (
        <SubjectsPage
          onOpen={props.onOpen}
          reload={props.library}
          visit={props.visit}
          subjectId={props.subject}
          onSelect={props.onSelectSubject}
          onOpenAssignment={props.onOpenAssignment}
          onOpenLesson={props.onOpenRecording}
          teacherFocus={props.teacherFocus}
          onTeacherFocusDone={props.onTeacherFocusDone}
          openReview={props.scheduleReview}
          onReviewTaken={props.onScheduleReviewTaken}
        />
      );
    case "journal":
      return <JournalPage />;
    case "homework":
      return (
        <HomeworkPage
          openAssignment={props.assignment}
          onSelect={props.onSelectAssignment}
          editAssignment={props.assignmentEdit}
          onEditTaken={props.onAssignmentEditDone}
        />
      );
    case "knowledge":
      return <KnowledgeBasePage visit={props.visit} onOpenLesson={props.onOpenLesson} lessonsSeen={props.lessonsSeen} lessonsEdited={props.lessonsEdited} jump={props.sourceJump} onJumpDone={props.onSourceJumpDone} />;
    case "tickets":
      return <TicketsPage onOpen={props.onOpen} />;
    case "schedule":
      return <SchedulePage onOpen={props.onOpen} onReview={props.onOpenScheduleReview} />;
    case "connections":
      return <ConnectionsPage visit={props.visit} />;
    case "models":
      return <ModelsPage />;
    case "diagnostics":
      return <DiagnosticsPage />;
    case "settings":
      return <SettingsPage />;
    case "profile":
      return <SettingsPage part="profile" />;
    case "appearance":
      return <SettingsPage part="appearance" />;
  }
}

// The window: a quiet side rail on the window background and the current section on a sheet (docs/design/brief.md 5).
export function App() {
  const [route, setRoute] = useState(initialRoute);
  // Counts the rail clicks: a click on the current section takes a page with sub-pages back to its first level.
  const [visit, setVisit] = useState(0);
  const current = route.page;
  const openPage = useCallback((id: PageId) => setRoute(routeToPage(id)), []);
  // Counts the jumps to an assignment: each one opens «Домашние задания» afresh on that assignment.
  const [jump, setJump] = useState(0);
  const openAssignment = useCallback((id: string) => {
    setRoute(routeToAssignment(id));
    setJump((j) => j + 1);
  }, []);
  // A subject or a card opened inside its page is a route too, so «Назад» finds the list again (TASK-202).
  const selectSubject = useCallback((id: string) => setRoute((r) => routeToSubject(r, id)), []);
  const selectAssignment = useCallback((id: string) => setRoute((r) => routeToOpenedAssignment(r, id)), []);
  // «Указать почту» (TASK-238): opens the subject and asks its page for the form of one teacher. The request is state, not
  // part of the route: it is taken once, so «Назад» and a later visit never reopen the form, and the window drops it
  // as soon as the route points elsewhere.
  const [teacherFocus, setTeacherFocus] = useState<TeacherFocus | null>(null);
  const openTeacher = useCallback((subjectId: string, role: TeacherFocus["role"]) => {
    setTeacherFocus({ subjectId, role });
    setRoute((r) => routeToSubject(r, subjectId));
  }, []);
  const teacherFocusDone = useCallback(() => setTeacherFocus(null), []);
  useEffect(() => {
    setTeacherFocus((f) => keepTeacherFocus(f, { page: route.page, subject: route.subject }));
  }, [route.page, route.subject]);
  // «Завести предметы из расписания» (TASK-312): «Предметы» opens with the review open. Same one-shot idea: state, not
  // route; taken once, and dropped as soon as the route leaves the list of «Предметы» (a page or an open subject), so
  // coming back to the list later does not open the panel late.
  const [scheduleReview, setScheduleReview] = useState(false);
  const openScheduleReview = useCallback(() => {
    setScheduleReview(true);
    setRoute(routeToPage("subjects"));
    setVisit((v) => v + 1); // "" shows the last opened subject again
  }, []);
  const scheduleReviewTaken = useCallback(() => setScheduleReview(false), []);
  useEffect(() => {
    setScheduleReview((r) => keepScheduleReview(r, { page: route.page, subject: route.subject }));
  }, [route.page, route.subject]);
  // «Записать пару» (TASK-348): «Записи» opens with the start panel open. The same one-shot idea: state, not route; taken
  // once, and dropped as soon as the route leaves «Записи», so Alt+← never opens the panel late.
  const [startPanel, setStartPanel] = useState(false);
  const openStartPanel = useCallback(() => {
    setStartPanel(true);
    setRoute(routeToPage("recordings"));
  }, []);
  const startPanelTaken = useCallback(() => setStartPanel(false), []);
  useEffect(() => {
    setStartPanel((s) => keepStartPanel(s, { page: route.page }));
  }, [route.page]);
  // «Выбрать предмет» from the agent's card: the assignment opens with «Изменить» already open; the same one-shot idea.
  const [assignmentEdit, setAssignmentEdit] = useState("");
  const openAssignmentFor = useCallback(
    (id: string, opts?: { edit?: boolean }) => {
      setAssignmentEdit(opts?.edit === true ? id : "");
      openAssignment(id);
    },
    [openAssignment],
  );
  const assignmentEditDone = useCallback(() => setAssignmentEdit(""), []);
  useEffect(() => {
    setAssignmentEdit((e) => keepAssignmentEdit(e, { page: route.page, assignment: route.assignment }));
  }, [route.page, route.assignment]);
  // A lesson of «База знаний» (TASK-351): route.lesson is its key; the screen finds the lesson in `lessons`, which every
  // page that opens one fills (a lecture of the student's notes has no other way to be found by its key). What the
  // opening carries besides the route (the found place, the file to mark, the subject form) is one-shot state like the
  // requests above: taken once by the screen and dropped as soon as the route leaves the lesson.
  const lessons = useRef(new Map<string, KnowledgeLesson>());
  const [lessonOpening, setLessonOpening] = useState<LessonOpening | null>(null);
  // Counts the openings: a lesson opened again (another place of it) opens its screen afresh, switching a tab does not.
  const [lessonVisit, setLessonVisit] = useState(0);
  const openLessonKey = useCallback((key: string, o: LessonOpeningRequest) => {
    setLessonOpening({ key, focus: o.focus, materialId: o.materialId, editing: o.editing });
    setLessonVisit((n) => n + 1);
    setRoute((r) => routeToLesson(r, key, openingRouteTab(o.focus, o.materialId)));
  }, []);
  const openLesson = useCallback(
    (lesson: KnowledgeLesson, o: LessonOpeningRequest) => {
      lessons.current.set(lesson.key, lesson);
      openLessonKey(lesson.key, o);
    },
    [openLessonKey],
  );
  // Every «Открыть конспект / текст лекции / расшифровку» (TASK-352): the lesson of a recording by its id; the screen
  // loads it by its key (lessonByKey), so no page has to hold the lesson.
  const openRecordingLesson = useCallback(
    (recordingId: string, tab?: LessonTab, index?: number) => {
      const { key, ...o } = recordingLessonOpening(recordingId, tab, index);
      openLessonKey(key, o);
    },
    [openLessonKey],
  );
  const openRecording = useCallback((recordingId: string) => openRecordingLesson(recordingId), [openRecordingLesson]);
  // A place of an answer's source (TASK-296): a recording's place opens its lesson at once; a note or a pair is found by
  // its ids first, which «База знаний» does (it opens afresh and takes the jump once), then the lesson opens over it.
  const [sourceJump, setSourceJump] = useState<CitationOpening | undefined>(undefined);
  const openSource = useCallback(
    (o: CitationOpening) => {
      if (o.kind === "recording") {
        openLessonKey(recordingLessonKey(o.recordingId), { focus: o.focus, materialId: "", editing: false });
        return;
      }
      setSourceJump(o);
      setRoute(routeToPage("knowledge"));
      setJump((j) => j + 1);
    },
    [openLessonKey],
  );
  const sourceJumpDone = useCallback(() => setSourceJump(undefined), []);
  const lessonOpeningTaken = useCallback(() => setLessonOpening(null), []);
  // The button that closed the lesson screen is gone with it: the focus goes to the sheet, so the keyboard does not fall
  // to the body. Set by «Назад» and by a deletion only; a rail click keeps the focus on the rail.
  const sheet = useRef<HTMLElement>(null);
  const refocusSheet = useRef(false);
  // Alt+←/→ and the mouse side buttons walk the history of routes (TASK-201).
  const navHistory = useNavHistory(route, setRoute, (r, from) => {
    if (needsRemount(r, from)) {
      setJump((j) => j + 1);
    }
  });
  // The recording was deleted on the lesson screen: back to the section it was opened from, and the lesson leaves the
  // history (Alt+← must not return to it). That section is read afresh, its lists still hold the recording.
  const lessonDeleted = useCallback(
    (key: string) => {
      lessons.current.delete(key);
      refocusSheet.current = true;
      navHistory.lessonDeleted(key);
      setJump((j) => j + 1);
    },
    [navHistory],
  );
  // «Назад» of the lesson screen.
  const lessonBack = useCallback(() => {
    refocusSheet.current = true;
    setRoute(closeLesson);
  }, []);
  // A mention pill of the agent's thread (TASK-247): the object itself where a route opens it (a subject, an assignment,
  // a pair's lesson, TASK-352), else the closest existing page; a deleted object meets its normal state.
  const openMention = useCallback(
    (kind: string, id: string) => {
      const o = mentionOpening(kind, id);
      if (o === null) {
        return;
      }
      if (o.type === "subject") {
        selectSubject(o.id);
      } else if (o.type === "assignment") {
        openAssignment(o.id);
      } else if (o.type === "pair") {
        openSource({ kind: "pair", lessonId: o.lessonId });
      } else {
        openPage(o.page);
      }
    },
    [selectSubject, openAssignment, openSource, openPage],
  );
  const openLessonTab = useCallback((tab: string) => setRoute((r) => routeToLessonTab(r, tab)), []);
  // «База знаний» under the lesson reads its lists when the lesson screen is left, and searches again after a correction.
  const [lessonsSeen, setLessonsSeen] = useState(0);
  const [lessonsEdited, setLessonsEdited] = useState(0);
  const lessonChanged = useCallback((l: KnowledgeLesson) => {
    lessons.current.set(l.key, l);
    setLessonsEdited((n) => n + 1);
  }, []);
  // The crumbs «Предметы / <предмет>» are routes: «Предметы» is the rail click, the subject opens in «Предметы».
  const lessonNav = useMemo(
    () => ({
      onOpenSubjects: () => {
        openPage("subjects");
        setVisit((v) => v + 1);
      },
      onOpenSubject: selectSubject,
    }),
    [openPage, selectSubject],
  );
  const lastLesson = useRef("");
  useEffect(() => {
    setLessonOpening((o) => keepLessonOpening(o, { lesson: route.lesson }));
    if (lastLesson.current !== "" && route.lesson === "") {
      setLessonsSeen((n) => n + 1);
      if (refocusSheet.current) {
        sheet.current?.focus();
      }
    }
    refocusSheet.current = false;
    lastLesson.current = route.lesson;
  }, [route.lesson]);
  const recipientNav = useMemo<RecipientNav>(
    () => ({
      openTeacher,
      openAssignment: openAssignmentFor,
      openSubjects: () => {
        setRoute(routeToPage("subjects"));
        setVisit((v) => v + 1);
      },
      openAssignments: () => openPage("homework"),
    }),
    [openTeacher, openAssignmentFor, openPage],
  );
  // Counts the files the drop zone added: «Предметы» reads the library again.
  const [library, setLibrary] = useState(0);
  // The page buttons the agent offers (TASK-324): a press does what the page's own button does, or opens that page.
  const offerNav = useMemo(
    () => ({
      openSummary,
      openTranscript,
      // As a rail click: a page with sub-pages starts at its first level.
      openPage: (id: PageId) => {
        openPage(id);
        setVisit((v) => v + 1);
      },
      startRecording: openStartPanel,
    }),
    [openSummary, openTranscript, openPage, openStartPanel],
  );
  const libraryChanged = useCallback(() => setLibrary((n) => n + 1), []);
  const panel = useResizablePanel();
  const open = panel.pref.open;
  const width = panel.width;
  const overlay = route.lesson !== "";
  const inSettings = railItemOf(current).id === "settings";

  const pageView = (
    <OpenLessonContext.Provider value={openRecordingLesson}>
      <PageView
        id={current}
        visit={visit}
        assignment={route.assignment}
        subject={route.subject}
        library={library}
        onOpen={openPage}
        onOpenAssignment={openAssignment}
        onSelectAssignment={selectAssignment}
        onSelectSubject={selectSubject}
        teacherFocus={teacherFocus}
        onTeacherFocusDone={teacherFocusDone}
        scheduleReview={scheduleReview}
        onOpenScheduleReview={openScheduleReview}
        onScheduleReviewTaken={scheduleReviewTaken}
        startPanel={startPanel}
        onOpenStartPanel={openStartPanel}
        onStartPanelTaken={startPanelTaken}
        assignmentEdit={assignmentEdit}
        onAssignmentEditDone={assignmentEditDone}
        sourceJump={sourceJump}
        onSourceJumpDone={sourceJumpDone}
        onOpenLesson={openLesson}
        onOpenRecording={openRecording}
        lessonsSeen={lessonsSeen}
        lessonsEdited={lessonsEdited}
      />
    </OpenLessonContext.Provider>
  );

  return (
    <RecipientNavContext.Provider value={recipientNav}>
    <div
      className={`shell with-agent${open ? "" : " agent-closed"}${panel.dragging ? " agent-dragging" : ""}`}
      style={{ "--ag-w": `${open ? width : 0}px` } as CSSProperties}
    >
      <IconSprite />
      <Rail
        current={current}
        onOpen={(id) => {
          openPage(id);
          setVisit((v) => v + 1);
        }}
      />
      <main className="sheet" ref={sheet} tabIndex={-1}>
        {/* A new key per section replays the entry animation. The section stays mounted under an open lesson, so
            «Назад» finds it as the user left it. */}
        <div key={`${current}:${jump}`} className="screen" hidden={overlay}>
          {inSettings && <SettingsHead />}
          {inSettings ? (
            <div className="set-layout">
              <SettingsTabs current={current} onOpen={openPage} />
              <div className="set-body">{pageView}</div>
            </div>
          ) : (
            pageView
          )}
        </div>
        {route.lesson !== "" && (
          // The tab is not in the key: switching tabs keeps the screen; another lesson, or the same one opened again
          // at another place, opens it afresh.
          <div key={lessonScreenKey(route.lesson, lessonVisit)} className="screen">
            <LessonScreen
              lessonKey={route.lesson}
              origin={route.page}
              tab={route.tab}
              cache={lessons.current}
              opening={lessonOpening}
              onOpeningTaken={lessonOpeningTaken}
              onTab={openLessonTab}
              onBack={lessonBack}
              onChanged={lessonChanged}
              onDeleted={lessonDeleted}
              onOpenAssignment={openAssignment}
              onCheckSound={() => openPage("diagnostics")}
              nav={lessonNav}
            />
          </div>
        )}
      </main>
      <AgentPanel page={current} assignmentId={route.assignment} open={open} reveal={panel.reveal} hide={panel.hide} onOpenAssignment={openAssignment} onOpenSource={openSource} onOpenMention={openMention} offerNav={offerNav} onLibraryChanged={libraryChanged} />
      <PanelResizeHandle panel={panel} />
      <DropZone onImported={() => setLibrary((n) => n + 1)} />
    </div>
    </RecipientNavContext.Provider>
  );
}
