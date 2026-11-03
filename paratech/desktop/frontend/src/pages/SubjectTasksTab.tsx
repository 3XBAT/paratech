import { useEffect, useRef, useState } from "react";
import type { AssignmentItem, HomeworkApi } from "../bindings/homework.js";
import type { LibraryApi } from "../bindings/library.js";
import { type OverviewState, texts } from "../libraryView.js";
import { addTaskId, refocusAllowed, tabTexts, taskRowId, taskRows, tasksTexts } from "../subjectTabsView.js";
import { Button } from "../ui/Button.js";
import { Card } from "../ui/controls.js";
import { StatusChip } from "../ui/StatusChip.js";
import { ErrorLine } from "./FileRows.js";
import { AddAssignmentDialog } from "./HomeworkDialogs.js";

// The tab «Задания» of the subject page (TASK-353): the open assignments of the subject in the order the core sent them.
// Titles are the student's or the model's text: text nodes only (spans inside the row button, which holds phrasing only).

export function SubjectTasksTab(props: {
  subjectId: string;
  /** The overview of the page: the assignments come from it. */
  state: OverviewState;
  /** «Повторить» after a failed overview or a failed re-read. */
  onRetry: () => void;
  /** An assignment was added: the overview is read again. */
  onCreated: () => void;
  /** A row opens its assignment (navigation only); without it the rows only show. */
  onOpenAssignment?: (assignmentId: string) => void;
  api: Pick<HomeworkApi, "createAssignment">;
  library: Pick<LibraryApi, "subjects">;
}) {
  const { state } = props;
  const [adding, setAdding] = useState(false);
  const wasAdding = useRef(false);
  // The new assignment whose row takes the focus once the overview has been read again.
  const pending = useRef<{ id: string; seen: unknown } | null>(null);

  // The dialog went: the focus is back on «Добавить задание» (the unmounted dialog may not give it back).
  useEffect(() => {
    if (wasAdding.current && !adding) {
      document.getElementById(addTaskId)?.focus();
    }
    wasAdding.current = adding;
  }, [adding]);

  const overview = state.overview;
  useEffect(() => {
    const p = pending.current;
    if (p === null || overview === p.seen) {
      return;
    }
    pending.current = null;
    const at = document.activeElement;
    if (refocusAllowed(at === null ? null : { id: at.id, isBody: at === document.body }, addTaskId)) {
      (document.getElementById(taskRowId(p.id)) ?? document.getElementById(addTaskId))?.focus();
    }
  }, [overview]);

  const open = props.onOpenAssignment;
  const ready = state.status === "ready" && overview !== null;
  const rows = ready ? taskRows(overview.assignments) : [];

  return (
    <>
      {ready && rows.length === 0 && state.rereadFailed !== true && <p className="muted">{tasksTexts.empty}</p>}
      <div className="tab-acts">
        <Button id={addTaskId} variant="primary" icon="plus" onClick={() => setAdding(true)}>
          {tasksTexts.add}
        </Button>
      </div>
      {adding && (
        <AddAssignmentDialog
          api={props.api}
          library={props.library}
          initialSubjectId={props.subjectId}
          onClose={() => setAdding(false)}
          onCreated={(a: AssignmentItem) => {
            pending.current = { id: a.id, seen: overview };
            setAdding(false);
            props.onCreated();
          }}
        />
      )}
      {/* A failed re-read keeps the list shown; the new assignment may be missing from it, so the line says so. */}
      {(state.status === "failed" || state.rereadFailed === true) && (
        <ErrorLine text={tasksTexts.failed} onRefresh={props.onRetry} onRetry={props.onRetry} />
      )}
      {!ready && state.status !== "failed" && (
        <p className="muted small" role="status">
          {tabTexts.loading}
        </p>
      )}
      {rows.length > 0 && (
        <Card className="lrows" label={tasksTexts.list}>
          <div role="list">
            {rows.map((r) => {
              const body = (
                <>
                  {/* Spans only: the same body sits inside a <button> when the row opens its assignment. */}
                  <span className="d num" aria-hidden={r.day === "" ? true : undefined}>
                    {r.weekday}
                    <span>{r.day}</span>
                  </span>
                  <span className="tx">
                    <span className="t">{r.title}</span>
                  </span>
                  {r.chip === null ? (
                    <span className="small muted">{tasksTexts.noDue}</span>
                  ) : (
                    <StatusChip kind={r.chip.kind}>{r.chip.label}</StatusChip>
                  )}
                </>
              );
              return open === undefined ? (
                <div className="lrow drow" role="listitem" key={r.id}>
                  {body}
                </div>
              ) : (
                <div className="drow-item" role="listitem" key={r.id}>
                  <button type="button" id={taskRowId(r.id)} className="lrow drow drow-open" title={texts.openAssignment} onClick={() => open(r.id)}>
                    {body}
                  </button>
                </div>
              );
            })}
          </div>
        </Card>
      )}
    </>
  );
}
