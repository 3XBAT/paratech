import { type RefObject, useEffect, useRef } from "react";
import { type AgentContext, entryModel, texts } from "../agentView.js";
import { type Entry, orderedEntries, panelMode, type PanelState } from "../agentState.js";
import { Icon } from "../ui/Icon.js";
import { AgentRequest, type CardActions } from "./AgentCards.js";

export interface AgentPanelViewProps {
  state: PanelState;
  ctx: AgentContext;
  draft: string;
  /** A refused button call, shown under the thread; "" when none. */
  notice: string;
  inputRef: RefObject<HTMLInputElement | null>;
  onDraft(text: string): void;
  onSubmit(): void;
  onRemoveContext(): void;
  actionsFor(entry: Entry): CardActions;
}

/** The right panel on every screen: header with the input, the context chip and the thread of requests. */
export function AgentPanelView(props: AgentPanelViewProps) {
  const { state, ctx } = props;
  const mode = panelMode(state);
  const entries = orderedEntries(state.entries);
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: "end" });
  }, [entries.length]);

  return (
    <aside className="ag" aria-label="Помощник paratech">
      <header className="ag-head">
        <div className="ag-title">
          <span className="ag-name">{texts.title}</span>
          <kbd className="ag-kbd">{texts.hint}</kbd>
        </div>
        <form
          className="ag-ask-form"
          onSubmit={(e) => {
            e.preventDefault();
            props.onSubmit();
          }}
        >
          <input
            ref={props.inputRef}
            className="ag-input"
            type="text"
            value={props.draft}
            placeholder={mode.placeholder}
            aria-label={mode.placeholder}
            disabled={!mode.inputEnabled}
            maxLength={4000}
            autoComplete="off"
            onChange={(e) => props.onDraft(e.target.value)}
          />
          <button type="submit" className="btn primary sm" disabled={!mode.inputEnabled || props.draft.trim() === ""}>
            {texts.send}
          </button>
        </form>
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
      </header>
      <div className="ag-thread">
        {entries.length === 0 && <p className="ag-empty muted">{texts.empty}</p>}
        {mode.staleNote !== "" && <p className="ag-stale small muted">{mode.staleNote}</p>}
        {entries.map((e) => (
          <AgentRequest key={e.key} model={entryModel(e)} actions={props.actionsFor(e)} />
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
