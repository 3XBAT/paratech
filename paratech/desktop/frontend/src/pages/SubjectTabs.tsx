import { type ReactNode, useEffect, useId, useMemo, useState } from "react";
import { knowledgeBaseApi, type KnowledgeLesson } from "../bindings/knowledgeBase.js";
import { homeworkApi } from "../bindings/homework.js";
import { type LibrarySubject, libraryApi } from "../bindings/library.js";
import { callByName } from "../bindings/runtime.js";
import type { OverviewState } from "../libraryView.js";
import { defaultTab, type SubjectTab, tabSuffix, tabTexts, validTab } from "../subjectTabsView.js";
import { SegmentTabs, type SegmentedOption, segmentPanelId, segmentTabId } from "../ui/controls.js";
import { type LessonOpening, SubjectPairsTab, useSubjectPairs } from "./SubjectPairsTab.js";
import { SubjectTasksTab } from "./SubjectTasksTab.js";

type Value = SubjectTab | "";

/**
 * The tabs «Пары · Файлы · Задания» of the page of a subject (TASK-353, ADR-0034 §2). The tab is in the window route
 * (`tab`, "" = not chosen): when it is "" the lessons are read first and the tab is decided once (see defaultTab) and put
 * into the route, so a tab the student chose is never flipped and «Назад» lands on the same tab. Until then the bar is
 * there and the panel says «Загрузка…». The content of «Файлы» is the page's own (`files`). The count on a tab is
 * `num` text, shown only when there is something to count.
 */
export function SubjectTabs(props: {
  subject: LibrarySubject;
  tab: string;
  onTab: (tab: SubjectTab) => void;
  overview: OverviewState;
  /** «Повторить» after a failed overview, and the read after an assignment was added. */
  onRetryOverview: () => void;
  onOverviewChanged: () => void;
  onOpenAssignment?: (assignmentId: string) => void;
  /** A lesson row opens the lesson screen; counters of the lesson screens left and corrected re-read the list. */
  onOpenLesson: (lesson: KnowledgeLesson, opening: LessonOpening) => void;
  lessonsSeen: number;
  lessonsEdited: number;
  files: ReactNode;
}) {
  const base = useId();
  const kb = useMemo(() => knowledgeBaseApi(callByName), []);
  const homework = useMemo(() => homeworkApi(callByName), []);
  const library = useMemo(() => libraryApi(callByName), []);
  const [imports, setImports] = useState(0);
  const { data, retry } = useSubjectPairs(kb, props.subject.id, props.lessonsSeen + props.lessonsEdited + imports);

  const chosen = validTab(props.tab);
  const { onTab } = props;
  useEffect(() => {
    if (chosen === "") {
      const next = defaultTab({ status: data.status, lessons: data.lessons.length });
      if (next !== "") {
        onTab(next);
      }
    }
  }, [chosen, data.status, data.lessons.length, onTab]);

  const tasks = props.overview.status === "ready" && props.overview.overview !== null ? props.overview.overview.assignments.length : 0;
  const pairs = data.status === "ready" ? data.lessons.length : 0;
  const options: readonly SegmentedOption<Value>[] = [
    { value: "pairs", label: <Label text={tabTexts.pairs} count={pairs} /> },
    { value: "files", label: tabTexts.files },
    { value: "tasks", label: <Label text={tabTexts.tasks} count={tasks} /> },
  ];

  return (
    <>
      <div className="kb-tabs">
        <SegmentTabs<Value> label={tabTexts.bar} base={base} options={options} value={chosen} onChange={(t) => t !== "" && onTab(t)} />
      </div>
      <div
        role="tabpanel"
        id={chosen === "" ? undefined : segmentPanelId(base, chosen)}
        aria-labelledby={chosen === "" ? undefined : segmentTabId(base, chosen)}
        aria-busy={chosen === "" ? true : undefined}
      >
        {chosen === "" ? (
          <p className="muted small" role="status">
            {tabTexts.loading}
          </p>
        ) : chosen === "pairs" ? (
          <SubjectPairsTab
            api={kb}
            subject={props.subject}
            data={data}
            onRetry={retry}
            onImported={() => setImports((n) => n + 1)}
            onOpenLesson={props.onOpenLesson}
          />
        ) : chosen === "tasks" ? (
          <SubjectTasksTab
            subjectId={props.subject.id}
            state={props.overview}
            onRetry={props.onRetryOverview}
            onCreated={props.onOverviewChanged}
            onOpenAssignment={props.onOpenAssignment}
            api={homework}
            library={library}
          />
        ) : (
          props.files
        )}
      </div>
    </>
  );
}

function Label(props: { text: string; count: number }) {
  return (
    <>
      {props.text}
      {props.count > 0 && <span className="num">{tabSuffix(props.count)}</span>}
    </>
  );
}
