import { useId, useState } from "react";
import type { SecretStatus } from "../bindings/connections";
import { secretControls, secretStatusLabel, type SecretLabels } from "../connectionsView";

export interface SecretMessage {
  text: string;
  ok: boolean;
}

// A write-only secret: the page shows only its status. The typed value lives in this component's state until the
// call returns and goes nowhere else (no other state, no log, no error text). Reused for the Hub key (TASK-039).
export function SecretSlot(props: {
  labels: SecretLabels;
  status: SecretStatus;
  busy: boolean;
  message: SecretMessage | null;
  onStore: (value: string) => Promise<void>;
  onRemove: () => Promise<void>;
}) {
  const id = useId();
  const [value, setValue] = useState("");
  const [confirming, setConfirming] = useState(false);
  const { labels, status } = props;
  const c = secretControls(status, props.busy, value !== "");

  async function store() {
    try {
      await props.onStore(value);
    } finally {
      setValue("");
    }
  }

  async function remove() {
    setConfirming(false);
    await props.onRemove();
  }

  return (
    <div className="block">
      <h2>{labels.title}</h2>
      <p className={status === "unsupported" || status === "unavailable" ? "warning" : ""}>
        {secretStatusLabel(labels, status)}
      </p>
      <div className="field">
        <label htmlFor={id}>{labels.input}</label>
        <input
          id={id}
          type="password"
          autoComplete="new-password"
          spellCheck={false}
          value={value}
          disabled={c.inputDisabled}
          onChange={(e) => setValue(e.target.value)}
        />
        {c.removeShown && <span className="muted">{labels.replaceHint}</span>}
      </div>
      <div className="actions">
        <button type="button" disabled={c.saveDisabled} onClick={() => void store()}>
          {labels.save}
        </button>
        {c.removeShown && !confirming && (
          <button type="button" disabled={c.removeDisabled} onClick={() => setConfirming(true)}>
            {labels.remove}
          </button>
        )}
      </div>
      {c.removeShown && confirming && (
        <div className="confirm">
          <p>{labels.confirmRemove}</p>
          <div className="actions">
            <button type="button" disabled={c.removeDisabled} onClick={() => void remove()}>
              {labels.confirmYes}
            </button>
            <button type="button" onClick={() => setConfirming(false)}>
              {labels.confirmNo}
            </button>
          </div>
        </div>
      )}
      {props.message !== null && (
        <p className={props.message.ok ? "ok" : "error"} role={props.message.ok ? "status" : "alert"}>
          {props.message.text}
        </p>
      )}
    </div>
  );
}
