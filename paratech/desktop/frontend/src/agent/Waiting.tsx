import { texts } from "../agentView.js";

/**
 * Three softly pulsing dots while a request is sent or runs. The text is static and the element is rendered once per
 * request, so the polite live region (role="status" is polite by itself; an explicit aria-live would trip the plan
 * indicator test) announces «Агент отвечает…» once and not on every update.
 */
export function Waiting() {
  return (
    <span className="ag-wait" role="status">
      <span className="ag-wdot" aria-hidden="true" />
      <span className="ag-wdot" aria-hidden="true" />
      <span className="ag-wdot" aria-hidden="true" />
      <span className="ag-vh">{texts.waiting}</span>
    </span>
  );
}
