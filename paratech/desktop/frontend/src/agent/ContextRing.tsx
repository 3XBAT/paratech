import type { ContextFill } from "../bindings/agent.js";
import { ringModel, ringTexts } from "../agentView.js";
import { Button } from "../ui/Button.js";

const radius = 8;
/** The arc starts at the top: three quarters of a turn from the circle's own start at three o'clock. */
const startTurn = 270;

/**
 * How much of the conversation the agent still holds in context, as a small ring by the send button. Nothing is drawn
 * while the fill is unknown. The text (tooltip and accessible name) carries the percent, so colour is never the only
 * signal. The visually hidden copy sits inside the role="img" element: its children are presentational, so a reader
 * says the text once (the name), while the words stay in the page text.
 */
export function ContextRing(props: { fill: ContextFill | null }) {
  const ring = ringModel(props.fill);
  if (ring === null) {
    return null;
  }
  return (
    <span className={`ag-ring ag-ring-${ring.level}`} role="img" aria-label={ring.text} title={ring.text}>
      <svg viewBox="0 0 24 24" width="20" height="20" fill="none" strokeWidth="3" aria-hidden="true" focusable="false">
        <circle className="ag-ring-track" cx="12" cy="12" r={radius} pathLength={100} />
        <circle className="ag-ring-arc" cx="12" cy="12" r={radius} pathLength={100} strokeDasharray={`${ring.percent} 100`} transform={`rotate(${startTurn} 12 12)`} />
      </svg>
      <span className="vh">{ring.text}</span>
    </span>
  );
}

/** From «full»: one quiet line under the field with the way out. Nothing at the other levels. */
export function ContextHint(props: { fill: ContextFill | null; onNew?: () => void }) {
  const ring = ringModel(props.fill);
  if (ring === null || !ring.hint || props.onNew === undefined) {
    return null;
  }
  return (
    <div className="ag-fullhint small">
      <span className="ag-fullhint-tx">{ringTexts.hint}</span>
      <Button size="sm" variant="secondary" onClick={props.onNew}>
        {ringTexts.hintButton}
      </Button>
    </div>
  );
}
