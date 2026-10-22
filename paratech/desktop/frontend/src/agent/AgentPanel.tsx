import { type RefObject, useCallback, useEffect, useReducer, useRef, useState } from "react";
import { type AgentApi, agentApi, type CallProblem, parseUpdate, type UpdateView, updateEvent } from "../bindings/agent.js";
import { callByName, onEventData } from "../bindings/runtime.js";
import { askInput, callProblemText, contextFor, entryModel, isFocusPanelKey, requestText, type UndoNote, undoableActions } from "../agentView.js";
import { draftEvent, draftText } from "../agentDraft.js";
import { problemText, undoOutcome } from "../journalView.js";
import { type Entry, initialPanelState, reducePanel } from "../agentState.js";
import type { PageId } from "../nav.js";
import type { ButtonId } from "../agentView.js";
import { AgentPanelView } from "./AgentPanelView.js";
import type { CardActions } from "./AgentCards.js";

const api: AgentApi = agentApi(callByName);

/** Ctrl+K focuses the input of the panel from anywhere in the window. */
function useFocusShortcut(input: RefObject<HTMLInputElement | null>, reveal?: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (isFocusPanelKey(e)) {
        e.preventDefault();
        // A closed panel opens first; the input can take focus once it is shown again.
        reveal?.();
        setTimeout(() => input.current?.focus(), 0);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [input, reveal]);
}

/**
 * The agent panel: talks to the core through the bindings only. The Hub is the core's business; the panel holds no
 * keys and keeps nothing in browser storage.
 */
