import { useEffect, useMemo, useRef, useState } from "react";
import {
  connectionsApi,
  type CheckResult,
  type Connections,
  type FieldError,
  type SecretResult,
} from "../bindings/connections";
import { callByName } from "../bindings/runtime";
import {
  checkControls,
  failureText,
  fieldErrorText,
  hubFields,
  hubKeyLabels,
  mailConfiguredText,
  mailFields,
  mailFormDirty,
  mailFormFrom,
  mailInputFrom,
  mailPasswordLabels,
  otherErrorTexts,
  secretOutcomeOk,
  securityOptions,
  texts,
  type FieldCode,
  type MailForm,
} from "../connectionsView";
import {
  quotaControls,
  quotaDisplay,
  quotaRefreshable,
  quotaTexts,
  quotaUsedPercent,
  type QuotaMessage,
} from "../quotaView";
import { Button } from "../ui/Button";
import { Card, Field, Input, Progress, ResultLine, Select } from "../ui/controls";
import { GoogleBlock } from "./GoogleBlock";
import { SecretSlot, type SecretMessage } from "./SecretSlot";

const mailCodes: readonly FieldCode[] = mailFields.map((f) => f.code);

// "Подключения": the Hub address, the Hub key and the remaining quota, the mail account, the mail password and the
// mail check and the Google sign-in (GoogleBlock), in cards (docs/design/concept-final.html). The page only shows
// and forwards the values; validation, the secret store and the quota are in the Go core (ARCH-44).
export function ConnectionsPage() {
  const api = useMemo(() => connectionsApi(callByName), []);
  const [saved, setSaved] = useState<Connections | null>(null);
  const [loadError, setLoadError] = useState("");
  const mounted = useRef(true);

  const [hubUrl, setHubUrl] = useState("");
  const [hubBusy, setHubBusy] = useState(false);
  const [hubErrors, setHubErrors] = useState<FieldError[]>([]);
  const [hubDone, setHubDone] = useState("");

  const [hubKeyBusy, setHubKeyBusy] = useState(false);
  const [hubKeyMessage, setHubKeyMessage] = useState<SecretMessage | null>(null);

  const [refreshing, setRefreshing] = useState(false);
  const [quotaMessage, setQuotaMessage] = useState<QuotaMessage | null>(null);

  const [mail, setMail] = useState<MailForm | null>(null);
  const [mailBusy, setMailBusy] = useState(false);
  const [mailErrors, setMailErrors] = useState<FieldError[]>([]);
  const [mailDone, setMailDone] = useState("");

  const [secretBusy, setSecretBusy] = useState(false);
  const [secretMessage, setSecretMessage] = useState<SecretMessage | null>(null);

  const [checking, setChecking] = useState(false);
  const [check, setCheck] = useState<CheckResult | null>(null);

  // A double click must not start a second call before the disabled button renders.
  const running = useRef(new Set<string>());

  async function once(key: string, setBusy: (b: boolean) => void, action: () => Promise<void>) {
    if (running.current.has(key)) {
      return;
    }
    running.current.add(key);
    setBusy(true);
    try {
      await action();
    } finally {
      running.current.delete(key);
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  const saveHub = () =>
    once("hub", setHubBusy, async () => {
      setHubErrors([]);
      setHubDone("");
      try {
        const r = await api.saveHubUrl(hubUrl);
        if (!mounted.current) {
          return;
        }
        if (r.saved && r.connections !== null) {
          const c = r.connections;
          setSaved(c);
          setHubUrl(c.hubUrl);
          setHubDone(texts.saved);
          setQuotaMessage(null); // an answer of the old address would mislead
          if (quotaRefreshable(c)) {
            void refreshQuota();
          }
        } else {
          setHubErrors(r.errors); // the typed value stays for correction
        }
      } catch {
        if (mounted.current) {
          setHubErrors([{ field: "", message: failureText("save") }]);
        }
      }
    });

  const saveMail = () => {
    if (mail === null) {
      return;
    }
    return once("mail", setMailBusy, async () => {
      setMailErrors([]);
      setMailDone("");
      try {
        const r = await api.saveMailSettings(mailInputFrom(mail));
        if (!mounted.current) {
          return;
        }
        if (r.saved && r.connections !== null) {
          const c = r.connections;
          setSaved(c);
          setMail(mailFormFrom(c));
          setMailDone(texts.saved);
          setCheck(null); // a result of the old settings would mislead
        } else {
          setMailErrors(r.errors);
        }
      } catch {
        if (mounted.current) {
          setMailErrors([{ field: "", message: failureText("save") }]);
        }
      }
    });
  };

  async function changeSecret(action: "store" | "remove", call: () => Promise<SecretResult>) {
    await once("secret", setSecretBusy, async () => {
      setSecretMessage(null);
      try {
        const r = await call();
        if (mounted.current) {
          setSaved((s) => (s === null ? s : { ...s, mailPassword: r.status }));
          setSecretMessage({ text: r.message, ok: secretOutcomeOk(action, r.status) });
          setCheck(null); // the check ran with the old password
        }
      } catch {
        if (mounted.current) {
          setSecretMessage({ text: failureText("save"), ok: false });
        }
      }
    });
  }

  // The key goes straight from the slot to the call. The re-read and the refresh run after the change returns, so the
  // slot clears its field without waiting for the Hub.
  async function changeHubKey(action: "store" | "remove", call: () => Promise<SecretResult>) {
    await once("hubKey", setHubKeyBusy, async () => {
      setHubKeyMessage(null);
      try {
        const r = await call();
        if (mounted.current) {
          setSaved((s) => (s === null ? s : { ...s, hubKey: r.status }));
          setHubKeyMessage({ text: r.message, ok: secretOutcomeOk(action, r.status) });
          setQuotaMessage(null); // the answer belonged to the old key
        }
      } catch {
        if (mounted.current) {
          setHubKeyMessage({ text: failureText("save"), ok: false });
        }
      }
    });
    void reloadHub();
  }

  // Reads the key status and the quota again (the core forgets the quota when the key changes) and asks for the
  // quota of a usable key.
  async function reloadHub() {
    try {
      const c = await api.get();
      if (!mounted.current) {
        return;
      }
      setSaved((s) => (s === null ? s : { ...s, hubKey: c.hubKey, quota: c.quota }));
      if (quotaRefreshable(c)) {
        void refreshQuota();
      }
    } catch {
      if (mounted.current) {
        setQuotaMessage({ text: failureText("load"), ok: false });
      }
    }
  }

  // Asks the core for the quota; the reply carries the fixed text and the quota to show, stale or not.
  const refreshQuota = () =>
    once("quota", setRefreshing, async () => {
      setQuotaMessage(null);
      try {
        const r = await api.refreshQuota();
        if (mounted.current) {
          setSaved((s) => (s === null ? s : { ...s, quota: r.quota }));
          setQuotaMessage({ text: r.message, ok: r.ok });
        }
      } catch {
        if (mounted.current) {
          setQuotaMessage({ text: quotaTexts.refreshFailed, ok: false });
        }
      }
    });

  // On open: read the section, then ask once for the quota when an address and a key are set.
  useEffect(() => {
    mounted.current = true;
    api.get().then(
      (c) => {
        if (mounted.current) {
          setSaved(c);
          setHubUrl(c.hubUrl);
          setMail(mailFormFrom(c));
          if (quotaRefreshable(c)) {
            void refreshQuota();
          }
        }
      },
      () => mounted.current && setLoadError(failureText("load")),
    );
    return () => {
      mounted.current = false;
    };
  }, [api]); // refreshQuota uses only refs, state setters and api


  const runCheck = () =>
    once("check", setChecking, async () => {
      setCheck(null);
      try {
        const r = await api.checkMail();
        if (mounted.current) {
          setCheck(r);
        }
      } catch {
        if (mounted.current) {
          setCheck({ ok: false, message: failureText("check") });
        }
      }
    });

  if (saved === null || mail === null) {
    return (
      <section className="page">
        <div className="head">
          <h1>Подключения</h1>
        </div>
        {loadError ? (
          <ResultLine kind="fail" role="alert">
            {loadError}
          </ResultLine>
        ) : (
          <p className="muted">{texts.loading}</p>
        )}
      </section>
    );
  }

  const dirty = mailFormDirty(mail, saved);
  const checkState = checkControls({ dirty, checking, saving: mailBusy, secretBusy });
  const quota = quotaDisplay(saved.quota, saved, quotaMessage);
  const quotaState = quotaControls({ refreshing, keyBusy: hubKeyBusy, hubBusy });
  const used = quotaUsedPercent(saved.quota);

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">Сервер обработки, почта и Google Календарь</span>
          <h1>Подключения</h1>
        </div>
      </div>

      <div className="grid2">
        <Card className="box" label="Сервер обработки">
          <h2>Сервер обработки</h2>
          <span className="small muted box-desc">
            Делает конспекты и ищет домашние задания. Звук туда не отправляется — только текст расшифровки.
          </span>
          <Field label="Адрес сервера" error={fieldErrorText(hubErrors, "hubUrl")}>
            {(id) => (
              <Input
                id={id}
                type="text"
                inputMode="url"
                spellCheck={false}
                placeholder={texts.hubPlaceholder}
                value={hubUrl}
                disabled={hubBusy}
                onChange={(e) => setHubUrl(e.target.value)}
              />
            )}
          </Field>
          <Errors texts={otherErrorTexts(hubErrors, hubFields)} />
          <div className="acts start">
            <Button disabled={hubBusy} onClick={() => void saveHub()}>
              Сохранить
            </Button>
          </div>
          {hubDone && (
            <ResultLine kind="ok" role="status">
              {hubDone}
            </ResultLine>
          )}

          <hr className="sep" />
          <SecretSlot
            nested
            labels={hubKeyLabels}
            status={saved.hubKey}
            busy={hubKeyBusy}
            message={hubKeyMessage}
            onStore={(value) => changeHubKey("store", () => api.setHubKey(value))}
            onRemove={() => changeHubKey("remove", () => api.removeHubKey())}
          />

          <hr className="sep" />
          <h3>{quotaTexts.title}</h3>
          {used !== null && (
            <div className="meter">
              <Progress value={used} label={quotaTexts.used} thick text={`${quotaTexts.used} ${used} %`} />
            </div>
          )}
          {quota.amounts.length > 0 && (
            <dl className="amounts">
              {quota.amounts.map((a) => (
                <div key={a.label}>
                  <dt className="small muted">{a.label}</dt>
                  <dd className="num">{a.value}</dd>
                </div>
              ))}
            </dl>
          )}
          {quota.updated && <p className="small">{quota.updated}</p>}
          {quota.stale && <ResultLine kind="wait">{quota.stale}</ResultLine>}
          {quota.renews && <p className="small muted">{quota.renews}</p>}
          {quota.note && <p className="small muted">{quota.note}</p>}
          {quota.checked && <p className="small muted">{quota.checked}</p>}
          {quota.lines.map((l, i) =>
            l.tone === "muted" ? (
              <p key={i} className="small muted">
                {l.text}
              </p>
            ) : (
              <ResultLine
                key={i}
                kind={l.tone === "ok" ? "ok" : l.tone === "error" ? "fail" : "wait"}
                role={l.tone === "error" ? "alert" : l.tone === "ok" ? "status" : undefined}
              >
                {l.text}
              </ResultLine>
            ),
          )}
          <div className="acts start">
            <Button disabled={quotaState.disabled} onClick={() => void refreshQuota()}>
              {quotaState.label}
            </Button>
          </div>
        </Card>

        <Card className="box" label="Почта">
          <h2>Почта</h2>
          {saved.mailConfigured ? (
            <span className="small muted box-desc">{mailConfiguredText(true)}</span>
          ) : (
            <ResultLine kind="wait">{mailConfiguredText(false)}</ResultLine>
          )}
          {mailFields.map((f) => (
            <Field
              key={f.code}
              label={f.label}
              hint={f.code === "smtpPort" ? texts.portHint : undefined}
              error={fieldErrorText(mailErrors, f.code)}
            >
              {(id) =>
                f.code === "smtpSecurity" ? (
                  <Select
                    id={id}
                    value={mail.smtpSecurity}
                    disabled={mailBusy}
                    onChange={(e) =>
                      setMail({ ...mail, smtpSecurity: e.target.value === "tls" ? "tls" : "starttls" })
                    }
                  >
                    {securityOptions.map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </Select>
                ) : (
                  <Input
                    id={id}
                    type="text"
                    inputMode={f.code === "smtpPort" ? "numeric" : f.code === "smtpHost" ? "url" : "email"}
                    spellCheck={false}
                    autoComplete="off"
                    value={mail[f.code]}
                    disabled={mailBusy}
                    onChange={(e) => setMail({ ...mail, [f.code]: e.target.value })}
                  />
                )
              }
            </Field>
          ))}
          <Errors texts={otherErrorTexts(mailErrors, mailCodes)} />
          <div className="acts start">
            <Button disabled={mailBusy} onClick={() => void saveMail()}>
              Сохранить
            </Button>
          </div>
          {mailDone && !dirty && (
            <ResultLine kind="ok" role="status">
              {mailDone}
            </ResultLine>
          )}

          <hr className="sep" />
          <SecretSlot
            nested
            labels={mailPasswordLabels}
            status={saved.mailPassword}
            busy={secretBusy}
            message={secretMessage}
            onStore={(value) => changeSecret("store", () => api.setMailPassword(value))}
            onRemove={() => changeSecret("remove", () => api.removeMailPassword())}
          />

          <hr className="sep" />
          <h3>Проверка почты</h3>
          <p className="small muted">{texts.checkNoSend}</p>
          <div className="acts start">
            <Button
              disabled={checkState.disabled}
              disabledReason={checkState.hint}
              busy={checking}
              busyLabel={texts.checking}
              onClick={() => void runCheck()}
            >
              Проверить почту
            </Button>
          </div>
          {check !== null && (
            <ResultLine kind={check.ok ? "ok" : "fail"} role={check.ok ? "status" : "alert"}>
              {check.message}
            </ResultLine>
          )}
        </Card>

        <GoogleBlock />
      </div>
    </section>
  );
}

function Errors(props: { texts: readonly string[] }) {
  return (
    <>
      {props.texts.map((t, i) => (
        <ResultLine key={i} kind="fail" role="alert">
          {t}
        </ResultLine>
      ))}
    </>
  );
}
