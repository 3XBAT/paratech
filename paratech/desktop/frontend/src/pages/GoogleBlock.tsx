import { useEffect, useMemo, useRef, useState } from "react";
import { googleApi, type GoogleResult, type GoogleState } from "../bindings/google";
import { callByName } from "../bindings/runtime";
import { secretStoreCaption } from "../connectionsView";
import {
  accountChipText,
  accountStatusNote,
  clientChipText,
  clientStatusNote,
  googleControls,
  googleTexts,
  pollStatus,
  statusPollMs,
  statusWarns,
  type GoogleAction,
} from "../googleView";
import { Button } from "../ui/Button";
import { Card, Dialog, Field, Input, ResultLine } from "../ui/controls";
import { StatusChip, type ChipKind } from "../ui/StatusChip";

interface Message {
  text: string;
  ok: boolean;
}

// The chip of a status; a warning status (no store, no answer) reads as a failure, like the secret slots.
function clientChip(status: string): ChipKind {
  return statusWarns(status) ? "fail" : status === "set" ? "ok" : "wait";
}

function accountChip(status: string): ChipKind {
  return statusWarns(status) ? "fail" : status === "signedIn" ? "ok" : "neutral";
}

// «Google Календарь» on "Подключения": the client id and secret, the sign-in in the system browser and the sign-out.
// The client id and secret are write-only: typed values live in this component's state until the save call returns and
// go nowhere else. The core keeps them and the token in Credential Manager (ARCH-44).
export function GoogleBlock() {
  const api = useMemo(() => googleApi(callByName), []);
  const [state, setState] = useState<GoogleState | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [clientId, setClientId] = useState("");
  const [secret, setSecret] = useState("");
  const [action, setAction] = useState<GoogleAction>(null);
  const [cancelling, setCancelling] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [message, setMessage] = useState<Message | null>(null);
  const mounted = useRef(true);

  // A double click must not start a second call before the disabled button renders.
  const running = useRef(false);

  useEffect(() => {
    mounted.current = true;
    api.status().then(
      (s) => mounted.current && setState(s),
      () => mounted.current && setLoadFailed(true),
    );
    return () => {
      mounted.current = false;
    };
  }, [api]);

  // A sign-in started before the page was opened answers to nobody: follow it by its status.
  const poll = pollStatus(state, action);
  useEffect(() => {
    if (!poll) {
      return;
    }
    const timer = setInterval(() => {
      api.status().then(
        (s) => mounted.current && setState(s),
        () => {}, // the next tick tries again
      );
    }, statusPollMs);
    return () => clearInterval(timer);
  }, [poll, api]);

  async function run(a: Exclude<GoogleAction, null>, call: () => Promise<GoogleResult>): Promise<GoogleResult | null> {
    if (running.current) {
      return null;
    }
    running.current = true;
    setAction(a);
    setMessage(null);
    setConfirming(false);
    try {
      const r = await call();
      if (mounted.current) {
        setState(r.state);
        setMessage({ text: r.message, ok: r.ok });
      }
      return r;
    } catch {
      if (mounted.current) {
        setMessage({ text: googleTexts.actionFailed, ok: false });
      }
      return null;
    } finally {
      running.current = false;
      if (mounted.current) {
        setAction(null);
      }
    }
  }

  // The typed id stays after a refusal for correction; the secret is cleared whatever the outcome.
  async function save() {
    try {
      const r = await run("save", () => api.setClient(clientId, secret));
      if (r !== null && r.ok && mounted.current) {
        setClientId("");
      }
    } finally {
      setSecret("");
    }
  }

  async function cancel() {
    if (cancelling) {
      return;
    }
    setCancelling(true);
    try {
      const r = await api.cancelSignIn();
      if (mounted.current) {
        setMessage({ text: r.message, ok: r.ok });
      }
    } catch {
      if (mounted.current) {
        setMessage({ text: googleTexts.actionFailed, ok: false });
      }
    } finally {
      if (mounted.current) {
        setCancelling(false);
      }
    }
  }

  const header = (
    <>
      <h2>{googleTexts.title}</h2>
      <span className="small muted box-desc">{googleTexts.hint}</span>
    </>
  );

  if (state === null) {
    return (
      <Card className="box" label={googleTexts.title}>
        {header}
        {loadFailed ? (
          <ResultLine kind="fail" role="alert">
            {googleTexts.loadFailed}
          </ResultLine>
        ) : (
          <p className="muted">{googleTexts.loading}</p>
        )}
      </Card>
    );
  }

  const c = googleControls({ state, action, typedId: clientId !== "", typedSecret: secret !== "" });

  return (
    <Card className="box" label={googleTexts.title}>
      {header}

      <h3>{googleTexts.clientTitle}</h3>
      <div className="secret">
        <StatusChip kind={clientChip(state.client)} icon="key">
          {clientChipText(state.client)}
        </StatusChip>
        {clientStatusNote(state.client) !== "" && <span className="small muted">{clientStatusNote(state.client)}</span>}
      </div>
      {(state.client === "set" || state.client === "unset") && <p className="small muted">{secretStoreCaption}</p>}
      <Field label={googleTexts.clientId}>
        {(id) => (
          <Input
            id={id}
            type="text"
            spellCheck={false}
            autoComplete="off"
            value={clientId}
            disabled={c.inputsDisabled}
            onChange={(e) => setClientId(e.target.value)}
          />
        )}
      </Field>
      <Field label={googleTexts.clientSecret} hint={c.removeShown ? googleTexts.replaceHint : undefined}>
        {(id) => (
          <Input
            id={id}
            type="password"
            autoComplete="new-password"
            spellCheck={false}
            value={secret}
            disabled={c.inputsDisabled}
            onChange={(e) => setSecret(e.target.value)}
          />
        )}
      </Field>
      <div className="acts start">
        <Button
          disabled={c.saveDisabled}
          busy={action === "save"}
          busyLabel={googleTexts.saving}
          onClick={() => void save()}
        >
          {googleTexts.save}
        </Button>
        {c.removeShown && (
          <Button variant="danger" disabled={c.removeDisabled} onClick={() => setConfirming(true)}>
            {googleTexts.remove}
          </Button>
        )}
      </div>
      <Dialog
        open={c.removeShown && confirming}
        title={googleTexts.confirmRemove}
        confirmLabel={googleTexts.confirmYes}
        cancelLabel={googleTexts.confirmNo}
        confirmDisabled={c.removeDisabled}
        danger
        onConfirm={() => void run("remove", () => api.clearClient())}
        onCancel={() => setConfirming(false)}
      >
        {googleTexts.confirmRemoveNote}
      </Dialog>

      <hr className="sep" />
      <h3>{googleTexts.accountTitle}</h3>
      <div className="secret">
        <StatusChip kind={accountChip(state.account)} icon="cal">
          {accountChipText(state.account)}
        </StatusChip>
        {accountStatusNote(state.account) !== "" && <span className="small muted">{accountStatusNote(state.account)}</span>}
      </div>
      {c.signingIn ? (
        <>
          <div className="secret" role="status">
            <StatusChip kind="run" icon="clock">
              {googleTexts.waiting}
            </StatusChip>
          </div>
          <div className="acts start">
            <Button disabled={cancelling} onClick={() => void cancel()}>
              {googleTexts.cancel}
            </Button>
          </div>
        </>
      ) : (
        <div className="acts start">
          {c.signInShown && (
            <Button
              variant="primary"
              disabled={c.signInDisabled}
              disabledReason={state.client === "unset" ? googleTexts.signInNeedsClient : undefined}
              onClick={() => void run("signIn", () => api.signIn())}
            >
              {googleTexts.signIn}
            </Button>
          )}
          {c.signOutShown && (
            <Button
              disabled={c.signOutDisabled}
              busy={action === "signOut"}
              busyLabel={googleTexts.signingOut}
              onClick={() => void run("signOut", () => api.signOut())}
            >
              {googleTexts.signOut}
            </Button>
          )}
        </div>
      )}

      {message !== null && (
        <ResultLine kind={message.ok ? "ok" : "fail"} role={message.ok ? "status" : "alert"}>
          {message.text}
        </ResultLine>
      )}
    </Card>
  );
}
