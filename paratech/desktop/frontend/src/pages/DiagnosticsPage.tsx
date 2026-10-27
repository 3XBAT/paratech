import { useEffect, useId, useMemo, useRef, useState } from "react";
import { diagnosticsApi, type SoundCheck } from "../bindings/diagnostics";
import { promptApi } from "../bindings/prompt";
import { callByName } from "../bindings/runtime";
import {
  bundleButton,
  bundleFailedLines,
  bundleLines,
  checkButton,
  heardMarkPercent,
  pickerHint,
  pollSoundCheck,
  soundDisplay,
  soundFailedDisplay,
  texts,
  type Line,
  type SoundDisplay,
  type Timers,
  type Tone,
} from "../diagnosticsView";
import { SourceNotes, SourcePicker } from "../sourcePicker/SourcePicker";
import { useSources } from "../sourcePicker/useSources";
import { pickerModel, savedSelection } from "../sourcePicker/view";
import { Button } from "../ui/Button";
import { Card, ResultLine, type ResultKind } from "../ui/controls";

const timers: Timers = {
  set: (fn, ms) => window.setTimeout(fn, ms),
  clear: (handle) => window.clearTimeout(handle as number),
};

// "Диагностика": the sound check and the support bundle. Both run in the Go core; the page shows the fixed texts it
// gets back (ARCH-44). The check listens to the remembered recording source, chosen here in the popup's picker
// (TASK-081); the picker is locked while a check runs.
export function DiagnosticsPage() {
  const api = useMemo(() => diagnosticsApi(callByName), []);
  const prompts = useMemo(() => promptApi(callByName), []);
  const mounted = useRef(true);

  const [calling, setCalling] = useState(false);
  const [sound, setSound] = useState<SoundDisplay | null>(null);
  const [hint, setHint] = useState("");

  // The source picker: the app being remembered while selectSource runs, and the error of the last choice.
  const [listOpen, setListOpen] = useState(false);
  const [saving, setSaving] = useState<string | null>(null);
  const [selectError, setSelectError] = useState("");
  const labelId = useId();
  const { sources, refresh, remembered } = useSources(!listOpen && saving === null);

  const [bundleSaving, setBundleSaving] = useState(false);
  const [bundle, setBundle] = useState<Line[]>([]);

  // A double click must not start a second call before the disabled button renders.
  const running = useRef(new Set<string>());

  // The poller of a running check and whether the last answer was running (to stop it when the page is left).
  const stopPolling = useRef<(() => void) | null>(null);
  const checkRuns = useRef(false);

  function halt() {
    stopPolling.current?.();
    stopPolling.current = null;
  }

  function show(r: SoundCheck) {
    setSound(soundDisplay(r));
    setHint(pickerHint(r));
  }

  function failed() {
    halt();
    checkRuns.current = false;
    setSound(soundFailedDisplay());
    setHint("");
  }

  // Shows an answer and polls again while the check runs. After the page is left a running check is stopped.
  function apply(r: SoundCheck) {
    if (!mounted.current) {
      if (r.state === "running") {
        api.stopSoundCheck().catch(() => undefined);
      }
      return;
    }
    checkRuns.current = r.state === "running";
    show(r);
    if (r.state !== "running") {
      halt();
      return;
    }
    if (stopPolling.current === null) {
      stopPolling.current = pollSoundCheck(
        () => api.soundCheckStatus(),
        (next) => {
          checkRuns.current = next.state === "running";
          if (!checkRuns.current) {
            stopPolling.current = null; // the poller ended itself
          }
          show(next);
        },
        failed,
        timers,
      );
    }
  }

  useEffect(() => {
    mounted.current = true;
    // A check that runs already (started before a page switch) is picked up; an old result is not shown again, and
    // a failed read here shows nothing: the user has not asked for anything yet.
    api.soundCheckStatus().then(
      (r) => r.state === "running" && apply(r),
      () => undefined,
    );
    return () => {
      mounted.current = false;
      halt();
      if (checkRuns.current) {
        checkRuns.current = false;
        api.stopSoundCheck().catch(() => undefined);
      }
    };
  }, [api]);

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

  const toggleCheck = () =>
    once("sound", setCalling, async () => {
      // A poll answer must not overwrite the answer of stop.
      halt();
      try {
        apply(checking ? await api.stopSoundCheck() : await api.startSoundCheck());
      } catch {
        if (mounted.current) {
          failed();
        }
      }
    });

  const saveBundle = () =>
    once("bundle", setBundleSaving, async () => {
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

  const selection = savedSelection(sources, saving);
  const picker = pickerModel(sources, selection);

  // The old result was about the previous source: it goes once another one is chosen.
  async function choose(app: string) {
    setListOpen(false);
    if (saving !== null || checkRuns.current || app === selection.app) {
      return;
    }
    setSaving(app);
    setSelectError("");
    setSound(null);
    setHint("");
    try {
      await prompts.selectSource(app);
      if (mounted.current) {
        remembered(app);
      }
    } catch {
      if (mounted.current) {
        setSelectError(texts.selectFailed);
      }
    } finally {
      if (mounted.current) {
        setSaving(null);
        refresh();
      }
    }
  }

  const check = checkButton(checking, calling);
  const save = bundleButton(bundleSaving);

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">{texts.lead}</span>
          <h2>{texts.title}</h2>
        </div>
      </div>

      <div className="grid2">
        <Card className="box diag" label={texts.soundTitle}>
          <h2>{texts.soundTitle}</h2>
          <span className="small muted box-desc">{texts.soundExplain}</span>
          <span className="label" id={labelId}>
            {texts.sourceLabel}
          </span>
          <div className="acts start diag-pick">
            <SourcePicker
              picker={picker}
              disabled={sources === null || saving !== null || checking || calling}
              disabledReason={checking ? texts.sourceLocked : undefined}
              open={listOpen}
              onToggle={() => setListOpen((open) => !open)}
              onClose={() => setListOpen(false)}
              onChoose={(app) => void choose(app)}
              labelId={labelId}
            />
            <Button disabled={check.disabled || saving !== null} onClick={() => void toggleCheck()}>
              {check.label}
            </Button>
          </div>
          {hint !== "" && <p className="small picker-warning">{hint}</p>}
          <SourceNotes picker={picker} />
          {selectError !== "" && (
            <ResultLine kind="fail" role="alert">
              {selectError}
            </ResultLine>
          )}
          {sound !== null && <SoundView sound={sound} />}
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

// The source, the hint, the live level with the "слышно" mark and the seconds left, then the result lines.
function SoundView(props: { sound: SoundDisplay }) {
  const s = props.sound;
  return (
    <div className="diag-result">
      {s.source !== "" && <p className="small muted diag-note">{s.source}</p>}
      {s.prompt !== "" && <p className="diag-prompt">{s.prompt}</p>}
      {s.running && (
        <div className="level-wrap">
          <div
            className={s.heard ? "level heard" : "level"}
            role="meter"
            aria-label={texts.levelLabel}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={s.percent}
            aria-valuetext={`${s.percent} %`}
          >
            <b style={{ width: `${s.percent}%` }} />
            <i style={{ left: `${heardMarkPercent}%` }} aria-hidden="true" />
          </div>
          <span className="small muted num">{s.remaining}</span>
        </div>
      )}
      <Lines lines={s.lines} />
    </div>
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
