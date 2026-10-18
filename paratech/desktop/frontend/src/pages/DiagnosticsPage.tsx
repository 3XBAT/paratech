import { useEffect, useMemo, useRef, useState } from "react";
import { diagnosticsApi } from "../bindings/diagnostics";
import { callByName } from "../bindings/runtime";
import {
  bundleButton,
  bundleFailedLines,
  bundleLines,
  checkButton,
  pollMs,
  soundDisplay,
  soundFailedDisplay,
  texts,
  type Line,
  type SoundDisplay,
  type Tone,
} from "../diagnosticsView";
import { Button } from "../ui/Button";
import { Card, ResultLine, type ResultKind } from "../ui/controls";

// "Диагностика": the sound check and the support bundle. Both run in the Go core; the page shows the fixed texts it
// gets back (ARCH-44).
export function DiagnosticsPage() {
  const api = useMemo(() => diagnosticsApi(callByName), []);
  const mounted = useRef(true);

  const [calling, setCalling] = useState(false);
  const [sound, setSound] = useState<SoundDisplay | null>(null);

  const [saving, setSaving] = useState(false);
  const [bundle, setBundle] = useState<Line[]>([]);

  // A double click must not start a second call before the disabled button renders.
  const running = useRef(new Set<string>());

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

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

  const checking = sound?.running ?? false;

  // While the check runs, its state is read again every pollMs; leaving the page stops the polling only.
  useEffect(() => {
    if (!checking) {
      return;
    }
    const timer = setTimeout(() => {
      api.soundCheckStatus().then(
        (r) => mounted.current && setSound(soundDisplay(r)),
        () => mounted.current && setSound(soundFailedDisplay()),
      );
    }, pollMs);
    return () => clearTimeout(timer);
  }, [api, checking, sound]);

  const toggleCheck = () =>
    once("sound", setCalling, async () => {
      try {
        const r = checking ? await api.stopSoundCheck() : await api.startSoundCheck();
        if (mounted.current) {
          setSound(soundDisplay(r));
        }
      } catch {
        if (mounted.current) {
          setSound(soundFailedDisplay());
        }
      }
    });

  const saveBundle = () =>
    once("bundle", setSaving, async () => {
      setBundle([]);
      try {
        const r = await api.saveSupportBundle();
        if (mounted.current) {
          setBundle(bundleLines(r));
        }
      } catch {
        if (mounted.current) {
          setBundle(bundleFailedLines());
        }
      }
    });

  const check = checkButton(checking, calling);
  const save = bundleButton(saving);

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">{texts.lead}</span>
          <h1>{texts.title}</h1>
        </div>
      </div>

      <div className="grid2">
        <Card className="box diag" label={texts.soundTitle}>
          <h2>{texts.soundTitle}</h2>
          <span className="small muted box-desc">{texts.soundExplain}</span>
          <div className="acts start">
            <Button disabled={check.disabled} onClick={() => void toggleCheck()}>
              {check.label}
            </Button>
          </div>
          {sound !== null && (
            <div className="diag-result">
              {sound.source !== "" && <p className="small muted diag-note">{sound.source}</p>}
              {sound.running && <progress max={1} value={sound.level} aria-label={texts.levelLabel} />}
              <Lines lines={sound.lines} />
            </div>
          )}
        </Card>

        <Card className="box diag" label={texts.bundleTitle}>
          <h2>{texts.bundleTitle}</h2>
          <span className="small muted box-desc">{texts.bundleExplain}</span>
          <p className="small muted">{texts.bundleLocal}</p>
          <div className="acts start">
            <Button disabled={save.disabled} onClick={() => void saveBundle()}>
              {save.label}
            </Button>
          </div>
          {bundle.length > 0 && (
            <div className="diag-result">
              <Lines lines={bundle} />
            </div>
          )}
        </Card>
      </div>
    </section>
  );
}

const resultKinds: Readonly<Record<Exclude<Tone, "muted">, ResultKind>> = { ok: "ok", warning: "wait", error: "fail" };

// A verdict is a result line with an icon; a muted line (the device, the note of a saved bundle) is small text.
function Lines(props: { lines: readonly Line[] }) {
  return (
    <>
      {props.lines.map((l, i) =>
        l.tone === "muted" ? (
          <p key={i} className="small muted diag-note">
            {l.text}
          </p>
        ) : (
          <ResultLine
            key={i}
            kind={resultKinds[l.tone]}
            role={l.tone === "error" ? "alert" : l.tone === "ok" ? "status" : undefined}
          >
            {l.text}
          </ResultLine>
        ),
      )}
    </>
  );
}
