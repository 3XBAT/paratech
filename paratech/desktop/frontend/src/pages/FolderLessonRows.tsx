import type { KnowledgeFolderLesson, KnowledgeLesson } from "../bindings/knowledgeBase.js";
import { noFocus, type LessonTab } from "../knowledgeView.js";
import { folderLessonMeta, folderLessonTexts, folderLessonTitle, type FolderLessonsState } from "../libraryView.js";
import { type LessonMark, loadingDelayMs } from "../subjectTabsView.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { IconAction } from "../ui/IconAction.js";
import { StatusChip } from "../ui/StatusChip.js";
import type { LessonOpening } from "./SubjectPairsTab.js";
import { useAfterDelay } from "./useAfterDelay.js";

// The link rows of the recorded lessons in the folders «Лекции» / «Семинары» / «Лабораторные» (TASK-430, ADR-0034
// amendment 2026-11-10). A row is derived from the recording and stores nothing: it only opens the lesson screen. Titles
// are recorded or typed text: text nodes only.

/** What a row opens the lesson screen with: no tab (the screen chooses) or one of its tabs; no place inside it, no editing. */
export function lessonRowOpening(tab?: LessonTab): LessonOpening {
  return { focus: tab === undefined ? noFocus : { tab, index: -1 }, materialId: "", editing: false };
}

/**
 * The rows of one folder. The whole row is a button (the title, the date and length, the quiet status of a lesson that is
 * not processed yet); the two icon actions sit beside it, not inside it, so the buttons are not nested. No «⋯», no drag,
 * no checkbox: the row is changed through the recording itself. `marks` are the statuses of the overview (see lessonMarks).
 */
export function FolderLessonRows(props: {
  rows: readonly KnowledgeFolderLesson[];
  marks?: ReadonlyMap<string, LessonMark>;
  onOpen: (lesson: KnowledgeLesson, opening: LessonOpening) => void;
}) {
  return (
    <div role="list">
      {props.rows.map(({ lesson }) => {
        const chip = props.marks?.get(lesson.recordingId)?.chip ?? null;
        const meta = folderLessonMeta(lesson);
        return (
          <div className="lrow lrow-lesson" role="listitem" key={lesson.key}>
            <button type="button" className="lrow-open" onClick={() => props.onOpen(lesson, lessonRowOpening())}>
              <Icon name="rec" />
              <span className="lrow-tx">
                <span className="lt">{folderLessonTitle(lesson)}</span>
                {meta !== "" && <span className="lm num">{meta}</span>}
              </span>
              {chip !== null && (
                <StatusChip kind={chip.kind} icon={chip.icon}>
                  {chip.label}
                </StatusChip>
              )}
            </button>
            {lesson.summaryReady && (
              <IconAction icon="doc" label={folderLessonTexts.openSummary} onClick={() => props.onOpen(lesson, lessonRowOpening("summary"))} />
            )}
            <IconAction icon="play" label={folderLessonTexts.openRecording} onClick={() => props.onOpen(lesson, lessonRowOpening("transcript"))} />
          </div>
        );
      })}
    </div>
  );
}

/**
 * The state of the read as one quiet line: a failure with an icon «Повторить» (the files stay shown), a slow read with a
 * small «Загрузка пар…»; nothing otherwise. `slow` keeps a quick read from flashing the text.
 */
export function FolderLessonsNotice(props: { failed: boolean; slow: boolean; onRetry: () => void }) {
  if (props.failed) {
    return (
      <div className="lrow-error">
        <ResultLine kind="fail" role="alert">
          {folderLessonTexts.failed}
        </ResultLine>
        <IconAction icon="retry" label={folderLessonTexts.retry} onClick={props.onRetry} />
      </div>
    );
  }
  return props.slow ? (
    <p className="small muted" role="status">
      {folderLessonTexts.loading}
    </p>
  ) : null;
}

export function FolderLessonsLine(props: { state: FolderLessonsState; onRetry: () => void }) {
  const loading = props.state.rows === null && !props.state.failed;
  const slow = useAfterDelay(loading, loadingDelayMs);
  return <FolderLessonsNotice failed={props.state.failed} slow={slow} onRetry={props.onRetry} />;
}
