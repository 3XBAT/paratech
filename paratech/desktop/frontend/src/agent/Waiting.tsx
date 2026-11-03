import { texts } from "../agentView.js";

/**
 * Three softly pulsing dots while a request is sent or runs. The text is static and the element is rendered once per
 * request, so the polite live region (role="status" is polite by itself; an explicit aria-live would trip the plan
 * indicator test) announces «Агент отвечает…» once and not on every update.
 * A queued request mounts the same element in the same place with its own label and static muted dots (nothing is
 * answering yet), so the region announces the change when it starts running.
 */
export function Waiting(props: { label?: string; queued?: boolean }) {
  return (
    <span className={props.queued === true ? "ag-wait queued" : "ag-wait"} role="status">
      <span className="ag-wdot" aria-hidden="true" />
      <span className="ag-wdot" aria-hidden="true" />
      <span className="ag-wdot" aria-hidden="true" />
      <span className="ag-vh">{props.label ?? texts.waiting}</span>
    </span>
  );
}
