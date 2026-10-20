import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { activityApi, type AdviceView } from "../bindings/activity";
import { popupWindowApi, promptApi, shownEvent, type PromptView, type SourcesView } from "../bindings/prompt";
import { callByName, hideWindow, onEvent } from "../bindings/runtime";
import { Button } from "../ui/Button";
import { Banner } from "../ui/controls";
import { IconSprite } from "../ui/Icon";
import { SoundLevel } from "../ui/SoundLevel";
import { soundTexts } from "../soundLevelView";
import { useRecordingSound } from "../useRecordingSound";
import {
  adviceHints,
  adviceKey,
  adviceLabels,
  adviceModel,
  keepSource,
  switchToSuggestion,
  type AdvicePending,
} from "../sourcePicker/advice";
import { SourcePicker } from "../sourcePicker/SourcePicker";
import {
  actionErrorText,
  chooseSource,
  failedSources,
  initialSelection,
  nextSelection,
  pickerModel,
  SourcesPoller,
  type Selection,
} from "../sourcePicker/view";
import {
  adviceHeading,
  buttonHints,
  buttonLabels,
  HeightFitter,
  loadErrorText,
  popupModel,
  recordingTexts,
  shouldRefresh,
  showsRecording,
  textsLayout,
  wantedHeight,
  type Action,
  type ButtonId,
} from "./view";

const browserTimers = {
  set: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clear: (handle: unknown) => window.clearTimeout(handle as number),
};

