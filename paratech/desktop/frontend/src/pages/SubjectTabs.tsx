import { type ReactNode, useEffect, useId, useMemo, useState } from "react";
import { knowledgeBaseApi, type KnowledgeLesson } from "../bindings/knowledgeBase.js";
import { homeworkApi } from "../bindings/homework.js";
import { type LibrarySubject, libraryApi } from "../bindings/library.js";
import { callByName } from "../bindings/runtime.js";
import type { OverviewState } from "../libraryView.js";
import { defaultTab, lessonMarks, loadingDelayMs, type SubjectTab, tabSuffix, tabTexts, validTab } from "../subjectTabsView.js";
import { SegmentTabs, type SegmentedOption, segmentPanelId, segmentTabId } from "../ui/controls.js";
import { type LessonOpening, SubjectPairsTab, useSubjectPairs } from "./SubjectPairsTab.js";
import { SubjectTasksTab } from "./SubjectTasksTab.js";
import { useAfterDelay } from "./useAfterDelay.js";

type Value = SubjectTab | "";

/**
 * The tabs «Пары · Файлы · Задания» of the page of a subject (TASK-353, ADR-0034 §2). The tab is in the window route
 * (`tab`, "" = not chosen): when it is "" the lessons are read first and the tab is decided once (see defaultTab) and put
 * into the route, so a tab the student chose is never flipped and «Назад» lands on the same tab. Until then the bar is
 * there (its first tab is the tab stop) and the panel is an empty box of a stable height; «Загрузка…» comes only after
 * loadingDelayMs. The content of «Файлы» is the page's own (`files`). The count on a tab is `num` text, shown only when
 * there is something to count and the tab is decided. «Пары» stays mounted once shown (hidden, not exposed) so a running
 * export is not lost by a switch of the tab; `overview.lessons` give its rows the status chip and «новое».
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

  // The counts come together with the decision (nothing to count while the tab is undecided), so the bar does not move twice.
  const decided = chosen !== "";
  const tasks = decided && props.overview.status === "ready" && props.overview.overview !== null ? props.overview.overview.assignments.length : 0;
  const pairs = decided && data.status === "ready" ? data.lessons.length : 0;
  const options: readonly SegmentedOption<Value>[] = [
    { value: "pairs", label: <Label text={tabTexts.pairs} count={pairs} /> },
    { value: "files", label: tabTexts.files },
    { value: "tasks", label: <Label text={tabTexts.tasks} count={tasks} /> },
  ];

  // «Пары» stays mounted (hidden) once it has been shown: a running export and its result survive a switch of the tab.
  const [pairsShown, setPairsShown] = useState(false);
  if (chosen === "pairs" && !pairsShown) {
    setPairsShown(true);
  }
  const marks = useMemo(() => lessonMarks(props.overview.overview?.lessons ?? []), [props.overview.overview]);
  const slow = useAfterDelay(!decided, loadingDelayMs);

  return (
    <>
      <div className="kb-tabs">
        <SegmentTabs<Value> label={tabTexts.bar} base={base} options={options} value={chosen} onChange={(t) => t !== "" && onTab(t)} />
      </div>
      {pairsShown && (
        <div
          role="tabpanel"
          id={segmentPanelId(base, "pairs")}
          aria-labelledby={segmentTabId(base, "pairs")}
          className="tab-panel"
          hidden={chosen !== "pairs"}
        >
          <SubjectPairsTab
            api={kb}
            subject={props.subject}
            data={data}
            onRetry={retry}
            onImported={() => setImports((n) => n + 1)}
            onOpenLesson={props.onOpenLesson}
            marks={marks}
          />
        </div>
      )}
      {chosen !== "pairs" && (
        <div
          role="tabpanel"
          id={chosen === "" ? undefined : segmentPanelId(base, chosen)}
          aria-labelledby={chosen === "" ? undefined : segmentTabId(base, chosen)}
          aria-busy={chosen === "" ? true : undefined}
          className="tab-panel"
        >
          {chosen === "" ? (
            // A quick read leaves no flash: the text comes only if the wait is long.
            slow && (
              <p className="muted small" role="status">
                {tabTexts.loading}
              </p>
            )
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
      )}
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
