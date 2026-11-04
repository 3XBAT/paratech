import type { ContextFill } from "../bindings/agent.js";
import { ringModel, ringTexts } from "../agentView.js";
import { Button } from "../ui/Button.js";

const radius = 8;
/** The arc starts at the top: three quarters of a turn from the circle's own start at three o'clock. */
const startTurn = 270;

/**
 * How much of the conversation the agent still holds in context, as a small ring by the send button. Nothing is drawn
 * while the fill is unknown or the chat is empty (0 %). The text (tooltip and accessible name) carries the percent, so colour is never the only
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

/**
 * While the agent compacts the open chat: one muted text line under the field, no box and no animation (the thread's
 * Waiting dots already show the request; TASK-270). The live region is always rendered and empty when idle (zero
 * height), so a reader picks up the text when it appears. In normal flow, never over the thread. The field stays usable.
 */
export function CompactionNote(props: { active: boolean }) {
  return (
    <div className="ag-compact small" role="status" aria-live="polite">
      {props.active && ringTexts.compacting}
    </div>
  );
}

/** From «full»: one quiet line under the field with the way out. Nothing at the other levels. */
export function ContextHint(props: { fill: ContextFill | null; onNew?: () => void }) {
  const ring = ringModel(props.fill);
  if (ring === null || !ring.hint || props.onNew === undefined) {
    return null;
  }
  return (
    <div className="ag-fullhint small" role="status">
      <span className="ag-fullhint-tx">{ring.hintText}</span>
      <Button size="sm" variant="secondary" onClick={props.onNew}>
        {ringTexts.hintButton}
      </Button>
    </div>
  );
}
