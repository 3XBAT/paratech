import { type ReactNode, useEffect, useMemo, useReducer, useRef, useState } from "react";
import type { AgentConfirmApi } from "../bindings/agentConfirm.js";
import {
  choicesOf,
  initialScheduleFlow,
  refusalOpensSubjects,
  ScheduleCardFlow,
  type ScheduleFlowState,
  scheduleModel,
  scheduleTexts,
} from "../agentScheduleView.js";
import { useRecipientNav } from "../recipientNav.js";
import {
  initialState,
  mainButton,
  omittedLeft,
  omittedNote,
  planOf,
  reduce,
  reviewTexts,
  type ReviewState,
  type SubjectRef,
  takenOf,
} from "../scheduleSubjectsView.js";
import type { RestoreCall } from "../subjectRemovalView.js";
import { DoneRow, GroupRow, NoticeRows } from "../pages/ScheduleSubjectsReview.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";

// The schedule_subjects card (TASK-324): the check of the «Предметы» review inside the chat. Every name is schedule text:
// React text nodes and input values only. No heading: the rows speak, and the buttons say what «Да» will do.

/** What the card asks of its owner. Absent handlers leave the matching button off. */
export interface ScheduleHandlers {
  check(group: string, checked: boolean): void;
  rename(group: string, name: string): void;
  member(group: string, member: string, mode: "keep" | "separate" | "skip"): void;
  renameMember(group: string, member: string, name: string): void;
  yes(): void;
  no(): void;
  undo(): void;
  /** «Повторить» after a card that could not be read, and the fresh version after a «Вернуть» that found it changed. */
  reload(): void;
  /** «Открыть «Предметы»»; absent: the button is not drawn. */
  openSubjects?(): void;
  /** «Вернуть» of a skipped row a removed subject blocks; absent: such a row is only a line. */
  restore?(subjectId: string, version: number): Promise<RestoreCall>;
}

/** The card before it is read: a small line, or the reason it could not be read with «Повторить». */
export function ScheduleCardPending(props: { error: string; onReload?: () => void }) {
  if (props.error === "") {
    return (
      <p className="ag-sched-load small muted" role="status">
        {scheduleTexts.loading}
      </p>
    );
  }
  return (
    <section className="ag-card ag-sched" aria-label={reviewTexts.label}>
      <ResultLine kind="fail" role="alert">
        {props.error}
      </ResultLine>
      {props.onReload !== undefined && (
        <div className="ag-actions">
          <Button size="sm" variant="secondary" onClick={props.onReload}>
            {scheduleTexts.reload}
          </Button>
        </div>
      )}
    </section>
  );
}

/**
 * The card for any state of the flow (no hooks: the tests render it directly). `review` holds the rows being chosen while
 * the card waits and, afterwards, the names the student saw; `subjects` are the existing ones (their names are taken).
 */
