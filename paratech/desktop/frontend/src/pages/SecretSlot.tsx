import { useState } from "react";
import type { SecretStatus } from "../bindings/connections";
import { secretChipLabel, secretControls, secretStatusNote, secretStoreCaption, type SecretLabels } from "../connectionsView";
import { Button } from "../ui/Button";
import { Dialog, Field, Input, ResultLine } from "../ui/controls";
import { StatusChip, type ChipKind } from "../ui/StatusChip";

export interface SecretMessage {
  text: string;
  ok: boolean;
}

const chipKinds: Readonly<Record<SecretStatus, ChipKind>> = { set: "ok", unset: "wait", unsupported: "fail", unavailable: "fail" };

// A write-only secret: the page shows only its status. The typed value lives in this component's state until the
// call returns and goes nowhere else (no other state, no log, no error text). Used for the mail password and the Hub
// key; nested renders it as a part of the enclosing block.
export function SecretSlot(props: {
  labels: SecretLabels;
  status: SecretStatus;
  busy: boolean;
  message: SecretMessage | null;
  nested?: boolean;
  onStore: (value: string) => Promise<void>;
  onRemove: () => Promise<void>;
}) {
  const [value, setValue] = useState("");
  const [confirming, setConfirming] = useState(false);
  const { labels, status } = props;
  const c = secretControls(status, props.busy, value !== "");
  // An unknown status from a newer core reads as "не ответило", like its label.
  const kind = Object.hasOwn(chipKinds, status) ? chipKinds[status] : "fail";
  const storeWorks = status === "set" || status === "unset";
  const note = secretStatusNote(labels, status);

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
    <div className={props.nested ? undefined : "card box"}>
      <h2 className="sub">{labels.title}</h2>
      <div className="secret">
        <StatusChip kind={kind} icon="key">
          {secretChipLabel(labels, status)}
        </StatusChip>
        {note !== "" && <span className="small muted">{note}</span>}
      </div>
      {storeWorks && <p className="small muted">{secretStoreCaption}</p>}
      <Field label={labels.input} hint={c.removeShown ? labels.replaceHint : undefined}>
        {(id) => (
          <Input
            id={id}
            type="password"
            autoComplete="new-password"
            spellCheck={false}
            value={value}
            disabled={c.inputDisabled}
            onChange={(e) => setValue(e.target.value)}
          />
        )}
      </Field>
      <div className="acts start">
        <Button disabled={c.saveDisabled} onClick={() => void store()}>
          {labels.save}
        </Button>
        {c.removeShown && (
          <Button variant="danger" disabled={c.removeDisabled} onClick={() => setConfirming(true)}>
            {labels.remove}
          </Button>
        )}
      </div>
      <Dialog
        open={c.removeShown && confirming}
        title={labels.confirmTitle}
        confirmLabel={labels.confirmYes}
        cancelLabel={labels.confirmNo}
        confirmDisabled={c.removeDisabled}
        danger
        onConfirm={() => void remove()}
        onCancel={() => setConfirming(false)}
      >
        {labels.confirmRemove}
      </Dialog>
      {props.message !== null && (
        <ResultLine kind={props.message.ok ? "ok" : "fail"} role={props.message.ok ? "status" : "alert"}>
          {props.message.text}
        </ResultLine>
      )}
    </div>
  );
}
