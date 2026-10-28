// «Выполнено» in the screens (TASK-316): the round check, the button and the date line of the assignment page, the
// refusal line with its button, and the row of the list of assignments. They hold no data or decisions: the pages pass
// what the view model (assignmentDoneView.ts) says. Titles and subjects are student or model text: text nodes only.
import type { ReactNode } from "react";
import type { AssignmentItem } from "../bindings/homework.js";
import {
  checkLabel,
  checkTitle,
  type DoneIssue,
  doneStamp,
  doneTexts,
  issueButton,
  pageDoneLabel,
  type RemoveLock,
  removeItem,
} from "../assignmentDoneView.js";
import { changeTexts, dueText } from "../homeworkView.js";
import { lessonText } from "../lessonLabel.js";
import { Button } from "../ui/Button.js";
import { ResultLine, Row } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { RowMenu } from "../ui/RowMenu.js";

/**
 * The round check of a row (Todoist, Google Tasks): an empty ring is open, a filled check is done. Its name is the
 * action, not the state. It stays focusable while a call runs (aria-busy), so the focus is not lost; the page's guard
 * keeps a second click from starting a second call.
 */
export function DoneCheck(props: {
  title: string;
  done: boolean;
  busy?: boolean;
  /** The assignment of the row: the page finds the check by it to keep the focus when the row leaves. */
  rowId?: string;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      className={props.done ? "done-check on" : "done-check"}
      aria-label={checkLabel(props.title, props.done)}
      title={checkTitle(props.done)}
      aria-busy={props.busy || undefined}
      data-done-id={props.rowId}
      onClick={props.onToggle}
    >
      <Icon name="ok" />
    </button>
  );
}

/**
 * «Отметить выполненным» next to «Изменить» and «Удалить» on the assignment page; «Вернуть в работу» when it is done
 * (no icon: the only one that fits, "retry", reads as "try again").
 */
export function DoneButton(props: { done: boolean; busy: boolean; disabled?: boolean; onToggle: () => void }) {
  return (
    <Button
      icon={props.done ? undefined : "ok"}
      busy={props.busy}
      busyLabel={doneTexts.saving}
      aria-busy={props.busy || undefined}
      disabled={props.disabled}
      onClick={props.onToggle}
    >
      {pageDoneLabel(props.done)}
    </Button>
  );
}

/** The muted line «Выполнено 28 окт.» under the head of a done assignment. */
export function DoneStamp(props: { assignment: Pick<AssignmentItem, "done" | "doneAt"> }) {
  const text = doneStamp(props.assignment);
  return text === "" ? null : <p className="small muted hw-donedate">{text}</p>;
}

/** A refusal of mark, un-mark or undo: why, in plain Russian, and the button that leads out. */
export function DoneIssueLine(props: {
  issue: DoneIssue;
  where: "list" | "page";
  onRetry: () => void;
  onOpen: (id: string) => void;
  onBack?: () => void;
  /** «Закрыть» for a line that stays above the list after its row is gone. */
  onClose?: () => void;
}) {
  const button = issueButton(props.issue, props.where);
  const click = () => {
    if (button?.kind === "retry") {
      props.onRetry();
    } else if (button?.kind === "open") {
      props.onOpen(props.issue.openId);
    } else {
      props.onBack?.();
    }
  };
  return (
    <div className="hw-issue">
      <ResultLine kind="fail" role="alert">
        {props.where === "page" ? props.issue.pageText : props.issue.text}
      </ResultLine>
      {button !== null && (button.kind !== "back" || props.onBack !== undefined) && (
        <Button size="sm" onClick={click}>
          {button.label}
        </Button>
      )}
      {props.onClose !== undefined && (
        <Button size="sm" variant="ghost" onClick={props.onClose}>
          {doneTexts.close}
        </Button>
      )}
    </div>
  );
}

/**
 * A row of the list of assignments. The quiet action is the round check in the lead slot; «Открыть» and the «⋯»
 * menu with «Удалить» (the red button moved there to keep the row calm) are on the right. busyText is the small
 * inline text while a call of the row runs; a done row is muted and struck through and says when in its meta.
 */
export function AssignmentRowView(props: {
  assignment: AssignmentItem;
  /** The email chip of the row. */
  status?: ReactNode;
  /** "" when idle, else «Сохранение…» or «Удаление…». */
  busyText: string;
  /** The line under the row: a refusal. */
  below?: ReactNode;
  focusOpen?: boolean;
  /** The delete check runs or its question is open: «Удалить» of the menu is off. */
  removeLock?: RemoveLock;
  onOpen: () => void;
  onToggleDone: () => void;
  onRemove: () => void;
}) {
  const a = props.assignment;
  const lesson = lessonText(a);
  const stamp = doneStamp(a);
  const busy = props.busyText !== "";
  return (
    <Row
      lead={<DoneCheck title={a.title} done={a.done} busy={busy} rowId={a.id} onToggle={props.onToggleDone} />}
      title={a.done ? <span className="hw-done-title">{a.title}</span> : a.title}
      meta={
        <>
          {lesson !== "" && <span className="hw-lesson">{lesson}</span>}
          <span className="num">{dueText(a.due)}</span>
          {stamp !== "" && <span className="num hw-donedate">{` · ${stamp}`}</span>}
        </>
      }
      status={props.status}
      actions={
        <>
          {busy && (
            <span className="small muted hw-inline" role="status">
              {props.busyText}
            </span>
          )}
          <Button autoFocus={props.focusOpen} onClick={props.onOpen}>
            Открыть
          </Button>
          <RowMenu
            rowId={a.id}
            label={`${doneTexts.moreActions}: ${a.title}`}
            title={doneTexts.moreActions}
            items={[{ key: "remove", label: changeTexts.remove, ...removeItem(busy, props.removeLock ?? "") }]}
            onPick={props.onRemove}
          />
        </>
      }
      below={props.below}
    />
  );
}
