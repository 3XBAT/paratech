import { type RefObject, useEffect, useLayoutEffect, useRef } from "react";
import type { MentionView } from "../bindings/agent.js";
import { maxDraftLength, maxMentions, mentionTexts } from "../agentMentions.js";
import { type AgentContext, chatTexts, composerKey, type EarlierItem, entryModel, inputPlacement, texts } from "../agentView.js";
import { blockingEntry, draftsFor, type Entry, orderedEntries, panelMode, type PanelState } from "../agentState.js";
import { Icon } from "../ui/Icon.js";
import { AgentRequest, type CardActions } from "./AgentCards.js";
import type { ContextFill } from "../bindings/agent.js";
import { ChatBar, type ChatBarProps } from "./ChatBar.js";
import { ContextHint, ContextRing } from "./ContextRing.js";
import { ComposerPills, MentionPopup, MessagePills } from "./MentionParts.js";
import type { MentionComposer } from "./useMentionComposer.js";

/** Earlier requests of the open chat: text only (the student's words, the mentions as pills, the reply line). */
function EarlierRequests(props: { items: EarlierItem[]; onOpenMention?: (kind: string, id: string) => void }) {
  if (props.items.length === 0) {
    return null;
  }
  return (
    <section className="ag-earlier" aria-label={chatTexts.earlierCaption}>
      {props.items.map((it) => (
        <div className="ag-req ag-earlier-req" key={it.key}>
          <p className="ag-user">{it.text}</p>
          <MessagePills pills={it.mentions} onOpen={props.onOpenMention} />
          {it.reply !== "" && <p className="ag-earlier-reply ag-text small muted">{it.reply}</p>}
        </div>
      ))}
    </section>
  );
}

export interface AgentPanelViewProps {
  state: PanelState;
  ctx: AgentContext;
  draft: string;
  /** A refused button call, shown under the thread; "" when none. */
  notice: string;
  inputRef: RefObject<HTMLTextAreaElement | null>;
  onDraft(text: string): void;
  onSubmit(): void;
  onRemoveContext(): void;
  actionsFor(entry: Entry): CardActions;
  /** The chat list of the header; absent: no chat controls. */
  chat?: ChatBarProps;
  /** Earlier requests of the open chat, as text. */
  earlier?: EarlierItem[];
  /** The «@» mentions of the field; absent: the field has none. */
  mentions?: MentionComposer;
  /** A pill of the thread was pressed: opens the object (kind and id as stored). */
  onOpenMention?: (kind: string, id: string) => void;
  /** The mentions of a live request that has no scope of its own (restored after a restart). */
  mentionsFor?: (entry: Entry) => readonly MentionView[] | undefined;
  /** The context fill of the open chat; null or absent: no ring. */
  fill?: ContextFill | null;
}

/**
 * The right panel on every screen: header, the request field, the context chip and the thread of requests. The field
 * is one element that only changes its place by CSS order (top before the first request, bottom after), so focus and
 * the draft stay when it moves.
 */
