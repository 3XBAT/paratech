import { type RefObject, useCallback, useEffect, useReducer, useRef, useState } from "react";
import { type AgentApi, agentApi, type AskInput, type CallProblem, draftEvent as draftEventName, parseDraft, parseUpdate, updateEvent, type UpdateView } from "../bindings/agent.js";
import { agentConfirmApi } from "../bindings/agentConfirm.js";
import { libraryApi } from "../bindings/library.js";
import { turnkeyApi } from "../bindings/turnkey.js";
import { recordingDeleteApi } from "../bindings/recordingDelete.js";
import { callByName, onEventData } from "../bindings/runtime.js";
import { askInput, callProblemText, contextFor, entryModel, isFocusPanelKey, panelShortcut, requestText, type UndoNote, undoableActions } from "../agentView.js";
import { draftEvent, draftText } from "../agentDraft.js";
import { problemText, undoOutcome } from "../journalView.js";
import { blockingEntry, type Entry, initialPanelState, PressTracker, reducePanel } from "../agentState.js";
import type { PageId } from "../nav.js";
import type { ButtonId } from "../agentView.js";
import { AgentPanelView } from "./AgentPanelView.js";
import type { CardActions } from "./AgentCards.js";

const api: AgentApi = agentApi(callByName);
const turnkey = turnkeyApi(callByName);
const library = libraryApi(callByName);
const confirm = { api: agentConfirmApi(callByName), deleteApi: recordingDeleteApi(callByName) };

/** Ctrl+K from anywhere in the window: a closed panel opens with focus in the input, an open one closes. */
function useFocusShortcut(input: RefObject<HTMLTextAreaElement | null>, open: boolean, reveal?: () => void, hide?: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!isFocusPanelKey(e)) {
        return;
      }
      e.preventDefault();
      if (panelShortcut(open) === "close") {
        // Focus goes back to the page rather than into the hidden panel.
        const active = document.activeElement;
        if (active instanceof HTMLElement && active.closest(".ag") !== null) {
          active.blur();
        }
        hide?.();
        return;
      }
      // A closed panel opens first; the input can take focus once it is shown again.
      reveal?.();
      setTimeout(() => input.current?.focus(), 0);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [input, open, reveal, hide]);
}

/**
 * The agent panel: talks to the core through the bindings only. The Hub is the core's business; the panel holds no
 * keys and keeps nothing in browser storage.
 */
