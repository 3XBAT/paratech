import { type ChangeEvent, type FocusEvent, type KeyboardEvent, type RefObject, type SyntheticEvent, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { MentionView, MentionsView } from "../bindings/agent.js";
import {
  activeFragment,
  addMention,
  backspaceRemovesPill,
  clampActive,
  createMentionSearch,
  createSlowGate,
  idleSearch,
  insertTrigger,
  isSettledLine,
  maxMentions,
  mentionAddedText,
  mentionRemovedText,
  moveActive,
  pickerKey,
  type PopupModel,
  popupModel,
  popupVisible,
  removeFragment,
  type SearchState,
} from "../agentMentions.js";

export interface MentionComposerDeps {
  /** The field takes requests; while it does not (offline, read-only) the list never opens. */
  enabled: boolean;
  draft: string;
  setDraft(text: string): void;
  input: RefObject<HTMLTextAreaElement | null>;
  search(query: string): Promise<MentionsView>;
  /** Changes with the open chat: the list closes. */
  resetKey: string;
}

/** What the field and the view need of the «@» mentions; the state machines are in agentMentions.ts. */
export interface MentionComposer {
  /** The chosen mentions of the request being written. */
  pills: MentionView[];
  /** The list as drawn; null while closed or while a fast search has nothing to show yet. */
  popup: PopupModel | null;
  active: number;
  listId: string;
  /** Id of the active option, for aria-activedescendant. */
  activeId: string | undefined;
  /** The text of the always-mounted live region: the last pill change or the last status line of the list. */
  announcement: string;
  onChange(e: ChangeEvent<HTMLTextAreaElement>): void;
  /** True when the key was used by the list or the pills (the caller then does nothing else). */
  onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>): boolean;
  /** Key up, click and select of the field: where the caret is now. */
  onCaret(e: SyntheticEvent<HTMLTextAreaElement>): void;
  onFocus(e: FocusEvent<HTMLElement>): void;
  onBlur(e: FocusEvent<HTMLElement>): void;
  pick(index: number): void;
  hover(index: number): void;
  remove(index: number): void;
  retry(): void;
  /** The «@» button: puts an «@» at the caret, focuses the field and opens the list. */
  openList(): void;
  /** After a send: the pills go. */
  clear(): void;
}

