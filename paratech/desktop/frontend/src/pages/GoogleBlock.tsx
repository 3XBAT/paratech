import { useEffect, useId, useMemo, useRef, useState } from "react";
import { googleApi, type GoogleResult, type GoogleState } from "../bindings/google";
import { callByName } from "../bindings/runtime";
import {
  accountStatusText,
  clientStatusText,
  googleControls,
  googleTexts,
  pollStatus,
  statusPollMs,
  statusWarns,
  type GoogleAction,
} from "../googleView";

interface Message {
  text: string;
  ok: boolean;
}

// «Google Календарь» on "Подключения": the client id and secret, the sign-in in the system browser and the sign-out.
// The client id and secret are write-only: typed values live in this component's state until the save call returns and
// go nowhere else. The core keeps them and the token in Credential Manager (ARCH-44).
export function GoogleBlock() {
  const api = useMemo(() => googleApi(callByName), []);
  const idField = useId();
  const secretField = useId();
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
      <p className="muted">{googleTexts.hint}</p>
    </>
  );

  if (state === null) {
    return (
      <div className="block">
        {header}
        {loadFailed ? (
          <p className="error" role="alert">
            {googleTexts.loadFailed}
          </p>
        ) : (
          <p className="muted">{googleTexts.loading}</p>
        )}
      </div>
    );
  }

  const c = googleControls({ state, action, typedId: clientId !== "", typedSecret: secret !== "" });

  return (
    <div className="block">
      {header}
      <p className={statusWarns(state.client) ? "warning" : ""}>{clientStatusText(state.client)}</p>
      <p className={statusWarns(state.account) ? "warning" : ""}>{accountStatusText(state.account)}</p>

      <div className="field">
        <label htmlFor={idField}>{googleTexts.clientId}</label>
        <input
          id={idField}
          type="text"
          spellCheck={false}
          autoComplete="off"
          value={clientId}
          disabled={c.inputsDisabled}
          onChange={(e) => setClientId(e.target.value)}
        />
      </div>
      <div className="field">
        <label htmlFor={secretField}>{googleTexts.clientSecret}</label>
        <input
          id={secretField}
          type="password"
          autoComplete="new-password"
          spellCheck={false}
          value={secret}
          disabled={c.inputsDisabled}
          onChange={(e) => setSecret(e.target.value)}
        />
        {c.removeShown && <span className="muted">{googleTexts.replaceHint}</span>}
      </div>
      <div className="actions">
        <button type="button" disabled={c.saveDisabled} onClick={() => void save()}>
          {action === "save" ? googleTexts.saving : googleTexts.save}
        </button>
        {c.removeShown && !confirming && (
          <button type="button" disabled={c.removeDisabled} onClick={() => setConfirming(true)}>
            {googleTexts.remove}
          </button>
        )}
      </div>
      {c.removeShown && confirming && (
        <div className="confirm">
          <p>{googleTexts.confirmRemove}</p>
          <div className="actions">
            <button type="button" disabled={c.removeDisabled} onClick={() => void run("remove", () => api.clearClient())}>
              {googleTexts.confirmYes}
            </button>
            <button type="button" onClick={() => setConfirming(false)}>
              {googleTexts.confirmNo}
            </button>
          </div>
        </div>
      )}

      {c.signingIn ? (
        <>
          <p role="status">{googleTexts.waiting}</p>
          <div className="actions">
            <button type="button" disabled={cancelling} onClick={() => void cancel()}>
              {googleTexts.cancel}
            </button>
          </div>
        </>
      ) : (
        <div className="actions">
          {c.signInShown && (
            <button type="button" disabled={c.signInDisabled} onClick={() => void run("signIn", () => api.signIn())}>
              {googleTexts.signIn}
            </button>
          )}
          {c.signOutShown && (
            <button type="button" disabled={c.signOutDisabled} onClick={() => void run("signOut", () => api.signOut())}>
              {action === "signOut" ? googleTexts.signingOut : googleTexts.signOut}
            </button>
          )}
        </div>
      )}

      {message !== null && (
        <p className={message.ok ? "ok" : "error"} role={message.ok ? "status" : "alert"}>
          {message.text}
        </p>
      )}
    </div>
  );
}
