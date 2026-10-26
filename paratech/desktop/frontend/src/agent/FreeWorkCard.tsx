import { useEffect, useMemo, useState } from "react";
// The .js extensions let the Node test runner load the compiled card.
import type { AgentApi, FreeWorkRef } from "../bindings/agent.js";
import type { TurnkeyApi } from "../bindings/turnkey.js";
import { FreeWorkFlow, type FreeWorkFlowState, freeWorkModel, freeWorkTexts, initialFreeWorkFlow } from "../freeWorkView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { WorkWritingCard } from "./WorkCard.js";

// Every text is a React text node; the card holds no topic.

/** The card for a state of the flow (no hooks: the tests render it directly). */
export function FreeWorkCardView(props: {
  state: FreeWorkFlowState;
  turnkey?: TurnkeyApi;
  onDontShow: (on: boolean) => void;
  onYes: () => void;
  onNo: () => void;
  onReload: () => void;
}) {
  const { state } = props;
  const m = freeWorkModel(state);
  if (m.kind === "work") {
    return props.turnkey ? <WorkWritingCard key={state.workId} api={props.turnkey} workId={state.workId} /> : null;
  }
  return (
    <section className="ag-card ag-fw">
      <h3 className="ag-card-h">{freeWorkTexts.caption}</h3>
      {m.kind === "loading" && <p className="muted">{m.line}</p>}
      {m.kind === "line" && (
        <p className="ag-fw-out" role="status">
          {m.line}
        </p>
      )}
      {m.kind === "ask" && (
        <>
          <p>{freeWorkTexts.text}</p>
          <label className="ag-fw-chk small">
            <input type="checkbox" checked={state.dontShow} disabled={m.buttonsOff} onChange={(e) => props.onDontShow(e.target.checked)} />
            <span>{freeWorkTexts.dontShow}</span>
          </label>
        </>
      )}
      {state.error !== "" && (
        <ResultLine kind="fail" role="alert">
          {state.error}
        </ResultLine>
      )}
      {m.kind === "ask" && (
        <div className="ag-actions">
          <Button size="sm" variant="primary" disabled={m.buttonsOff} busy={state.busy} busyLabel={freeWorkTexts.working} onClick={props.onYes}>
            {freeWorkTexts.yes}
          </Button>
          <Button size="sm" variant="secondary" disabled={m.buttonsOff} onClick={props.onNo}>
            {freeWorkTexts.no}
          </Button>
        </div>
      )}
      {m.kind === "unknown" && (
        <div className="ag-actions">
          <Button size="sm" onClick={props.onReload}>
            {freeWorkTexts.reload}
          </Button>
        </div>
      )}
    </section>
  );
}

/** Reads the card from the core (an old thread draws its outcome) and maps the buttons to the core. */
export function FreeWorkCard(props: { api: Pick<AgentApi, "freeWork" | "freeWorkCard">; fwRef: FreeWorkRef; turnkey?: TurnkeyApi }) {
  const { api, fwRef } = props;
  const [state, setState] = useState<FreeWorkFlowState>(initialFreeWorkFlow);
  const { threadId, turn, callId } = fwRef;
  const flow = useMemo(() => new FreeWorkFlow(api, { threadId, turn, callId }, setState), [api, threadId, turn, callId]);

  useEffect(() => {
    flow.attach();
    void flow.load();
    return () => flow.dispose();
  }, [flow]);

  return (
    <FreeWorkCardView
      state={state}
      turnkey={props.turnkey}
      onDontShow={(on) => flow.setDontShow(on)}
      onYes={() => void flow.answer(true)}
      onNo={() => void flow.answer(false)}
      onReload={() => void flow.load()}
    />
  );
}
