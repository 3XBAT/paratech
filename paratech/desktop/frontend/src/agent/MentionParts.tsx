import { useEffect, useRef } from "react";
import type { MentionView } from "../bindings/agent.js";
import { mentionKindCaption, mentionOpening, type MentionPill, mentionTexts, type PopupModel, removeMentionLabel } from "../agentMentions.js";
import { Button } from "../ui/Button.js";
import { Icon } from "../ui/Icon.js";

// Names of objects are untrusted plain text: they reach the page as React text nodes (and as attribute values), never as
// markup, and never inside a URL or a class name.

/** The pills of a request in the thread: the name stored at mention time; a real button that opens the object. */
export function MessagePills(props: { pills: MentionPill[]; onOpen?: (kind: string, id: string) => void }) {
  if (props.pills.length === 0) {
    return null;
  }
  const { onOpen } = props;
  return (
    <ul className="ag-pills" aria-label={mentionTexts.pillsLabel}>
      {props.pills.map((m) => (
        <li key={m.key}>
          {onOpen && mentionOpening(m.kind, m.id) !== null ? (
            <button type="button" className="ag-pill small" title={m.title} aria-label={m.aria} onClick={() => onOpen(m.kind, m.id)}>
              {m.label}
            </button>
          ) : (
            // Nothing opens from here, so the tooltip promises nothing («Откроется …» is for the button only).
            <span className="ag-pill small" title={`${m.caption}: ${m.label}`}>
              {m.label}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

/** The chosen mentions above the field: each can be removed. Looks like the context chip: both are «what the agent takes into account». */
export function ComposerPills(props: { pills: readonly MentionView[]; onRemove(index: number): void }) {
  if (props.pills.length === 0) {
    return null;
  }
  return (
    <ul className="ag-mpills" aria-label={mentionTexts.pillsLabel}>
      {props.pills.map((p, i) => (
        <li key={`${p.kind}:${p.id}`} className="ag-chip ag-mpill">
          <span className="ag-chip-text" title={`${mentionKindCaption(p.kind)}: ${p.label}`}>
            {p.label}
          </span>
          <button type="button" className="ag-chip-x ag-mpill-x" aria-label={removeMentionLabel(p.label)} title={removeMentionLabel(p.label)} onClick={() => props.onRemove(i)}>
            <Icon name="x" />
          </button>
        </li>
      ))}
    </ul>
  );
}

export interface MentionPopupProps {
  model: PopupModel;
  listId: string;
  active: number;
  onPick(index: number): void;
  onHover(index: number): void;
  onRetry(): void;
}

/**
 * The list of candidates (or the one line that says why there is none). Focus stays in the field: a press on the popup
 * does not take it, the options are not tab stops (aria-activedescendant on the field points at the active one). The
 * status lines are read out by the always-mounted live region of the field, not by a region of their own.
 */
export function MentionPopup(props: MentionPopupProps) {
  const { model, listId, active } = props;
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    box.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: "nearest" });
  }, [active, model.options.length]);
  return (
    <div className="ag-mpop" ref={box} onMouseDown={(e) => e.preventDefault()}>
      <ul className="ag-mlist" role="listbox" id={listId} aria-label={mentionTexts.listLabel}>
        {model.options.map((o, i) => (
          <li
            key={o.key}
            id={`${listId}-${i}`}
            role="option"
            aria-selected={i === active}
            aria-disabled={o.picked ? true : undefined}
            className="ag-mopt"
            onMouseMove={() => props.onHover(i)}
            onClick={() => props.onPick(i)}
          >
            <span className="ag-mopt-t">{o.label}</span>
            <span className="ag-mopt-k small muted">{o.caption}</span>
            {(o.hint !== "" || o.picked) && (
              <span className="ag-mopt-h small muted">
                {o.hint !== "" && <span className="ag-mopt-ht">{o.hint}</span>}
                {o.picked && <span className="ag-mopt-done">{o.hint !== "" ? ` · ${mentionTexts.picked}` : mentionTexts.picked}</span>}
              </span>
            )}
          </li>
        ))}
      </ul>
      {model.text !== "" && (
        <div className="ag-mstatus small muted">
          <span>{model.text}</span>
          {model.retry && (
            // Not a tab stop: Enter in the field repeats the search, so the field keeps the focus.
            <Button size="sm" tabIndex={-1} title={mentionTexts.retryTitle} onClick={props.onRetry}>
              {mentionTexts.retry}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
