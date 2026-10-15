import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { promptApi, shownEvent, type PromptView } from "../bindings/prompt";
import { callByName, hideWindow, onEvent } from "../bindings/runtime";
import { actionErrorText, buttonLabels, loadErrorText, popupModel, type Action, type ButtonId } from "./view";

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

  useEffect(() => {
    let alive = true;
    let off: (() => void) | undefined;
    onEvent(shownEvent, () => void reload(true)).then(
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
    return () => {
      alive = false;
      off?.();
    };
  }, [reload]);

  async function act(action: Action) {
    if (view === null || pending !== null) {
      return;
    }
    setPending(action);
    setError("");
    const seq = ++latest.current;
    try {
      const v = await api[action](view.id);
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

  if (!loaded) {
    return null;
  }

  const m = popupModel(view, pending);
  const errorLine = error || m.problem;
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
      {errorLine !== "" && (
        <div className="error" role="alert">
          {errorLine}
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
