import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { activityApi, type AdviceView } from "../bindings/activity";
import { popupWindowApi, promptApi, shownEvent, type PromptView, type SourcesView } from "../bindings/prompt";
import { libraryApi } from "../bindings/library";
import { callByName, hideWindow, onEvent } from "../bindings/runtime";
import { callLinkTexts } from "../callLinkView";
import { Button } from "../ui/Button";
import { Banner } from "../ui/controls";
import { Icon, IconSprite } from "../ui/Icon";
import { SoundLevel } from "../ui/SoundLevel";
import { soundTexts } from "../soundLevelView";
import { useNow } from "../useNow";
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
  idleJoin,
  JoinController,
  joinOnOwnRow,
  joinPromptId,
  joinView,
  StartedRecording,
  startedPromptId,
  type JoinState,
} from "./joinView";
import {
  adviceHeading,
  buttonHints,
  buttonLabels,
  dotClass,
  elapsedText,
  HeightFitter,
  loadErrorText,
  popupModel,
  recordingInfo,
  recordingTexts,
  shouldRefresh,
  showsRecording,
  textsLayout,
  topClass,
  wantedHeight,
  type Action,
  type ButtonId,
  type RecordingInfo,
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
  // «Подключиться» (TASK-370): the state machine is in joinView.ts; the page only draws it. The meeting opens through the
  // library binding by subject ID, never by a URL from here.
  const [join, setJoin] = useState<JoinState>(idleJoin);
  const joinCtl = useMemo(() => {
    const library = libraryApi(callByName);
    return new JoinController({ callSubject: api.callSubject, open: library.openSubjectCallLink }, setJoin);
  }, [api]);
  // The prompt a recording was started from in this popup: the running view keeps asking the core for its call link
  // with this ID, although the prompt is no longer the current one. "" for a recording not started from here.
  const startedRec = useMemo(() => new StartedRecording(), []);
  const [runningFrom, setRunningFrom] = useState("");
  // Bumped when a started prompt is registered: the binding below re-runs even if the sound poll saw the recording
  // before the start reply came.
  const [startedSeq, setStartedSeq] = useState(0);
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
  // Title and start of the running recording: read once per recording shown, the timer ticks here from startedAt.
  const [info, setInfo] = useState<{ id: string; info: RecordingInfo | null } | null>(null);

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
      joinCtl.refresh();
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
    const onVisibility = () => {
      setVisible(document.visibilityState === "visible");
      // The popup closed: the meeting may be opened again when it is shown next. The same prompt shown again has no
      // new offer to read the subject, so the page reads it itself.
      if (document.visibilityState === "hidden") {
        joinCtl.closed();
      } else {
        joinCtl.refresh();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      alive = false;
      off?.();
      document.removeEventListener("visibilitychange", onVisibility);
      poller.setActive(false);
    };
  }, [reload, poller, fitter, refreshSound, joinCtl]);

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
  const live = sound !== null && advice === null && showsRecording(view, sound.recording, pending);
  const liveId = live && sound !== null ? sound.recordingId : "";
  // While the recording runs «Подключиться» is not blocked by a start; the hover text and the notes differ (joinView).
  const jv = joinView(join, live ? false : m.disabled, live);
  const joinShown = showPicker && jv.visible;
  // Only start problems are red. The join texts are quiet notes (opened, gone) or the amber failure line, all in the
  // texts area, so they count in its layout but never grow the window.
  const errorLine = error || m.problem;
  const joinQuiet = showPicker ? [jv.confirm, jv.note].filter((n) => n !== "") : [];
  const joinFailure = showPicker ? jv.failure : "";
  const notes = showPicker
    ? [picker.autoNote, ...picker.warnings, ...joinQuiet, joinFailure].filter((n) => n !== "")
    : [];
  // Once opened, the join button and «Не записывать» each take a full row: one more row, so the advice shrinks sooner.
  const joinOwnRow = joinShown && joinOnOwnRow(jv);
  const layout = textsLayout(notes, errorLine, joinOwnRow);

  // Which recording captures now ("" none, null: the sound could not be read) ties the started prompt to its recording
  // and drops it when that one ends or another one is shown.
  const recordingNow = sound === null ? null : sound.recording ? sound.recordingId : "";
  useEffect(() => {
    setRunningFrom(startedRec.track(recordingNow));
  }, [startedRec, recordingNow, startedSeq]);

  // The button belongs to the offered lesson, or, in the running view, to the prompt the recording was started from.
  const offeredId = joinPromptId(view, advice !== null || live);
  const joinId = live ? (runningFrom === "" ? null : runningFrom) : offeredId;
  useEffect(() => {
    joinCtl.offer(joinId);
  }, [joinCtl, joinId]);

  useEffect(() => {
    if (liveId === "") {
      return;
    }
    let alive = true;
    // A failed read only leaves out the title and the timer.
    activity.listRecordings().then(
      (items) => {
        if (alive) {
          setInfo({ id: liveId, info: recordingInfo(items, liveId) });
        }
      },
      () => undefined,
    );
    return () => {
      alive = false;
    };
  }, [activity, liveId]);

  const liveInfo = info !== null && info.id === liveId ? info.info : null;
  const now = useNow(liveInfo !== null && liveInfo.startedAt !== "");

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
    joinCtl.clearProblem();
    const seq = ++latest.current;
    try {
      const v = action === "start" ? await api.start(view.id, picker.chosen.app) : await api[action](view.id);
      // A started recording keeps the prompt ID for «Подключиться» (even if a newer reply replaced the view meanwhile).
      const started = action === "start" ? startedPromptId(view, v) : "";
      if (started !== "") {
        startedRec.started(started);
        setStartedSeq((n) => n + 1);
      }
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
        <div className={topClass("advice")}>
          <span className={dotClass("advice")} aria-hidden="true" />
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
    const timer = liveInfo === null ? "" : elapsedText(liveInfo.startedAt, now);
    return (
      <div className="popup" ref={popupRef}>
        <IconSprite />
        <div className={topClass("recording")}>
          <span className={dotClass("recording")} aria-hidden="true" />
          <span className="heading">{recordingTexts.heading}</span>
          {timer !== "" && <span className="time num">{timer}</span>}
        </div>
        {liveInfo !== null && (
          <h1 className="title" title={liveInfo.title}>
            {liveInfo.title}
          </h1>
        )}
        <div className="p-sound">
          <SoundLevel sound={sound} large />
        </div>
        <div className="texts">
          {/* The calm hint replaces the tip: no alarm colour (the core's silent-source advice comes later). */}
          {quietHint ? (
            <p className="p-note soft">
              <Icon name="info" />
              <span>{soundTexts.hint}</span>
            </p>
          ) : (
            <p className="p-note">{recordingTexts.tip}</p>
          )}
          {stopProblem !== "" && <p className="warning">{stopProblem}</p>}
          {/* The confirmation («Ссылка открыта.») or the link is gone note, and the failure line of «Подключиться»: live
              regions drawn before their text. The note and the confirmation never exist together. */}
          {jv.live && (
            <p className="p-note join-note" role="status">
              {jv.note || jv.confirm}
            </p>
          )}
          {jv.live && (
            <p className="warning join-fail" role="status">
              {jv.failure}
            </p>
          )}
        </div>
        <div className="pbtns two">
          <Button
            className="stop"
            icon="stop"
            busy={stopping}
            busyLabel={recordingTexts.stopping}
            disabled={stopping}
            title={recordingTexts.stopHint}
            onClick={() => void stop(sound.recordingId)}
          >
            {recordingTexts.stop}
          </Button>
          <Button title={recordingTexts.hideHint} onClick={hideRecording}>
            {recordingTexts.hide}
          </Button>
          {jv.visible && (
            // At most one extra row, the width of the popup: «Подключиться», or «Открыть ещё раз» once it was opened.
            <Button
              className="wide"
              busy={jv.busy}
              busyLabel={callLinkTexts.joining}
              disabled={jv.disabled}
              title={jv.title}
              onClick={() => void joinCtl.click()}
            >
              {jv.label}
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="popup" ref={popupRef}>
      <IconSprite />
      <div className={topClass("offer")}>
        {showPicker && <span className={dotClass("offer")} aria-hidden="true" />}
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
          {showPicker && jv.live && (
            // A live region drawn before its text (the confirmation or the gone note) so that it is announced; empty, it
            // takes no room.
            <p
              className={layout.clamp ? "p-note join-note clamp" : "p-note join-note"}
              role="status"
              title={layout.clamp && joinQuiet.length > 0 ? joinQuiet[0] : undefined}
            >
              {joinQuiet[0]}
            </p>
          )}
          {showPicker && (
            <p
              className={layout.collapsed ? "advice collapsed" : "advice"}
              title={layout.collapsed ? picker.advice : undefined}
            >
              {picker.advice}
            </p>
          )}
          {showPicker && jv.live && (
            // Last, right above the buttons; a status like the line above (drawn before its text).
            <p
              className={layout.clamp ? "warning join-fail clamp" : "warning join-fail"}
              role="status"
              title={layout.clamp && joinFailure !== "" ? joinFailure : undefined}
            >
              {joinFailure}
            </p>
          )}
        </div>
      )}
      <div className="pbtns">
        {m.buttons.map((id) => (
          <Fragment key={id}>
            <Button
              variant={id === "start" ? "primary" : id === "skip" ? "ghost" : "secondary"}
              icon={id === "start" ? "rec" : undefined}
              className={id === "start" || id === "close" || (id === "skip" && joinOwnRow) ? "wide" : undefined}
              disabled={m.disabled}
              // The title is also the accessible description of the button (the label stays its name).
              title={buttonHints[id]}
              onClick={() => click(id)}
            >
              {id === "start" ? m.startLabel : buttonLabels[id]}
            </Button>
            {id === "start" && joinShown && (
              // Between «Начать запись» and «Не записывать»: «Подключиться» shares a row with the skip button, «Открыть
              // ещё раз» is too long for a half row and takes its own. It never disables the start and takes no focus.
              <Button
                className={joinOwnRow ? "wide" : undefined}
                busy={jv.busy}
                busyLabel={callLinkTexts.joining}
                disabled={jv.disabled}
                title={jv.title}
                onClick={() => void joinCtl.click()}
              >
                {jv.label}
              </Button>
            )}
          </Fragment>
        ))}
      </div>
    </div>
  );
}