export function AgentPanel(props: { page: PageId; assignmentId: string; reveal?: () => void }) {
  const [state, dispatch] = useReducer(reducePanel, initialPanelState);
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  // The context chip is removed per section: a new section brings its own label back.
  const [removedOn, setRemovedOn] = useState<PageId | null>(null);
  const ctx = contextFor(props.page, props.assignmentId, removedOn === props.page);
  const input = useRef<HTMLInputElement>(null);
  const askId = useRef(0);
  useFocusShortcut(input, props.reveal);

  // A hint chip elsewhere in the window puts its text into the input; sending stays with the user.
  useEffect(() => {
    const onDraft = (e: Event) => {
      const text = draftText(e);
      if (text !== "") {
        setDraft(text.slice(0, 4000));
        // A closed panel opens first; the input can take focus once it is shown again.
        props.reveal?.();
        setTimeout(() => input.current?.focus(), 0);
      }
    };
    window.addEventListener(draftEvent, onDraft);
    return () => window.removeEventListener(draftEvent, onDraft);
  }, [props.reveal]);

  useEffect(() => {
    let off: (() => void) | undefined;
    let closed = false;
    onEventData(updateEvent, (data) => {
      try {
        dispatch({ type: "update", update: parseUpdate(data) });
      } catch {
        // An update the panel cannot read is ignored: the core validated it before it was sent.
      }
    }).then((stop) => {
      if (closed) {
        stop();
      } else {
        off = stop;
      }
    }, () => undefined);
    return () => {
      closed = true;
      off?.();
    };
  }, []);

  const loadState = useCallback(
    () =>
      api.state().then(
        (link) => dispatch({ type: "link", link }),
        // No answer reads as read-only: nothing is sent into the dark.
        () => dispatch({ type: "link", link: { link: "unknown", readOnly: true } }),
      ),
    [],
  );

  // State is re-read on window focus and when the section changes.
  useEffect(() => {
    void loadState();
  }, [loadState, props.page]);

  useEffect(() => {
    const load = () => void loadState();
    window.addEventListener("focus", load);
    return () => window.removeEventListener("focus", load);
  }, [loadState]);

  const submit = useCallback(() => {
    const text = requestText(draft);
    if (text === null) {
      return;
    }
    const id = ++askId.current;
    const ask = askInput(ctx, text);
    dispatch({ type: "asked", id, text, scope: ask });
    setDraft("");
    setNotice("");
    api.ask(ask).then(
      (problem) => {
        dispatch({ type: "askResult", id, problem });
        // An offline refusal may be stale: the Hub link decides, not the refusal.
        if (problem === "offline") {
          void loadState();
        }
      },
      () => dispatch({ type: "askResult", id, problem: "failed" }),
    );
  }, [draft, ctx, loadState]);

  // Requests whose buttons were pressed, with the update they were pressed on: off until a newer update arrives.
  const [pressed, setPressed] = useState<Record<string, UpdateView | null>>({});

  // What «Отменить» did, per action. The journal itself is «Журнал»; here only the lines of the thread.
  const [undoNotes, setUndoNotes] = useState<Record<string, UndoNote>>({});
  const undoNotesRef = useRef(undoNotes);
  undoNotesRef.current = undoNotes;
  const setNote = useCallback((id: string, note: UndoNote) => setUndoNotes((m) => ({ ...m, [id]: note })), []);

  const undoOne = useCallback(
    (actionId: string) => {
      const cur = undoNotesRef.current[actionId];
      if (cur?.busy || cur?.done) {
        return;
      }
      setNote(actionId, { busy: true, done: false, text: "" });
      api.undo(actionId).then(
        (v) => {
          const o = undoOutcome(v);
          setNote(actionId, { busy: false, done: o.ok, text: o.text });
        },
        () => setNote(actionId, { busy: false, done: false, text: undoOutcome({ problem: "failed", actionId, status: "", refusal: "" }).text }),
      );
    },
    [setNote],
  );

  const undoAll = useCallback(
    (ref: { threadId: string; turn: number }, ids: string[]) => {
      ids.forEach((id) => setNote(id, { busy: true, done: false, text: "" }));
      api.undoRequest(ref).then(
        (v) => {
          const outcomes = new Map(v.actions.map((a) => [a.actionId, undoOutcome({ ...a, problem: "" })]));
          const whole = problemText(v.problem);
          ids.forEach((id) => {
            const o = outcomes.get(id);
            setNote(id, { busy: false, done: o?.ok === true, text: o?.ok ? "" : (o?.text ?? whole) });
          });
          if (whole !== "") {
            setNotice(whole);
          }
        },
        () => {
          ids.forEach((id) => setNote(id, { busy: false, done: false, text: problemText("failed") }));
        },
      );
    },
    [setNote],
  );

  const actionsFor = useCallback((e: Entry): CardActions => {
    const ref = { threadId: e.threadId, turn: e.turn };
    const busy = e.key in pressed && pressed[e.key] === e.update;
    const run = (p: Promise<CallProblem>) => {
      setPressed((m) => ({ ...m, [e.key]: e.update }));
      // A call the core refused produces no update, so the buttons come back at once.
      const release = () => setPressed(({ [e.key]: _, ...rest }) => rest);
      void p.then(
        (problem) => {
          setNotice(callProblemText(problem));
          if (problem !== "") {
            release();
          }
          if (problem === "offline") {
            void loadState();
          }
        },
        () => {
          setNotice(callProblemText("failed"));
          release();
        },
      );
    };
    const calls: Record<ButtonId, () => Promise<CallProblem>> = {
      approve: () => api.approvePlan(ref),
      cancel: () => api.cancel(ref),
      stop: () => api.stop(ref),
      continue: () => api.continueTurn(ref),
      retry: () => api.retry(ref),
      restart: () => api.restart(ref),
      revise: () => Promise.resolve("invalid"), // Handled by the plan card, which asks for the wish first.
    };
    // A request the core has not named yet has no buttons to press.
    const named = e.threadId !== "";
    return {
      busy,
      press: (id) => named && !busy && run(calls[id]()),
      revise: (wish) => named && !busy && run(api.revisePlan(ref, wish)),
      answer: (answers) => named && !busy && run(api.answer(ref, answers)),
      undo: (actionId) => named && undoOne(actionId),
      undoAll: () => {
        const ids = undoableActions(entryModel(e).cards).filter((id) => !undoNotes[id]?.done && !undoNotes[id]?.busy);
        if (named && ids.length > 0) {
          undoAll(ref, ids);
        }
      },
      undoNote: (actionId) => undoNotes[actionId],
    };
  }, [pressed, loadState, undoNotes, undoOne, undoAll]);

  return (
    <AgentPanelView
      state={state}
      ctx={ctx}
      draft={draft}
      notice={notice}
      inputRef={input}
      onDraft={setDraft}
      onSubmit={submit}
      onRemoveContext={() => setRemovedOn(props.page)}
      actionsFor={actionsFor}
    />
  );
}