export function AgentPanel(props: {
  page: PageId;
  assignmentId: string;
  open?: boolean;
  reveal?: () => void;
  hide?: () => void;
  onOpenAssignment?: (id: string) => void;
}) {
  const [state, dispatch] = useReducer(reducePanel, initialPanelState);
  // Subject names for the assignment lists of replies; unreadable subjects just leave the names out.
  const [subjects, setSubjects] = useState<Record<string, string>>({});
  useEffect(() => {
    let alive = true;
    library.subjects().then(
      (list) => alive && setSubjects(Object.fromEntries(list.map((s) => [s.id, s.name]))),
      () => undefined,
    );
    return () => {
      alive = false;
    };
  }, [props.page]);
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  // The context chip is removed per section: a new section brings its own label back.
  const [removedOn, setRemovedOn] = useState<PageId | null>(null);
  const ctx = contextFor(props.page, props.assignmentId, removedOn === props.page);
  const input = useRef<HTMLTextAreaElement>(null);
  const askId = useRef(0);
  useFocusShortcut(input, props.open ?? true, props.reveal, props.hide);

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

  useEffect(() => {
    let off: (() => void) | undefined;
    let closed = false;
    onEventData(draftEventName, (data) => {
      try {
        dispatch({ type: "draft", draft: parseDraft(data) });
      } catch {
        // A draft the panel cannot read is dropped: the verified answer card follows anyway.
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

  const send = useCallback(
    (ask: AskInput) => {
      const id = ++askId.current;
      dispatch({ type: "asked", id, text: ask.text, scope: ask });
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
    },
    [loadState],
  );

  const submit = useCallback(() => {
    const text = requestText(draft);
    if (text === null) {
      return;
    }
    setDraft("");
    send(askInput(ctx, text));
  }, [draft, ctx, send]);

  // Requests whose buttons were pressed, with the update they were pressed on: off until a newer update arrives.
  // The tracker is read synchronously, so a double click before the next render makes one call.
  const pressed = useRef(new PressTracker());
  const [, redraw] = useReducer((n: number) => n + 1, 0);

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
    const named = e.threadId !== "";
    // A request the core has not named yet has no buttons of its own: one refused as busy stops the running request
    // that blocks it, or is sent again.
    const blocking = named ? undefined : blockingEntry(state.entries, e);
    // Buttons are off while a call runs; the refused request's «Стоп» is guarded on the blocker, so it shows that mark.
    const busy = named ? pressed.current.busy(e.key, e.update) : blocking ? pressed.current.busy(blocking.key, blocking.update) : false;
    // `target`: the request whose update frees the buttons (default: this one). `onSuccess`: also free them when the
    // call is accepted. A call the core refused produces no update, so the buttons come back at once.
    const run = (
      make: () => Promise<CallProblem>,
      o: { key?: string; update?: UpdateView | null; onSuccess?: boolean; onRefused?: () => void } = {},
    ) => {
      const key = o.key ?? e.key;
      if (!pressed.current.mark(key, o.update === undefined ? e.update : o.update)) {
        return;
      }
      redraw();
      const release = () => {
        pressed.current.release(key);
        redraw();
      };
      const refused = () => {
        o.onRefused?.();
        release();
      };
      void make().then(
        (problem) => {
          setNotice(callProblemText(problem));
          if (problem !== "") {
            refused();
          } else if (o.onSuccess) {
            release();
          }
          if (problem === "offline") {
            void loadState();
          }
        },
        () => {
          setNotice(callProblemText("failed"));
          refused();
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
      rerun: () => api.rerun(ref),
      revise: () => Promise.resolve("invalid"), // Handled by the plan card, which asks for the wish first.
      resend: () => Promise.resolve("invalid"), // Handled below: it asks again, not a call on this request.
    };
    const press = (id: ButtonId) => {
      if (!named) {
        if (id === "stop" && blocking) {
          // Marked on the blocker, so both «Стоп» (its own and this one) and a double click make one call; the mark
          // goes with the blocker's next update.
          run(() => api.stop({ threadId: blocking.threadId, turn: blocking.turn }), { key: blocking.key, update: blocking.update });
        } else if (id === "resend" && e.scope !== null && e.text !== "" && pressed.current.mark(e.key, e.update)) {
          redraw();
          send(e.scope);
        }
        return;
      }
      if (id === "rerun") {
        // The new request shows at once with the same text; taken back if the call is refused.
        const next = `thread:${e.threadId}:${e.turn + 1}`;
        run(
          () => {
            dispatch({ type: "rerunAsked", from: { threadId: e.threadId, turn: e.turn, text: e.text, scope: e.scope } });
            return api.rerun(ref);
          },
          { onSuccess: true, onRefused: () => dispatch({ type: "rerunRefused", key: next }) },
        );
        return;
      }
      run(calls[id]);
    };
    return {
      busy,
      confirm,
      turnkey,
      subjects,
      openAssignment: props.onOpenAssignment,
      press,
      revise: (wish) => named && run(() => api.revisePlan(ref, wish)),
      answer: (answers) => named && run(() => api.answer(ref, answers)),
      undo: (actionId) => named && undoOne(actionId),
      undoAll: () => {
        const ids = undoableActions(entryModel(e).cards).filter((id) => !undoNotes[id]?.done && !undoNotes[id]?.busy);
        if (named && ids.length > 0) {
          undoAll(ref, ids);
        }
      },
      undoNote: (actionId) => undoNotes[actionId],
    };
  }, [state.entries, send, loadState, undoNotes, undoOne, undoAll, subjects, props.onOpenAssignment]);

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
