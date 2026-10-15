import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { promptApi, shownEvent, type PromptView, type SourcesView } from "../bindings/prompt";
import { callByName, hideWindow, onEvent } from "../bindings/runtime";
import {
  actionErrorText,
  buttonLabels,
  chooseSource,
  collapseAdvice,
  initialSelection,
  loadErrorText,
  nextSelection,
  pickerModel,
  popupModel,
  shouldRefresh,
  sourceMarks,
  SourcesPoller,
  type Action,
  type ButtonId,
  type Selection,
  type SourceOption,
} from "./view";

const browserTimers = {
  set: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clear: (handle: unknown) => window.clearTimeout(handle as number),
};

// The popup shows the prompt the Go core holds and passes the clicks back; the core hides the window after
// start, snooze and skip. No element takes focus on its own: the popup must not steal it (AC-01).
export function Popup() {
  const api = useMemo(() => promptApi(callByName), []);
  const [view, setView] = useState<PromptView | null>(null);
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

  const reload = useCallback(
    async (clearError: boolean) => {
      const seq = ++latest.current;
      try {
        const v = await api.current();
        if (seq === latest.current) {
          setView(v);
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
      s = { apps: [], selected: "", remembered: "", listError: true };
    }
    if (forPrompt === promptIdRef.current) {
      setSources(s);
      setSelection((prev) => nextSelection(prev, s));
    }
  }, [api]);

  const poller = useMemo(() => new SourcesPoller(loadSources, browserTimers), [loadSources]);

  useEffect(() => {
    let alive = true;
    let off: (() => void) | undefined;
    onEvent(shownEvent, () => {
      // The core has just shown the window, whatever the page's visibility state says yet.
      setVisible(true);
      void reload(true);
      poller.refreshNow();
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
  }, [reload, poller]);

  // A new prompt starts with a fresh picker: no choice carries over from the previous offer.
  useEffect(() => {
    promptIdRef.current = promptId;
    setSources(null);
    setSelection(initialSelection);
    setListOpen(false);
    poller.refreshNow();
  }, [promptId, poller]);

  const refresh = shouldRefresh(view, visible, pending, listOpen);
  useEffect(() => {
    poller.setActive(refresh);
  }, [refresh, poller]);

  const m = popupModel(view, pending);
  const picker = pickerModel(sources, selection);
  const showPicker = m.buttons.includes("start");
  const listShown = listOpen && showPicker && !m.disabled;

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

  const errorLine = error || m.problem;
  const collapsed = collapseAdvice(showPicker ? picker.warnings : [], errorLine);
  return (
    <div className="popup">
      <div className="head">
        <span className="heading">{m.heading}</span>
        {m.time !== "" && <span className="time">{m.time}</span>}
      </div>
      {m.title !== "" && (
        <div className="title" title={m.title}>
          {m.title}
        </div>
      )}
      {m.note !== "" && <div className="note">{m.note}</div>}
      {showPicker && (
        <div className="picker">
          <button
            type="button"
            className="source chosen"
            disabled={m.disabled}
            aria-haspopup="listbox"
            aria-expanded={listShown}
            title={picker.chosen.hint}
            onClick={() => setListOpen((open) => !open)}
          >
            <SourceRow option={picker.chosen} />
            <span className="caret" aria-hidden="true">
              ▾
            </span>
          </button>
          {listShown && (
            <>
              <div className="backdrop" onClick={() => setListOpen(false)} />
              <ul className="list" role="listbox">
                {picker.options.map((o) => (
                  <li key={o.app} role="option" aria-selected={o.app === picker.chosen.app}>
                    <button
                      type="button"
                      className={o.app === picker.chosen.app ? "source current" : "source"}
                      title={o.hint}
                      onClick={() => choose(o.app)}
                    >
                      <SourceRow option={o} />
                    </button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      )}
      {(showPicker || errorLine !== "") && (
        // Shrinks and, as a last resort, scrolls so that the buttons are never cut off.
        <div className="texts">
          {showPicker &&
            picker.warnings.map((w) => (
              <div key={w} className="warning" title={w}>
                {w}
              </div>
            ))}
          {showPicker && (
            <div className={collapsed ? "advice collapsed" : "advice"} title={collapsed ? picker.advice : undefined}>
              {picker.advice}
            </div>
          )}
          {errorLine !== "" && (
            <div className="error" role="alert" title={errorLine}>
              {errorLine}
            </div>
          )}
        </div>
      )}
      <div className="buttons">
        {m.buttons.map((id) => (
          <button
            key={id}
            type="button"
            className={id === "start" ? "primary" : ""}
            disabled={m.disabled}
            onClick={() => click(id)}
          >
            {id === "start" ? m.startLabel : buttonLabels[id]}
          </button>
        ))}
      </div>
    </div>
  );
}

// SourceRow renders one source as text: names and titles are untrusted, and the icon is only a validated PNG data URL.
function SourceRow({ option }: { option: SourceOption }) {
  return (
    <>
      {option.icon !== "" ? (
        <img className="icon" src={option.icon} alt="" draggable={false} />
      ) : (
        <span className={option.app === "" ? "icon letter whole" : "icon letter"} aria-hidden="true">
          {option.letter}
        </span>
      )}
      <span className="name">{option.name}</span>
      {option.gone && <span className="mark gone">{sourceMarks.gone}</span>}
      {option.muted && <span className="mark muted">{sourceMarks.muted}</span>}
      {option.playing && <span className="mark playing">{sourceMarks.playing}</span>}
    </>
  );
}
