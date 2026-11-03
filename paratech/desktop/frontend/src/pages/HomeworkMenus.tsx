import { type KeyboardEvent, type ReactNode, useEffect, useId, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled menus (test/homeworkJournal346.test.mjs renders them).
import { doneTexts, type RemoveLock, removeItem } from "../assignmentDoneView.js";
import { formatTime } from "../activityView.js";
import type { AssignmentItem, DeliveryItem } from "../bindings/homework.js";
import { CollapsibleList } from "../CollapsibleList.js";
import { type AssignmentGroup, changeTexts, deliveryView, draftButtonTexts, firstRunTexts } from "../homeworkView.js";
import { type ListState, type ListStates, stateOf } from "../listView.js";
import { Button } from "../ui/Button.js";
import { EmptyState, Row } from "../ui/controls.js";
import { menuKey, menuShown } from "../ui/menu.js";
import { RowMenu, type RowMenuItem } from "../ui/RowMenu.js";
import { StatusChip } from "../ui/StatusChip.js";

// Pieces of «Задания» (TASK-346) kept out of the long page: the groups of the list, the first run, the email row, the
// «⋯» menu in the head (everything but «Выполнено») and «Сохранить ▾» under the text. Pure view: the page decides what
// each pick does.

/**
 * The groups «Просрочено · На этой неделе · Позже · Без срока · Выполнено», each with its heading, its card and its own
 * «Показать ещё N» window (the state of every group is kept by the page under the group's key), so a long group never
 * pushes a later one out of sight. The count in a heading is the whole group. The «Выполненные» list is flat: one card,
 * no heading.
 */
export function GroupedRows(props: {
  groups: readonly AssignmentGroup[];
  states: ListStates;
  onStateChange: (key: string, next: ListState) => void;
  renderItem: (a: AssignmentItem) => ReactNode;
}) {
  return (
    <>
      {props.groups.map((g) => (
        <section key={g.key} className="hw-sec" aria-labelledby={g.title === "" ? undefined : `hw-h-${g.key}`}>
          {g.title !== "" && (
            <h2 className="hw-group" id={`hw-h-${g.key}`}>
              {g.title}
              <span className="cnt num">{g.items.length}</span>
            </h2>
          )}
          <CollapsibleList
            items={g.items}
            itemKey={(a) => a.id}
            renderItem={props.renderItem}
            renderContainer={(rows) => (
              <div className="card rows" role="list">
                {rows}
              </div>
            )}
            state={stateOf(props.states, g.key)}
            onStateChange={(next) => props.onStateChange(g.key, next)}
            classNames={{ button: "btn ghost" }}
          />
        </section>
      ))}
    </>
  );
}

/**
 * The first run of «Задания» (no assignment at all): where they come from and the way to add one by hand. The head of
 * the page already has the one accent button, so this one is a plain button.
 */
export function HomeworkFirstRun(props: { onAdd: () => void }) {
  return (
    <EmptyState
      label="Заданий пока нет"
      title="Здесь появятся задания"
      actions={<Button onClick={props.onAdd}>{firstRunTexts.manual}</Button>}
    >
      {firstRunTexts.text}
    </EmptyState>
  );
}

/**
 * A row of the emails of an assignment. «Письмо дошло» and «Отправить ещё раз…» come from the core's canMarkSent and
 * canSendAgain; both are ordinary buttons (the duplicate-send warning is the dialog behind «Отправить ещё раз…»), and
 * «Письмо дошло» has the line that says when to press it.
 */
export function DeliveryRow(props: {
  delivery: DeliveryItem;
  /** «Письмо дошло» of this row is being saved. */
  marking: boolean;
  /** No other call of the page runs. */
  idle: boolean;
  onMarkSent: () => void;
  onSendAgain: () => void;
}) {
  const d = props.delivery;
  const v = deliveryView(d);
  const noteId = `mark-note-${d.id}`;
  return (
    <Row
      title={<bdi>{d.recipient}</bdi>}
      meta={<span className="num">Создано {formatTime(d.createdAt)}</span>}
      status={
        <>
          <StatusChip kind={v.kind} icon={v.icon}>
            {v.label}
          </StatusChip>
          {/* With «Письмо дошло» the line under it says everything; the core's hint would repeat it. */}
          {v.hint !== "" && !d.canMarkSent && <span className="small muted">{v.hint}</span>}
          {v.problem !== "" && <span className="small row-problem">{v.problem}</span>}
          {d.canMarkSent && (
            <span id={noteId} className="small muted">
              {draftButtonTexts.markSentNote}
            </span>
          )}
        </>
      }
      actions={
        <>
          {d.canMarkSent && (
            <Button
              aria-describedby={noteId}
              busy={props.marking}
              busyLabel="Сохранение…"
              disabled={!props.idle}
              onClick={props.onMarkSent}
            >
              {draftButtonTexts.markSent}
            </Button>
          )}
          {d.canSendAgain && (
            <Button disabled={!props.idle} onClick={props.onSendAgain}>
              {draftButtonTexts.sendAgain}
            </Button>
          )}
        </>
      }
    />
  );
}

export type HeadKey = "edit" | "correct" | "remove";

export const headMenuTexts = {
  /** Under the disabled items while the delete call runs. */
  deleting: "Идёт удаление",
  /** Under «Поправить предмет и преподавателя» while its form is open. */
  formOpen: "Уже открыто",
} as const;

/** What the page is doing, for the items of the head's «⋯» menu. */
export interface HeadMenuState {
  /** The delete call runs. */
  changing: boolean;
  /** The done mark is being saved. */
  marking: boolean;
  /** The running-work check of «Удалить» is on its way. */
  checking: boolean;
  /** Its question is open. */
  confirming: boolean;
  /** The form of «Поправить предмет и преподавателя» is open. */
  correcting: boolean;
}

/**
 * The items of the head's «⋯» menu, in this order: «Изменить», «Поправить предмет и преподавателя», «Удалить» (last, the
 * only one that removes). A disabled item says why: the delete runs, the mark is saved, the check of «Удалить» runs, its
 * question waits (only «Удалить»), the correction form is already open (only that item).
 */
export function headMenuItems(s: HeadMenuState): RowMenuItem<HeadKey>[] {
  const lock: RemoveLock = s.checking ? "checking" : s.confirming ? "confirm" : "";
  const why = s.changing ? headMenuTexts.deleting : s.marking ? doneTexts.busyHint : s.checking ? doneTexts.removeChecking : "";
  const correctWhy = why !== "" ? why : s.correcting ? headMenuTexts.formOpen : "";
  return [
    { key: "edit", label: changeTexts.edit, disabled: why !== "", hint: why },
    { key: "correct", label: draftButtonTexts.correct, disabled: correctWhy !== "", hint: correctWhy },
    {
      key: "remove",
      label: changeTexts.remove,
      ...(s.changing ? { disabled: true, hint: headMenuTexts.deleting } : removeItem(s.marking, lock)),
    },
  ];
}

/** «⋯» of the assignment page; its button carries the id, so the page can give it the focus back (focusRowMenu). */
export function AssignmentHeadMenu(
  props: HeadMenuState & {
    id: string;
    title: string;
    onPick: (key: HeadKey) => void;
  },
) {
  return (
    <RowMenu
      rowId={props.id}
      label={`${doneTexts.moreActions}: ${props.title}`}
      title={doneTexts.moreActions}
      items={headMenuItems(props)}
      onPick={props.onPick}
    />
  );
}

/**
 * «Сохранить ▾»: one button for the two ways to save the shown version, «Файл .txt» and «Word (.docx)». The keys are those
 * of the «⋯» menu: Enter, Space or ArrowDown opens it, arrows move, Escape closes it and focuses the button, Tab or a
 * click outside closes it. While a save runs the button says «Сохранение…»; disabled, it says why next to it.
 * initialOpen only sets the first render (the markup of the open menu is tested).
 */
export function SaveMenu(props: {
  /** A save runs. */
  busy: boolean;
  disabled: boolean;
  reason?: string;
  onFile: () => void;
  onWord: () => void;
  initialOpen?: boolean;
}) {
  const [open, setOpen] = useState(props.initialOpen === true);
  const wrap = useRef<HTMLDivElement>(null);
  const picked = useRef(false);
  const menuId = useId();
  const shown = menuShown(open, 2);

  const trigger = () => wrap.current?.querySelector<HTMLButtonElement>("[aria-haspopup]") ?? null;
  const enabled = () =>
    Array.from(wrap.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? []);

  useEffect(() => {
    if (props.disabled) {
      setOpen(false);
    }
  }, [props.disabled]);

  useEffect(() => {
    if (!shown) {
      return;
    }
    enabled()[0]?.focus();
    const outside = (e: PointerEvent) => {
      if (wrap.current !== null && !wrap.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [shown]);

  // The button is off while the save runs: the focus it had must not be lost when the save is over.
  useEffect(() => {
    if (!props.busy && picked.current) {
      picked.current = false;
      const active = document.activeElement;
      if (active === null || active === document.body || active === trigger()) {
        trigger()?.focus();
      }
    }
  }, [props.busy]);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!open) {
      if (e.key === "ArrowDown" && e.target === trigger() && !props.disabled) {
        e.preventDefault();
        setOpen(true);
      }
      return;
    }
    const list = enabled();
    const next = menuKey(e.key, list.indexOf(document.activeElement as HTMLButtonElement), list.length);
    if (next === null) {
      return;
    }
    if (next === "leave") {
      setOpen(false); // the focus moves on by itself
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    if (next === "close") {
      setOpen(false);
      trigger()?.focus();
      return;
    }
    list[next]?.focus();
  };

  const pick = (run: () => void) => () => {
    picked.current = true;
    setOpen(false);
    trigger()?.focus();
    run();
  };

  return (
    <div className="mmenu mmenu-save" ref={wrap} onKeyDown={onKeyDown}>
      <Button
        busy={props.busy}
        busyLabel="Сохранение…"
        disabled={props.disabled}
        disabledReason={props.reason}
        aria-haspopup="menu"
        aria-expanded={shown}
        aria-controls={shown ? menuId : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        {draftButtonTexts.save}
        <span aria-hidden="true"> ▾</span>
      </Button>
      {shown && (
        <div className="mmenu-list mmenu-left" id={menuId} role="menu" aria-label={draftButtonTexts.save}>
          <button type="button" role="menuitem" className="mmenu-item" tabIndex={-1} onClick={pick(props.onFile)}>
            {draftButtonTexts.file}
          </button>
          <button type="button" role="menuitem" className="mmenu-item" tabIndex={-1} onClick={pick(props.onWord)}>
            {draftButtonTexts.word}
          </button>
        </div>
      )}
    </div>
  );
}
