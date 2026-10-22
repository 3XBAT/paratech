import { useEffect, useMemo, useState } from "react";
import type { AgentConfirmApi } from "../bindings/agentConfirm.js";
import type { RecordingDeleteApi } from "../bindings/recordingDelete.js";
import { calendarTexts, initialPropose, ProposeFlow, type ProposeState } from "../agentConfirmView.js";
import { ConfirmCard } from "../agent/ConfirmCard.js";
import { dueText } from "../todayView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";

/** The text of the button's due date: «до сб, 24 окт.», "" when unknown. */
export function calendarDue(due: string): string {
  return dueText(due);
}

/** The button of the markup: only a click makes the proposal (no calendar write yet). */
export function CalendarButton(props: { due: string; busy: boolean; error: string; onClick: () => void }) {
  const due = calendarDue(props.due);
  return (
    <div className="today-cal">
      <Button size="sm" disabled={props.busy} busy={props.busy} busyLabel={calendarTexts.busy} onClick={props.onClick}>
        {calendarTexts.button}
      </Button>
      {due !== "" && <span className="small muted">{due}</span>}
      {props.error !== "" && (
        <ResultLine kind="fail" role="alert">
          {props.error}
        </ResultLine>
      )}
    </div>
  );
}

/**
 * «Да, в календарь» of a post: proposes the stored deadline of the assignment, then shows the confirm card with
 * «Да» / «Нет». The event is made only by that «Да».
 */
export function PostCalendar(props: { api: AgentConfirmApi; deleteApi: RecordingDeleteApi; assignmentId: string; due: string }) {
  const [state, setState] = useState<ProposeState>(initialPropose);
  const flow = useMemo(() => new ProposeFlow(props.api, props.assignmentId, setState), [props.api, props.assignmentId]);
  useEffect(() => {
    flow.attach();
    return () => flow.dispose();
  }, [flow]);
  if (state.card !== null) {
    return <ConfirmCard api={props.api} deleteApi={props.deleteApi} initial={state.card} />;
  }
  return <CalendarButton due={props.due} busy={state.busy} error={state.error} onClick={() => void flow.propose()} />;
}