export function useMentionComposer(d: MentionComposerDeps): MentionComposer {
  const [pills, setPills] = useState<MentionView[]>([]);
  const [caret, setCaret] = useState(0);
  const [focused, setFocused] = useState(false);
  // The «@» an Esc closed: its list stays closed until the fragment goes away.
  const [dismissed, setDismissed] = useState(-1);
  const [active, setActive] = useState(0);
  const [search, setSearch] = useState<SearchState>(idleSearch);
  const [slow, setSlow] = useState(false);
  const [announcement, setAnnouncement] = useState("");
  const listId = useId();

  const fragment = d.enabled && focused ? activeFragment(d.draft, caret) : null;
  const open = fragment !== null && fragment.start !== dismissed;
  const full = pills.length >= maxMentions;
  // `model` drives the keys even while it is not drawn (a search still pending must not let Enter send the request).
  // The last settled line (empty / unavailable / failed) stays drawn while the next search has not earned «Поиск…».
  const settled = useRef<PopupModel | null>(null);
  const model = open ? popupModel(search, pills, slow, settled.current) : null;
  if (model === null || (model.kind !== "loading" && !isSettledLine(model))) {
    settled.current = null;
  } else if (isSettledLine(model)) {
    settled.current = model;
  }
  const popup = popupVisible(model) ? model : null;
  const count = model?.options.length ?? 0;
  const current = clampActive(active, count);
  const query = fragment?.query ?? "";

  const callRef = useRef(d.search);
  callRef.current = d.search;
  const searcher = useMemo(
    () =>
      createMentionSearch({
        call: (q) => callRef.current(q),
        setTimer: (fn, ms) => setTimeout(fn, ms),
        clearTimer: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
        onState: (s) => {
          setSearch(s);
          setActive(0);
        },
      }),
    [],
  );
  useEffect(() => () => searcher.cancel(), [searcher]);

  // A full row of pills needs no search: the list only says so.
  const searching = open && !full;
  useEffect(() => {
    if (searching) {
      searcher.request(query);
    } else {
      searcher.cancel();
    }
  }, [searching, query, searcher]);

  // «Поиск…» only for a search that takes long.
  const gate = useMemo(
    () =>
      createSlowGate({
        setTimer: (fn, ms) => setTimeout(fn, ms),
        clearTimer: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
        onChange: setSlow,
      }),
    [],
  );
  useEffect(() => () => gate.dispose(), [gate]);
  const running = searching && search.status === "loading";
  useEffect(() => {
    gate.set(running);
  }, [gate, running]);

  // The status line of the list goes to the live region too; the region itself stays mounted, so it is announced.
  const line = popup?.text ?? "";
  const lastLine = useRef("");
  useEffect(() => {
    if (line !== "") {
      lastLine.current = line;
      setAnnouncement(line);
    } else {
      // The line is gone: forget it, so the same line later (a second failure) is announced again.
      setAnnouncement((cur) => (cur === lastLine.current ? "" : cur));
    }
  }, [line]);

  const noFragment = fragment === null;
  useEffect(() => {
    if (noFragment) {
      setDismissed(-1);
    }
  }, [noFragment]);

  const fragmentRef = useRef(fragment);
  fragmentRef.current = fragment;
  // A switch of chat closes the list (the draft and its pills stay with the field).
  useEffect(() => {
    setDismissed(fragmentRef.current?.start ?? -1);
  }, [d.resetKey]);

  // The caret goes where the removed fragment was once the new text is in the field.
  const caretTo = useRef<number | null>(null);
  useLayoutEffect(() => {
    const to = caretTo.current;
    const el = d.input.current;
    if (to !== null && el) {
      caretTo.current = null;
      el.setSelectionRange(to, to);
    }
  }, [d.draft, d.input]);

  const { draft, setDraft } = d;
  const pick = useCallback(
    (index: number) => {
      const o = model?.options[index];
      if (!o || fragment === null) {
        return;
      }
      const r = addMention(pills, o);
      if (r.outcome !== "added") {
        // Already among the pills: the list only closes, the typed «@query» stays.
        setDismissed(fragment.start);
        return;
      }
      setPills(r.pills);
      setAnnouncement(mentionAddedText(o.label));
      const next = removeFragment(draft, fragment);
      caretTo.current = next.caret;
      setCaret(next.caret);
      setDraft(next.text);
    },
    [model, fragment, pills, draft, setDraft],
  );

  const input = d.input;
  const remove = useCallback(
    (index: number) => {
      const gone = pills[index];
      if (gone) {
        setAnnouncement(mentionRemovedText(gone.label));
      }
      setPills((p) => p.filter((_, i) => i !== index));
      input.current?.focus();
    },
    [pills, input],
  );

  const openList = useCallback(() => {
    const el = input.current;
    if (!el || !d.enabled || full) {
      return;
    }
    // Focus first: its focus handler reads the old caret, the new one is set after it.
    el.focus();
    const next = insertTrigger(draft, el.selectionEnd ?? draft.length);
    if (next === null) {
      return;
    }
    caretTo.current = next.caret;
    setCaret(next.caret);
    setDismissed(-1);
    setDraft(next.text);
  }, [input, d.enabled, full, draft, setDraft]);

  const retry = useCallback(() => searcher.retry(), [searcher]);
  const clear = useCallback(() => setPills([]), []);

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    const composing = e.nativeEvent.isComposing;
    if (model !== null && fragment !== null) {
      const k = pickerKey({ key: e.key, shiftKey: e.shiftKey, ctrlKey: e.ctrlKey, altKey: e.altKey, metaKey: e.metaKey, isComposing: composing, keyCode: e.keyCode }, count, model.kind);
      switch (k) {
        case "next":
        case "prev":
          e.preventDefault();
          setActive(moveActive(current, count, k));
          return true;
        case "pick":
          e.preventDefault();
          pick(current);
          return true;
        case "close":
          // Only the list closes: not the draft, not the panel. Enter with nothing to pick does the same, so a second Enter sends.
          e.preventDefault();
          e.stopPropagation();
          setDismissed(fragment.start);
          return true;
        case "swallow":
          // Enter must not send the request while the list searches; after a failure it repeats the search.
          e.preventDefault();
          if (model.retry) {
            searcher.retry();
          }
          return true;
        case "none":
          break;
      }
    }
    if (e.key === "Backspace" && !composing && e.keyCode !== 229) {
      if (backspaceRemovesPill(d.draft, pills.length)) {
        e.preventDefault();
        setAnnouncement(mentionRemovedText(pills[pills.length - 1].label));
        setPills((p) => p.slice(0, -1));
        return true;
      }
    }
    return false;
  };

  return {
    pills,
    popup,
    active: current,
    listId,
    activeId: popup !== null && count > 0 ? `${listId}-${current}` : undefined,
    announcement,
    onChange: (e) => {
      setCaret(e.target.selectionStart);
      d.setDraft(e.target.value);
    },
    onKeyDown,
    onCaret: (e) => setCaret(e.currentTarget.selectionStart),
    onFocus: (e) => {
      setFocused(true);
      if (e.target instanceof HTMLTextAreaElement) {
        setCaret(e.target.selectionStart);
      }
    },
    onBlur: (e) => {
      // Focus moving inside the field's block (a pill, the list) keeps the list.
      if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
        setFocused(false);
      }
    },
    pick,
    hover: setActive,
    remove,
    retry,
    openList,
    clear,
  };
}
