import { useEffect, useState } from "react";
// The .js extensions let the Node test runner load the block.
import type { TurnkeyApi } from "../bindings/turnkey.js";
import { WorkWritingCard } from "../agent/WorkCard.js";
import { loadTurnkeyBlock, startTurnkeyOnce, type TurnkeyBlockState, turnkeyTexts } from "../homeworkTurnkey.js";
import { Button } from "../ui/Button.js";
import { Card, ResultLine } from "../ui/controls.js";
import { singleFlight } from "../workView.js";

/** The block as drawn for a state; no hooks, so the tests render it for each state. */
export function TurnkeyBlockView(props: { api: TurnkeyApi; state: TurnkeyBlockState; busy: boolean; onStart: () => void }) {
  const { state } = props;
  return (
    <Card className="box">
      <h2>{turnkeyTexts.title}</h2>
      {state.phase === "loading" && <p className="muted">{turnkeyTexts.loading}</p>}
      {state.phase === "none" && (
        <>
          <p className="small muted box-desc">{turnkeyTexts.caption}</p>
          <div className="acts start">
            <Button variant="secondary" busy={props.busy} busyLabel={turnkeyTexts.busy} disabled={props.busy} onClick={props.onStart}>
              {turnkeyTexts.button}
            </Button>
          </div>
          {state.error !== "" && <ResultLine kind="fail" role="alert">{state.error}</ResultLine>}
        </>
      )}
      {state.phase === "work" && <WorkWritingCard key={state.workId} api={props.api} workId={state.workId} initial={state.work} />}
    </Card>
  );
}

/** «Сделать под ключ»: the work card of the assignment, or the button that starts the same flow as the agent's. */
export function HomeworkTurnkey(props: { api: TurnkeyApi; assignmentId: string }) {
  const { api, assignmentId } = props;
  const [state, setState] = useState<TurnkeyBlockState>({ phase: "loading" });
  const [busy, setBusy] = useState(false);
  // The guard answers a second click before the state has disabled the button.
  const [flight] = useState(singleFlight);

  useEffect(() => {
    let live = true;
    setState({ phase: "loading" });
    void loadTurnkeyBlock(api, assignmentId).then((s) => {
      if (live) {
        setState(s);
      }
    });
    return () => {
      live = false;
    };
  }, [api, assignmentId]);

  const start = () => {
    setBusy(true);
    void startTurnkeyOnce(flight, api, assignmentId).then((s) => {
      if (s === null) {
        return; // a call is already running; its answer sets the state
      }
      setBusy(false);
      setState(s);
    });
  };

  return <TurnkeyBlockView api={api} state={state} busy={busy} onStart={start} />;
}
