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
} from "../diagnosticsView";

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
    <section>
      <h1>{texts.title}</h1>

      <div className="block">
        <h2>{texts.soundTitle}</h2>
        <p>{texts.soundExplain}</p>
        <p className="muted">{texts.soundScope}</p>
        <div className="actions">
          <button type="button" disabled={check.disabled} onClick={() => void runCheck()}>
            {check.label}
          </button>
        </div>
        {checking && <p className="muted">{texts.checkTakes}</p>}
        {sound !== null && (
          <>
            <Lines lines={sound.lines} />
            {sound.modes.length > 0 && (
              <>
                <h3>{texts.modesTitle}</h3>
                <table className="list">
                  <tbody>
                    {sound.modes.map((m) => (
                      <tr key={m.label}>
                        <th scope="row">{m.label}</th>
                        <td className={m.ok ? "ok" : "error"}>{m.text}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </>
            )}
          </>
        )}
      </div>

      <div className="block">
        <h2>{texts.bundleTitle}</h2>
        <p>{texts.bundleExplain}</p>
        <p className="muted">{texts.bundleLocal}</p>
        <div className="actions">
          <button type="button" disabled={save.disabled} onClick={() => void saveBundle()}>
            {save.label}
          </button>
        </div>
        <Lines lines={bundle} />
      </div>
    </section>
  );
}

function Lines(props: { lines: readonly Line[] }) {
  return (
    <>
      {props.lines.map((l, i) => (
        <p key={i} className={l.tone} role={l.tone === "error" ? "alert" : l.tone === "ok" ? "status" : undefined}>
          {l.text}
        </p>
      ))}
    </>
  );
}