export function ScheduleCardView(props: {
  flow: ScheduleFlowState;
  review: ReviewState;
  subjects: readonly SubjectRef[];
  on: ScheduleHandlers;
}) {
  const { flow, review, on } = props;
  const card = flow.card;
  if (card === null) {
    return <ScheduleCardPending error={flow.error} onReload={on.reload} />;
  }
  const busy = flow.busy !== "";
  const phase = flow.busy === "undo" ? "undoing" : busy ? "saving" : "idle";
  const names = new Map(props.subjects.map((s) => [s.id, s.name]));
  const model = scheduleModel(card, review, names);
  const openSubjects = on.openSubjects;
  const subjectsButton = openSubjects !== undefined && (
    <Button size="sm" variant="secondary" disabled={busy} onClick={openSubjects}>
      {scheduleTexts.openSubjects}
    </Button>
  );

  let body: ReactNode;
  switch (model.view) {
    case "waiting": {
      if (model.groups === 0) {
        body = <p className="small muted">{review.omitted > 0 ? omittedLeft(review.omitted) : scheduleTexts.noGroups}</p>;
        break;
      }
      const taken = takenOf(props.subjects, { token: review.token, batches: [], attempt: [] });
      const plan = planOf(review.groups, review.selection, taken);
      const main = mainButton(plan);
      body = (
        <>
          <p className="small muted sched-intro">{reviewTexts.intro}</p>
          <ul className="sched-rows">
            {review.groups.map((g) => {
              const row = review.selection.get(g.key);
              return row === undefined ? null : <GroupRow key={g.key} group={g} row={row} problems={plan.problems} disabled={busy} on={on} />;
            })}
          </ul>
          {review.omitted > 0 && <p className="small muted">{omittedNote(review.omitted)}</p>}
          <div className="ag-actions">
            {/* «Нет» first and nothing is focused: Enter never lands on «Да» by itself. */}
            <Button size="sm" variant="secondary" disabled={busy} onClick={on.no}>
              {scheduleTexts.no}
            </Button>
            <Button
              size="sm"
              variant="primary"
              disabled={busy || main.disabled}
              disabledReason={busy ? undefined : main.reason || undefined}
              busy={flow.busy === "confirm"}
              busyLabel={reviewTexts.creating}
              onClick={on.yes}
            >
              {main.label}
            </Button>
            {!busy && main.extra !== "" && <span className="small muted">{main.extra}</span>}
          </div>
        </>
      );
      break;
    }
    case "done":
    case "confirmed":
      body = (
        <>
          <div role="status">
            {model.view === "done" && model.undo && <DoneRow text={model.summary} phase={phase} onUndo={on.undo} />}
            {model.view === "done" && !model.undo && model.summary !== "" && <ResultLine kind="wait">{model.summary}</ResultLine>}
            {model.view === "confirmed" && model.summary !== "" && <ResultLine kind="ok">{model.summary}</ResultLine>}
            {model.view === "confirmed" && <ResultLine kind="wait">{model.message}</ResultLine>}
            {model.lines.length > 0 && (
              <ul className="sched-lines small muted">
                {model.lines.map((l, i) => (
                  <li key={i}>{l}</li>
                ))}
              </ul>
            )}
          </div>
          <NoticeRows rows={model.rows} restore={on.restore} onChanged={on.reload} />
          {model.view === "confirmed" && (
            <div className="ag-actions">
              <Button size="sm" variant="secondary" disabled={busy} busy={flow.busy === "undo"} busyLabel={reviewTexts.undoing} onClick={on.undo}>
                {reviewTexts.undoAll}
              </Button>
              <Button size="sm" variant="primary" disabled={busy} busy={flow.busy === "confirm"} busyLabel={reviewTexts.creating} onClick={on.yes}>
                {scheduleTexts.yes}
              </Button>
            </div>
          )}
          {model.view === "done" && model.rest && subjectsButton && <div className="ag-actions">{subjectsButton}</div>}
        </>
      );
      break;
    case "stale":
      body = (
        <>
          <ResultLine kind="wait" role="status">
            {model.message}
          </ResultLine>
          {subjectsButton && <div className="ag-actions">{subjectsButton}</div>}
        </>
      );
      break;
    case "undone":
      body = (
        <div role="status">
          <ResultLine kind="ok">{model.summary}</ResultLine>
          {model.lines.length > 0 && (
            <ul className="sched-lines small muted">
              {model.lines.map((l, i) => (
                <li key={i}>{l}</li>
              ))}
            </ul>
          )}
        </div>
      );
      break;
    case "declined":
      body = (
        <p className="muted" role="status">
          {model.text}
        </p>
      );
      break;
  }

  return (
    <section className="ag-card ag-sched" aria-label={reviewTexts.label} aria-busy={busy || undefined} tabIndex={-1}>
      {body}
      {/* A refusal of the core, in its own words; the buttons above stay as they were, so the way on is a click away. */}
      {flow.error !== "" && (
        <>
          <ResultLine kind="fail" role="alert">
            {flow.error}
          </ResultLine>
          {refusalOpensSubjects(flow.error) && subjectsButton && <div className="ag-actions">{subjectsButton}</div>}
        </>
      )}
    </section>
  );
}

