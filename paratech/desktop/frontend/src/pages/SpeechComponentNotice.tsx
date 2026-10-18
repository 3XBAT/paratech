import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { callByName } from "../bindings/runtime";
import { speechApi, type SpeechStatus } from "../bindings/speech";
import { speechErrorText, speechNotice, speechTexts } from "../speechView";
import { Button } from "../ui/Button";
import { Banner, Progress } from "../ui/controls";

// Same cadence as the model downloads on the models page.
const pollMs = 2000;

// The notice of a missing, damaged or installing speech recognition component, shared by the models and recordings
// pages. It shows nothing while recognition works. onReady lets the page reload what depends on recognition.
export function SpeechComponentNotice({ onReady }: { onReady?: () => void }) {
  const api = useMemo(() => speechApi(callByName), []);
  const [status, setStatus] = useState<SpeechStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"" | "start" | "cancel">("");
  const mounted = useRef(true);
  const seq = useRef(0); // only the latest load is applied, so a slow old reply never hides a newer state
  const ready = useRef<boolean | null>(null);
  const readyCallback = useRef(onReady);
  readyCallback.current = onReady;

  const load = useCallback(async () => {
    const n = ++seq.current;
    try {
      const s = await api.status();
      if (!mounted.current || n !== seq.current) {
        return;
      }
      setStatus(s);
      const nowReady = s.state === "ready";
      if (nowReady && ready.current === false) {
        setError("");
        readyCallback.current?.();
      }
      ready.current = nowReady;
    } catch {
      // Status never fails in the core; a broken reply keeps the last shown state until the next load.
    }
  }, [api]);

  useEffect(() => {
    mounted.current = true;
    void load();
    return () => {
      mounted.current = false;
    };
  }, [load]);

  const installing = status?.state === "installing";
  useEffect(() => {
    if (!installing) {
      return;
    }
    const timer = setInterval(() => void load(), pollMs);
    return () => clearInterval(timer);
  }, [installing, load]);

  async function act(kind: "start" | "cancel", action: () => Promise<void>) {
    setBusy(kind);
    setError("");
    try {
      await action();
    } catch (err) {
      if (mounted.current) {
        setError(speechErrorText(err));
      }
    } finally {
      if (mounted.current) {
        setBusy("");
      }
    }
    if (mounted.current) {
      await load();
    }
  }

  const n = status !== null ? speechNotice(status) : null;
  if (n === null) {
    return null;
  }
  return (
    <Banner kind={n.kind} role="status">
      <p>{n.text}</p>
      {n.hint !== "" && <p className="small muted">{n.hint}</p>}
      {n.percent !== null && <Progress value={n.percent} label={speechTexts.installing} text={false} />}
      {n.progress !== "" && <p className="small muted num">{n.progress}</p>}
      {error !== "" && (
        <p className="small problem" role="alert">
          {error}
        </p>
      )}
      {(n.install || n.cancel) && (
        <div className="acts start">
          {n.install && (
            <Button
              variant="primary"
              icon="down"
              busy={busy === "start"}
              busyLabel={speechTexts.starting}
              disabled={busy !== ""}
              onClick={() => void act("start", () => api.startInstall())}
            >
              {speechTexts.install}
            </Button>
          )}
          {n.cancel && (
            <Button
              busy={busy === "cancel"}
              busyLabel={speechTexts.cancelling}
              disabled={busy !== ""}
              onClick={() => void act("cancel", () => api.cancelInstall())}
            >
              {speechTexts.cancel}
            </Button>
          )}
        </div>
      )}
    </Banner>
  );
}
