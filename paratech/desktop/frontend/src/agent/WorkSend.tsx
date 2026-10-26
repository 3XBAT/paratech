import { useCallback, useEffect, useMemo, useState } from "react";
// The .js extensions let the Node test runner load the card.
import { type AgentConfirmApi, type ConfirmCard as CardData, noAssignmentText } from "../bindings/agentConfirm.js";
import type { RecordingDeleteApi } from "../bindings/recordingDelete.js";
import { initialSendWork, isFinished, SendWorkFlow, type SendWorkState, sendWorkTexts } from "../sendWorkView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { ConfirmCard } from "./ConfirmCard.js";

/** The static part (no hooks, rendered by the tests): the button, the hint, the warning and the error as text nodes. */
export function WorkSendView(props: { state: SendWorkState; sendable: boolean; onSend: () => void }) {
  const { state } = props;
  if (!props.sendable) {
    return <p className="small muted">{noAssignmentText}</p>;
  }
  const open = state.card === null || isFinished(state.card);
  return (
    <div className="ag-send">
      {state.sentBefore && (
        <p className="ag-note small" role="status">
          {sendWorkTexts.alreadySent}
        </p>
      )}
      {state.error !== "" && (
        <ResultLine kind="fail" role="alert">
          {state.error}
        </ResultLine>
      )}
      {open && (
        <>
          <div className="ag-actions">
            <Button size="sm" variant="secondary" disabled={state.busy} busy={state.busy} busyLabel={sendWorkTexts.busy} onClick={props.onSend}>
              {state.sentBefore ? sendWorkTexts.again : sendWorkTexts.button}
            </Button>
          </div>
          <p className="small muted">{sendWorkTexts.hint}</p>
        </>
      )}
    </div>
  );
}

/**
 * «Отправить преподавателю» of a ready work: a click makes the proposal, the confirm card then asks «Да» / «Нет».
 * Nothing goes out before that «Да».
 */
export function WorkSend(props: { api: AgentConfirmApi; deleteApi: RecordingDeleteApi; workId: string; sendable: boolean }) {
  const { api, deleteApi, workId } = props;
  const [state, setState] = useState<SendWorkState>(initialSendWork);
  const flow = useMemo(() => new SendWorkFlow(api, workId, setState), [api, workId]);
  useEffect(() => {
    flow.attach();
    return () => flow.dispose();
  }, [flow]);
  const onCard = useCallback((c: CardData) => flow.noteCard(c), [flow]);

  return (
    <>
      <WorkSendView state={state} sendable={props.sendable} onSend={() => void flow.propose()} />
      {state.card !== null && <ConfirmCard key={state.card.targetId} api={api} deleteApi={deleteApi} initial={state.card} onCard={onCard} />}
    </>
  );
}
