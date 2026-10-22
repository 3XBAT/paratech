import { type RefObject, useCallback, useEffect, useReducer, useRef, useState } from "react";
import { type AgentApi, agentApi, type CallProblem, parseUpdate, updateEvent } from "../bindings/agent.js";
import { callByName, onEventData } from "../bindings/runtime.js";
import { askInput, callProblemText, contextFor, isFocusPanelKey, requestText } from "../agentView.js";
import { type Entry, initialPanelState, reducePanel } from "../agentState.js";
import type { PageId } from "../nav.js";
import type { ButtonId } from "../agentView.js";
import { AgentPanelView } from "./AgentPanelView.js";
import type { CardActions } from "./AgentCards.js";

const api: AgentApi = agentApi(callByName);

/** Ctrl+K focuses the input of the panel from anywhere in the window. */
function useFocusShortcut(input: RefObject<HTMLInputElement | null>) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (isFocusPanelKey(e)) {
        e.preventDefault();
        input.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [input]);
}

/**
 * The agent panel: talks to the core through the bindings only. The Hub is the core's business; the panel holds no
 * keys and keeps nothing in browser storage.
 */
export function AgentPanel(props: { page: PageId; assignmentId: string }) {
  const [state, dispatch] = useReducer(reducePanel, initialPanelState);
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  // The context chip is removed per section: a new section brings its own label back.
  const [removedOn, setRemovedOn] = useState<PageId | null>(null);
  const ctx = contextFor(props.page, props.assignmentId, removedOn === props.page);
  const input = useRef<HTMLInputElement>(null);
  const askId = useRef(0);
  useFocusShortcut(input);

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

  useEffect(() => {
    const load = () =>
      api.state().then(
        (link) => dispatch({ type: "link", link }),
        // No answer reads as read-only: nothing is sent into the dark.
        () => dispatch({ type: "link", link: { link: "unknown", readOnly: true } }),
      );
    void load();
    window.addEventListener("focus", load);
    // The history of the thread «Без предмета»; it says "unavailable" until the core can read it, and then nothing is shown.
    api.history("").then(
      (h) => h.problem === "" && dispatch({ type: "history", requests: h.requests }),
      () => undefined,
    );
    return () => window.removeEventListener("focus", load);
  }, []);

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
      (problem) => dispatch({ type: "askResult", id, problem }),
      () => dispatch({ type: "askResult", id, problem: "failed" }),
    );
  }, [draft, ctx]);

  const actionsFor = useCallback((e: Entry): CardActions => {
    const ref = { threadId: e.threadId, turn: e.turn };
    const run = (p: Promise<CallProblem>) =>
      void p.then(
        (problem) => setNotice(callProblemText(problem)),
        () => setNotice(callProblemText("failed")),
      );
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
      press: (id) => named && run(calls[id]()),
      revise: (wish) => named && run(api.revisePlan(ref, wish)),
      answer: (answers) => named && run(api.answer(ref, answers)),
    };
  }, []);

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
