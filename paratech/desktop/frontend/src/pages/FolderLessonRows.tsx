import { useLayoutEffect, useRef } from "react";
import type { KnowledgeFolderLesson, KnowledgeLesson } from "../bindings/knowledgeBase.js";
import { noFocus, type LessonTab } from "../knowledgeView.js";
import { folderLessonMeta, folderLessonTexts, folderLessonTitle, type FolderLessonsState, texts } from "../libraryView.js";
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
 * The rows of one folder. The whole row is a button (the title, the date and length, «новое», the quiet status of a lesson
 * that is not processed yet); the two icon actions sit beside it, not inside it, so the buttons are not nested. No «⋯», no
 * drag, no checkbox: the row is changed through the recording itself. `marks` are the statuses of the overview (see
 * lessonMarks). The icons are named after the lesson (aria-label); their tooltips stay the short phrases.
 */
export function FolderLessonRows(props: {
  rows: readonly KnowledgeFolderLesson[];
  marks?: ReadonlyMap<string, LessonMark>;
  onOpen: (lesson: KnowledgeLesson, opening: LessonOpening) => void;
}) {
  return (
    <div role="list">
      {props.rows.map(({ lesson }) => {
        const mark = props.marks?.get(lesson.recordingId);
        const chip = mark?.chip ?? null;
        const title = folderLessonTitle(lesson);
        const meta = folderLessonMeta(lesson);
        return (
          <div className="lrow lrow-lesson" role="listitem" key={lesson.key}>
            <button type="button" className="lrow-open" title={folderLessonTexts.openRow} onClick={() => props.onOpen(lesson, lessonRowOpening())}>
              <Icon name="rec" />
              <span className="lrow-tx">
                <span className="lrow-line">
                  <span className="lt">{title}</span>
                  {mark?.fresh === true && <span className="fresh">{texts.fresh}</span>}
                </span>
                {meta !== "" && <span className="lm num">{meta}</span>}
              </span>
              {chip !== null && (
                <StatusChip kind={chip.kind} icon={chip.icon}>
                  {chip.label}
                </StatusChip>
              )}
            </button>
            {lesson.summaryReady && (
              <IconAction
                icon="doc"
                label={`${folderLessonTexts.openSummary}: ${title}`}
                tipText={folderLessonTexts.openSummary}
                onClick={() => props.onOpen(lesson, lessonRowOpening("summary"))}
              />
            )}
            <IconAction
              icon="wave"
              label={`${folderLessonTexts.openRecording}: ${title}`}
              tipText={folderLessonTexts.openRecording}
              onClick={() => props.onOpen(lesson, lessonRowOpening("transcript"))}
            />
          </div>
        );
      })}
    </div>
  );
}

/**
 * The state of the read as one quiet line: a failure of the first read («Не удалось загрузить пары предмета.») or of a
 * re-read (the rows stay, «Список пар не обновился…»), each with an icon «Повторить»; a slow first read with a small
 * «Загрузка пар…»; nothing otherwise. While «Повторить» runs the line and its icon stay (the icon is off but keeps the
 * keyboard focus, see IconAction `disabledReason`) and the text says «Загрузка пар…». `slow` keeps a quick read from
 * flashing the text.
 */
export function FolderLessonsNotice(props: { failed: boolean; stale?: boolean; retrying?: boolean; slow: boolean; onRetry: () => void }) {
  const stale = props.stale === true;
  if (props.failed || stale) {
    const retrying = props.retrying === true;
    return (
      <div className="lrow-error">
        {retrying ? (
          <p className="small muted" role="status">
            {folderLessonTexts.loading}
          </p>
        ) : (
          <ResultLine kind={props.failed ? "fail" : "wait"} role={props.failed ? "alert" : "status"}>
            {props.failed ? folderLessonTexts.failed : folderLessonTexts.stale}
          </ResultLine>
        )}
        <IconAction
          icon="retry"
          label={folderLessonTexts.retry}
          disabled={retrying}
          disabledReason={retrying ? folderLessonTexts.loading : undefined}
          onClick={props.onRetry}
        />
      </div>
    );
  }
  return props.slow ? (
    <p className="small muted" role="status">
      {folderLessonTexts.loading}
    </p>
  ) : null;
}

/**
 * The line of the page. It sits in a box that is always there: when «Повторить» had the focus and the line goes away after
 * the answer, the focus moves to the box (the next Tab goes on from there) instead of falling to the page.
 */
export function FolderLessonsLine(props: { state: FolderLessonsState; onRetry: () => void }) {
  const { state } = props;
  const loading = state.rows === null && !state.failed;
  const slow = useAfterDelay(loading, loadingDelayMs);
  const box = useRef<HTMLDivElement>(null);
  const keep = useRef(false);
  const shown = state.failed || state.stale;
  const wasShown = useRef(shown);

  useLayoutEffect(() => {
    if (wasShown.current && !shown && keep.current) {
      const at = document.activeElement;
      if (at === null || at === document.body) {
        box.current?.focus({ preventScroll: true });
      }
    }
    if (!shown) {
      keep.current = false;
    }
    wasShown.current = shown;
  }, [shown]);

  return (
    <div ref={box} className="lrow-notice" tabIndex={-1}>
      <FolderLessonsNotice
        failed={state.failed}
        stale={state.stale}
        retrying={state.retrying}
        slow={slow}
        onRetry={() => {
          keep.current = box.current?.contains(document.activeElement) ?? false;
          props.onRetry();
        }}
      />
    </div>
  );
}