export function AgentPanelView(props: AgentPanelViewProps) {
  const { state, ctx, mentions: mn } = props;
  const mode = panelMode(state);
  const entries = orderedEntries(state.entries);
  const last = entries.at(-1);
  const earlier = props.earlier ?? [];
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: "end" });
  }, [entries.length, earlier.length]);

  // A draft grows often: the panel follows it only when the reader is at the bottom, so reading above never jumps.
  const atBottom = useRef(true);
  const draftSize = state.drafts.reduce((n, d) => n + d.texts.reduce((m, t) => m + t.length, d.texts.length), 0);
  useEffect(() => {
    if (draftSize > 0 && atBottom.current) {
      end.current?.scrollIntoView?.({ block: "end", behavior: "auto" });
    }
  }, [draftSize]);

  // The field grows with its text; the CSS max-height (six lines) stops it and the rest scrolls.
  const { inputRef, draft } = props;
  useLayoutEffect(() => {
    const el = inputRef.current;
    if (el) {
      el.style.height = "auto";
      el.style.height = `${el.scrollHeight}px`;
    }
  }, [inputRef, draft]);

  return (
    <aside className={`ag ag-in-${inputPlacement(entries.length + earlier.length)}`} aria-label="Помощник paratech">
      <header className="ag-head">
        <div className="ag-title">
          <span className="ag-name">{texts.title}</span>
          <kbd className="ag-kbd">{texts.hint}</kbd>
        </div>
        {props.chat && <ChatBar {...props.chat} />}
      </header>
      <form
        className="ag-ask-form"
        onSubmit={(e) => {
          e.preventDefault();
          props.onSubmit();
        }}
      >
        <div className="ag-compose" onFocus={mn?.onFocus} onBlur={mn?.onBlur}>
          {mn && <ComposerPills pills={mn.pills} onRemove={mn.remove} />}
          <div className="ag-field">
            <textarea
              ref={props.inputRef}
              className="ag-input"
              rows={1}
              value={props.draft}
              placeholder={mode.placeholder}
              aria-label={mode.placeholder}
              disabled={!mode.inputEnabled}
              maxLength={maxDraftLength}
              autoComplete="off"
              {...(mn
                ? {
                    role: "combobox",
                    "aria-autocomplete": "list" as const,
                    "aria-haspopup": "listbox" as const,
                    "aria-expanded": mn.popup !== null,
                    "aria-controls": mn.popup !== null ? mn.listId : undefined,
                    "aria-activedescendant": mn.activeId,
                    onKeyUp: mn.onCaret,
                    onClick: mn.onCaret,
                    onSelect: mn.onCaret,
                  }
                : {})}
              onChange={(e) => (mn ? mn.onChange(e) : props.onDraft(e.target.value))}
              onKeyDown={(e) => {
                if (mn?.onKeyDown(e)) {
                  return;
                }
                if (composerKey({ key: e.key, shiftKey: e.shiftKey, isComposing: e.nativeEvent.isComposing, keyCode: e.keyCode }) === "send") {
                  e.preventDefault();
                  props.onSubmit();
                }
              }}
            />
            {mn && (
              // A press must not take focus from the field (the caret and the list stay); the field is focused by the click itself.
              <button
                type="button"
                className="ag-at"
                aria-label={mentionTexts.atLabel}
                title={mentionTexts.atLabel}
                // A full field has no room for the space and the «@».
                disabled={!mode.inputEnabled || mn.pills.length >= maxMentions || props.draft.length > maxDraftLength - 2}
                onMouseDown={(e) => e.preventDefault()}
                onClick={mn.openList}
              >
                <span aria-hidden="true">@</span>
              </button>
            )}
          </div>
          {mn && (
            <div className="ag-vh" role="status" aria-live="polite" aria-atomic="true">
              {mn.announcement}
            </div>
          )}
          {mn?.popup && mode.inputEnabled && (
            <MentionPopup model={mn.popup} listId={mn.listId} active={mn.active} onPick={mn.pick} onHover={mn.hover} onRetry={mn.retry} />
          )}
        </div>
        <ContextRing fill={props.fill ?? null} />
        <button
          type="submit"
          className="ag-send"
          aria-label={texts.send}
          title={texts.send}
          disabled={!mode.inputEnabled || props.draft.trim() === ""}
        >
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M12 19V5M5 12l7-7 7 7" />
          </svg>
        </button>
      </form>
      <ContextHint fill={props.fill ?? null} onNew={props.chat?.onNew} />
      <div className="ag-ctx-wrap">
        <div className="ag-ctx">
          <span className="ag-chip">
            <span className="ag-chip-text">{ctx.label}</span>
            {ctx.removable && (
              <button type="button" className="ag-chip-x" aria-label={texts.removeContext} title={texts.removeContext} onClick={props.onRemoveContext}>
                <Icon name="x" />
              </button>
            )}
          </span>
        </div>
        {mode.offline && (
          <p className="ag-offline" role="status">
            <Icon name="off" />
            <span>{texts.offlineBanner}</span>
          </p>
        )}
      </div>
      <div
        className="ag-thread"
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
        }}
      >
        {entries.length === 0 && earlier.length === 0 && (
          <p className="ag-empty muted">{mn && mode.inputEnabled ? `${texts.empty} ${mentionTexts.hint}` : texts.empty}</p>
        )}
        {mode.staleNote !== "" && <p className="ag-stale small muted">{mode.staleNote}</p>}
        <EarlierRequests items={earlier} onOpenMention={props.onOpenMention} />
        {entries.map((e) => (
          <AgentRequest
            key={e.key}
            model={entryModel(e, { last: e.key === last?.key, blocked: blockingEntry(entries, e) !== undefined, mentions: props.mentionsFor?.(e) })}
            actions={props.actionsFor(e)}
            drafts={draftsFor(state, e)}
          />
        ))}
        {props.notice !== "" && (
          <p className="ag-callline small muted" role="status">
            {props.notice}
          </p>
        )}
        <div ref={end} />
      </div>
    </aside>
  );
}