/** Owns the flow of one card: reads it, keeps the choices of the rows, maps the buttons to the core. */
export function ScheduleSubjectsCard(props: {
  api: AgentConfirmApi;
  /** The token of the package the desktop built (the target id of the card). */
  token: string;
  /** Subject names by id; their names are taken for the rows, and the lines of the outcome call subjects by them. */
  subjects?: Readonly<Record<string, string>>;
  /** «Вернуть» of a skipped row, as the page does it. */
  restore?: (subjectId: string, version: number) => Promise<RestoreCall>;
  /** Subjects were created, taken back or restored: the pages read them again. */
  onChanged?: () => void;
}) {
  const nav = useRecipientNav();
  const [state, setState] = useState<ScheduleFlowState>(initialScheduleFlow);
  const flow = useMemo(() => new ScheduleCardFlow(props.api, props.token, setState), [props.api, props.token]);
  const [review, dispatch] = useReducer(reduce, null, () => initialState({ token: "", groups: [], omitted: 0, problem: "" }));
  const { onChanged, restore: restoreRow } = props;

  useEffect(() => {
    flow.attach();
    void flow.load();
    return () => flow.dispose();
  }, [flow]);

  // The rows are seeded from the waiting card; a card read again keeps what the student edited for the same rows.
  const card = state.card;
  useEffect(() => {
    if (card !== null && card.state === "waiting" && card.schedule !== null) {
      dispatch({ type: "refreshed", reply: { token: card.targetId, groups: card.schedule.groups, omitted: card.schedule.omitted, problem: "" } });
    }
  }, [card]);

  // The button that was pressed is gone when the card changes state: the focus goes to the card, not to the page body.
  const box = useRef<HTMLDivElement>(null);
  const lastState = useRef(card?.state);
  const cardState = card?.state;
  useEffect(() => {
    const was = lastState.current;
    lastState.current = cardState;
    if (was !== undefined && cardState !== undefined && was !== cardState) {
      const at = document.activeElement;
      if (at === null || at === document.body) {
        box.current?.querySelector<HTMLElement>("section")?.focus();
      }
    }
  }, [cardState]);

  const subjects = useMemo<SubjectRef[]>(() => Object.entries(props.subjects ?? {}).map(([id, name]) => ({ id, name, aliases: [] })), [props.subjects]);

  const seeded = card === null || card.state !== "waiting" || review.token === card.targetId;
  const shown: ScheduleFlowState = seeded ? state : { ...state, card: null };
  const restore = useMemo(
    () =>
      restoreRow === undefined
        ? undefined
        : async (subjectId: string, version: number) => {
            const call = await restoreRow(subjectId, version);
            if (call.state.phase === "done") {
              onChanged?.();
            }
            return call;
          },
    [restoreRow, onChanged],
  );

  return (
    <div ref={box}>
    <ScheduleCardView
      flow={shown}
      review={review}
      subjects={subjects}
      on={{
        check: (group, checked) => dispatch({ type: "check", group, checked }),
        rename: (group, name) => dispatch({ type: "rename", group, name }),
        member: (group, member, mode) => dispatch({ type: "member", group, member, mode }),
        renameMember: (group, member, name) => dispatch({ type: "renameMember", group, member, name }),
        yes: () => void flow.yes(choicesOf(review)).then(() => onChanged?.()),
        no: () => void flow.no(),
        undo: () => void flow.undo().then(() => onChanged?.()),
        reload: () => void flow.load(),
        openSubjects: nav.openSubjects,
        restore,
      }}
    />
    </div>
  );
}
