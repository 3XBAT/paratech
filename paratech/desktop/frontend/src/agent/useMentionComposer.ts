import { type ChangeEvent, type FocusEvent, type KeyboardEvent, type RefObject, type SyntheticEvent, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { MentionView, MentionsView } from "../bindings/agent.js";
import {
  activeFragment,
  addMention,
  backspaceRemovesPill,
  clampActive,
  createMentionSearch,
  idleSearch,
  maxMentions,
  moveActive,
  pickerKey,
  type PopupModel,
  popupModel,
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
  /** The list; null while closed. */
  popup: PopupModel | null;
  active: number;
  listId: string;
  /** Id of the active option, for aria-activedescendant. */
  activeId: string | undefined;
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
  const listId = useId();

  const fragment = d.enabled && focused ? activeFragment(d.draft, caret) : null;
  const open = fragment !== null && fragment.start !== dismissed;
  const full = pills.length >= maxMentions;
  const popup = open ? popupModel(search, pills) : null;
  const count = popup?.options.length ?? 0;
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

  const pick = useCallback(
    (index: number) => {
      const o = popup?.options[index];
      if (!o || fragment === null) {
        return;
      }
      const r = addMention(pills, o);
      if (r.outcome === "added") {
        setPills(r.pills);
      }
      // A repeated object adds nothing; the fragment goes either way and the list closes.
      const next = removeFragment(d.draft, fragment);
      caretTo.current = next.caret;
      setCaret(next.caret);
      d.setDraft(next.text);
    },
    [popup, fragment, pills, d],
  );

  const remove = useCallback(
    (index: number) => {
      setPills((p) => p.filter((_, i) => i !== index));
      d.input.current?.focus();
    },
    [d.input],
  );

  const retry = useCallback(() => searcher.retry(), [searcher]);
  const clear = useCallback(() => setPills([]), []);

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    const composing = e.nativeEvent.isComposing;
    if (popup !== null && fragment !== null) {
      const k = pickerKey({ key: e.key, shiftKey: e.shiftKey, ctrlKey: e.ctrlKey, altKey: e.altKey, metaKey: e.metaKey, isComposing: composing, keyCode: e.keyCode }, count);
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
          // Only the list closes: not the draft, not the panel.
          e.preventDefault();
          e.stopPropagation();
          setDismissed(fragment.start);
          return true;
        case "swallow":
          // Enter must not send the request while the list is open; after a failure it repeats the search.
          e.preventDefault();
          if (popup.retry) {
            searcher.retry();
          }
          return true;
        case "none":
          break;
      }
    }
    if (e.key === "Backspace" && !composing && e.keyCode !== 229) {
      const el = e.currentTarget;
      if (backspaceRemovesPill({ start: el.selectionStart, end: el.selectionEnd }, pills.length)) {
        e.preventDefault();
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
    clear,
  };
}