// The popup shows the prompt the Go core holds and passes the clicks back; the core hides the window after
// start and skip. While the running recording has a silent-source advice (TASK-082) the core shows the popup for it
// too, and the advice comes first. No element takes focus on its own: the popup must not steal it (AC-01).
export function Popup() {
  const api = useMemo(() => promptApi(callByName), []);
  const activity = useMemo(() => activityApi(callByName), []);
  const [view, setView] = useState<PromptView | null>(null);
  const [advice, setAdvice] = useState<AdviceView | null>(null);
  const [advicePending, setAdvicePending] = useState<AdvicePending>(null);
  const [adviceProblem, setAdviceProblem] = useState("");
  const adviceRef = useRef<AdviceView | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [pending, setPending] = useState<Action | null>(null);
  const [error, setError] = useState("");
  // Only the newest reply is applied: an older Current() must not overwrite a newer prompt or action result.
  const latest = useRef(0);

  // Source picker (TASK-059): the last reply for the current prompt and what will be recorded.
  const [sources, setSources] = useState<SourcesView | null>(null);
  const [selection, setSelection] = useState<Selection>(initialSelection);
  const [listOpen, setListOpen] = useState(false);
  const [visible, setVisible] = useState(() => document.visibilityState === "visible");
  const promptId = view?.id ?? "";
  const promptIdRef = useRef(promptId);

  // The running recording (TASK-099): its sound is read while the popup is visible and a recording captures.
  const { sound, refresh: refreshSound } = useRecordingSound(activity.recordingSound, visible && advice === null, true);
  const [stopping, setStopping] = useState(false);
  const [stopProblem, setStopProblem] = useState("");

  const reload = useCallback(
    async (clearError: boolean) => {
      const seq = ++latest.current;
      // A failed advice read only hides the advice: the prompt still shows.
      const adviceRead = api.advice().catch(() => null);
      try {
        const v = await api.current();
        const a = await adviceRead;
        if (seq === latest.current) {
          setView(v);
          // The problem of a click belongs to its advice: a new suggestion starts clean.
          if ((adviceRef.current && adviceKey(adviceRef.current)) !== (a && adviceKey(a))) {
            setAdviceProblem("");
          }
          adviceRef.current = a;
          setAdvice(a);
          if (clearError) {
            setError("");
          }
        }
      } catch {
        if (seq === latest.current) {
          setError(loadErrorText);
        }
      } finally {
        setLoaded(true);
      }
    },
    [api],
  );

  const loadSources = useCallback(async () => {
    const forPrompt = promptIdRef.current;
    let s: SourcesView;
    try {
      s = await api.sources();
    } catch {
      // A malformed or failed reply offers only the whole computer.
      s = failedSources(null);
    }
    if (forPrompt === promptIdRef.current) {
      setSources(s);
      setSelection((prev) => nextSelection(prev, s));
    }
  }, [api]);

  const poller = useMemo(() => new SourcesPoller(loadSources, browserTimers), [loadSources]);

  // Window growth for a start error: the natural content height is measured after every render and sent to the core.
  const fitter = useMemo(() => {
    const win = popupWindowApi(callByName);
    return new HeightFitter((h) => win.fit(h));
  }, []);
  const popupRef = useRef<HTMLDivElement>(null);
  const growsRef = useRef(false);
  const measure = useCallback(() => {
    const el = popupRef.current;
    let natural = 0;
    if (el !== null && growsRef.current) {
      // Laid out at its natural height for one synchronous read; the browser paints only the restored style.
      const prev = el.style.height;
      el.style.height = "auto";
      natural = el.getBoundingClientRect().height;
      el.style.height = prev;
    }
    fitter.request(wantedHeight(natural, growsRef.current));
  }, [fitter]);

  useEffect(() => {
    // Text scale or DPI changes and late fonts change the height without a render.
    window.addEventListener("resize", measure);
    void document.fonts?.ready.then(measure);
    return () => window.removeEventListener("resize", measure);
  }, [measure]);

  useEffect(() => {
    let alive = true;
    let off: (() => void) | undefined;
    onEvent(shownEvent, () => {
      // The core has just shown the window, whatever the page's visibility state says yet. It also reset its size;
      // the render after the reload measures the new content (the DOM still shows the previous offer here).
      setVisible(true);
      fitter.forget();
      void reload(true);
      poller.refreshNow();
      refreshSound();
      setStopProblem("");
    }).then(
      (stop) => {
        if (alive) {
          off = stop;
        } else {
          stop();
        }
      },
      () => setError(loadErrorText),
    );
    void reload(true);
    const onVisibility = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      alive = false;
      off?.();
      document.removeEventListener("visibilitychange", onVisibility);
      poller.setActive(false);
    };
  }, [reload, poller, fitter, refreshSound]);

  // A new prompt starts with a fresh picker: no choice carries over from the previous offer.
  useEffect(() => {
    promptIdRef.current = promptId;
    setSources(null);
    setSelection(initialSelection);
    setListOpen(false);
    poller.refreshNow();
  }, [promptId, poller]);

  const refresh = advice === null && shouldRefresh(view, visible, pending, listOpen);
  useEffect(() => {
    poller.setActive(refresh);
  }, [refresh, poller]);

  const m = popupModel(view, pending);
  const picker = pickerModel(sources, selection);
  const showPicker = m.buttons.includes("start");
  const errorLine = error || m.problem;
  const notes = showPicker ? [picker.autoNote, ...picker.warnings].filter((n) => n !== "") : [];
  const layout = textsLayout(notes, errorLine);
  const live = sound !== null && advice === null && showsRecording(view, sound.recording, pending);

  useLayoutEffect(() => {
    // The advice and the running recording keep the normal size: their texts are short.
    growsRef.current = advice === null && !live && layout.grows;
    measure();
  });

  async function adviceAct(kind: "switch" | "dismiss") {
    if (advice === null || advicePending !== null) {
      return;
    }
    setAdvicePending(kind);
    setAdviceProblem("");
    const out = kind === "switch" ? await switchToSuggestion(activity, advice) : await keepSource(activity, advice);
    setAdvicePending(null);
    if (out.kind === "problem") {
      setAdviceProblem(out.text);
      return;
    }
    // The core withdraws the advice and hides the popup unless a prompt is on offer: show what it holds now.
    adviceRef.current = null;
    setAdvice(null);
    void reload(false);
  }

  async function act(action: Action) {
    if (view === null || pending !== null) {
      return;
    }
    setPending(action);
    setListOpen(false);
    setError("");
    const seq = ++latest.current;
    try {
      const v = action === "start" ? await api.start(view.id, picker.chosen.app) : await api[action](view.id);
      if (seq === latest.current) {
        setView(v);
      }
    } catch (err) {
      setError(actionErrorText(err));
      // The prompt may have been replaced or ended meanwhile: show what the core holds now.
      void reload(false);
    } finally {
      setPending(null);
    }
  }

  // Hides the recording view. The tray's ad-hoc offer it replaced is skipped, so it does not come back after the
  // recording; the core hides the window then, and hideWindow covers the rest.
  function hideRecording() {
    if (view !== null && view.kind === "adhoc" && view.state === "OFFERED") {
      void api.skip(view.id).catch(() => undefined);
    }
    hideWindow().catch(() => undefined);
  }

  async function stop(recordingId: string) {
    if (stopping) {
      return;
    }
    setStopping(true);
    setStopProblem("");
    try {
      await activity.stopRecording(recordingId);
      hideRecording();
    } catch {
      setStopProblem(recordingTexts.stopFailed);
    } finally {
      setStopping(false);
      refreshSound();
    }
  }

  function click(id: ButtonId) {
    if (id === "close") {
      hideWindow().catch(() => undefined);
      return;
    }
    void act(id);
  }

  function choose(app: string) {
    setSelection(chooseSource(app));
    setListOpen(false);
  }

  if (!loaded) {
    return null;
  }

  if (advice !== null) {
    const am = adviceModel(advice);
    return (
      <div className="popup" ref={popupRef}>
        <IconSprite />
        <div className="p-top live">
          <span className="dot" aria-hidden="true" />
          <span className="heading">{adviceHeading}</span>
        </div>
        <div className="texts">
          <Banner kind="wait" role="status">
            <span title={am.hint}>{am.text}</span>
          </Banner>
          {adviceProblem !== "" && <p className="warning">{adviceProblem}</p>}
        </div>
        <div className="pbtns">
          <Button
            variant="primary"
            className="wide"
            disabled={advicePending !== null}
            title={adviceHints.switch}
            onClick={() => void adviceAct("switch")}
          >
            {advicePending === "switch" ? adviceLabels.switching : adviceLabels.switch}
          </Button>
          <Button disabled={advicePending !== null} title={adviceHints.keep} onClick={() => void adviceAct("dismiss")}>
            {adviceLabels.keep}
          </Button>
        </div>
      </div>
    );
  }

  if (live && sound !== null) {
    const quietHint = sound.quietHint && !sound.heard;
    return (
      <div className="popup" ref={popupRef}>
        <IconSprite />
        <div className="p-top live">
          <span className="dot" aria-hidden="true" />
          <span className="heading">{recordingTexts.heading}</span>
        </div>
        <div className="p-sound">
          <SoundLevel sound={sound} large />
        </div>
        <div className="texts">
          {/* The calm hint replaces the tip: no alarm colour (the core's silent-source advice comes later). */}
          <p className="p-note">{quietHint ? soundTexts.hint : recordingTexts.tip}</p>
          {stopProblem !== "" && <p className="warning">{stopProblem}</p>}
        </div>
        <div className="pbtns two">
          <Button
            variant="danger"
            icon="stop"
            disabled={stopping}
            title={recordingTexts.stopHint}
            onClick={() => void stop(sound.recordingId)}
          >
            {stopping ? recordingTexts.stopping : recordingTexts.stop}
          </Button>
          <Button title={recordingTexts.hideHint} onClick={hideRecording}>
            {recordingTexts.hide}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="popup" ref={popupRef}>
      <IconSprite />
      <div className={showPicker ? "p-top live" : "p-top"}>
        {showPicker && <span className="dot" aria-hidden="true" />}
        <span className="heading">{m.heading}</span>
        {m.time !== "" && <span className="time num">{m.time}</span>}
      </div>
      {m.title !== "" && (
        <h1 className="title" title={m.title}>
          {m.title}
        </h1>
      )}
      {m.note !== "" && <p className="p-note">{m.note}</p>}
      {showPicker && (
        <SourcePicker
          picker={picker}
          disabled={m.disabled}
          open={listOpen}
          onToggle={() => setListOpen((open) => !open)}
          onClose={() => setListOpen(false)}
          onChoose={choose}
        />
      )}
      {(showPicker || errorLine !== "") && (
        // With an error the window grows to show it in full; past its max height (or without an error) this area
        // shrinks and, as a last resort, scrolls so that the buttons are never cut off.
        <div className="texts">
          {errorLine !== "" && (
            <Banner kind="fail" role="alert">
              <span className={layout.clamp ? "clamp" : undefined} title={layout.clamp ? errorLine : undefined}>
                {errorLine}
              </span>
            </Banner>
          )}
          {showPicker && picker.autoNote !== "" && <p className="auto-note">{picker.autoNote}</p>}
          {showPicker &&
            picker.warnings.map((w) => (
              <p key={w} className={layout.clamp ? "warning clamp" : "warning"} title={layout.clamp ? w : undefined}>
                {w}
              </p>
            ))}
          {showPicker && (
            <p
              className={layout.collapsed ? "advice collapsed" : "advice"}
              title={layout.collapsed ? picker.advice : undefined}
            >
              {picker.advice}
            </p>
          )}
        </div>
      )}
      <div className="pbtns">
        {m.buttons.map((id) => (
          <Button
            key={id}
            variant={id === "start" ? "primary" : id === "skip" ? "ghost" : "secondary"}
            icon={id === "start" ? "rec" : undefined}
            className={id === "start" || id === "close" ? "wide" : undefined}
            disabled={m.disabled}
            // The title is also the accessible description of the button (the label stays its name).
            title={buttonHints[id]}
            onClick={() => click(id)}
          >
            {id === "start" ? m.startLabel : buttonLabels[id]}
          </Button>
        ))}
      </div>
    </div>
  );
}
