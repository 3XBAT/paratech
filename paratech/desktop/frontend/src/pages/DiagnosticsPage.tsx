import { useEffect, useMemo, useRef, useState } from "react";
import { diagnosticsApi } from "../bindings/diagnostics";
import { callByName } from "../bindings/runtime";
import {
  bundleButton,
  bundleFailedLines,
  bundleLines,
  checkButton,
  soundDisplay,
  soundFailedDisplay,
  texts,
  type Line,
  type SoundDisplay,
  type Tone,
} from "../diagnosticsView";
import { Button } from "../ui/Button";
import { Card, ResultLine, type ResultKind } from "../ui/controls";
import { StatusChip } from "../ui/StatusChip";

// "Диагностика": the sound check and the support bundle. Both run in the Go core; the page shows the fixed texts it
// gets back (ARCH-44).
export function DiagnosticsPage() {
  const api = useMemo(() => diagnosticsApi(callByName), []);
  const mounted = useRef(true);

  const [checking, setChecking] = useState(false);
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

  const runCheck = () =>
    once("sound", setChecking, async () => {
      setSound(null);
      try {
        const r = await api.checkSound();
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

  const check = checkButton(checking);
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
          <p className="small muted">{texts.soundScope}</p>
          <div className="acts start">
            <Button disabled={check.disabled} onClick={() => void runCheck()}>
              {check.label}
            </Button>
          </div>
          {checking && <p className="small muted diag-wait">{texts.checkTakes}</p>}
          {sound !== null && (
            <div className="diag-result">
              <Lines lines={sound.lines} />
              {sound.modes.length > 0 && (
                <>
                  <h3>{texts.modesTitle}</h3>
                  <ul className="modes">
                    {sound.modes.map((m) => (
                      <li key={m.label}>
                        <span>{m.label}</span>
                        <StatusChip kind={m.ok ? "ok" : "fail"} icon={m.ok ? "ok" : "warn"}>
                          {m.ok ? texts.modeWorks : texts.modeFailed}
                        </StatusChip>
                        {!m.ok && <span className="small muted mode-cause">{m.text}</span>}
                      </li>
                    ))}
                  </ul>
                </>
              )}
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
