import { useId, useState } from "react";
import type { JournalLine } from "../bindings/agent.js";
import {
  extraFilterCount,
  type FilterState,
  type KindChoice,
  groupLines,
  kindOptions,
  lineModel,
  periodOptions,
  statusOptions,
  texts,
  type UndoOutcome,
} from "../journalView.js";
import { Button } from "../ui/Button.js";
import { Card, ResultLine, Row, Segmented, Select } from "../ui/controls.js";
import { IconAction } from "../ui/IconAction.js";

// A journal line shows fixed phrases and the typed name of a subject, as text nodes only.

/**
 * The filters: «Период» and «Только отменяемые» are always in sight; «Что показывать», «Предмет» and «Состояние» are
 * under «Ещё фильтры». The disclosure starts open when one of them is set and shows how many are, so a filter that
 * narrows the list is never hidden without a trace.
 */
export function FilterBar(props: {
  filter: FilterState;
  subjects: readonly { id: string; name: string }[];
  onChange(next: FilterState): void;
}) {
  const { filter } = props;
  const extra = extraFilterCount(filter);
  const [more, setMore] = useState(extra > 0);
  const moreId = useId();
  return (
    <>
      <div className="jr-filter">
        <label className="jr-field">
          <span className="small muted">{texts.periodLabel}</span>
          <Select aria-label={texts.periodLabel} value={String(filter.days)} onChange={(e) => props.onChange({ ...filter, days: Number(e.target.value) })}>
            {periodOptions.map((o) => (
              <option key={o.value} value={String(o.value)}>
                {o.label}
              </option>
            ))}
          </Select>
        </label>
        <label className="jr-check">
          <input type="checkbox" checked={filter.undoable} onChange={(e) => props.onChange({ ...filter, undoable: e.target.checked })} />
          <span>{texts.onlyUndoable}</span>
        </label>
        <button
          type="button"
          className="btn ghost jr-more-btn"
          aria-expanded={more}
          aria-controls={more ? moreId : undefined}
          onClick={() => setMore((m) => !m)}
        >
          <span aria-hidden="true">{more ? "▾ " : "▸ "}</span>
          {texts.moreFilters}
          {extra > 0 && <span className="cnt num">{extra}</span>}
        </button>
      </div>
      {more && (
        <div className="jr-more" id={moreId}>
          <Segmented
            label={texts.kindLabel}
            options={kindOptions}
            value={filter.kind}
            onChange={(kind: KindChoice) => props.onChange({ ...filter, kind })}
          />
          <label className="jr-field">
            <span className="small muted">{texts.subjectLabel}</span>
            <Select aria-label={texts.subjectLabel} value={filter.subjectId} onChange={(e) => props.onChange({ ...filter, subjectId: e.target.value })}>
              <option value="">Любой</option>
              {props.subjects.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          </label>
          <label className="jr-field">
            <span className="small muted">{texts.statusLabel}</span>
            <Select aria-label={texts.statusLabel} value={filter.status} onChange={(e) => props.onChange({ ...filter, status: e.target.value })}>
              {statusOptions.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </Select>
          </label>
        </div>
      )}
    </>
  );
}

/** The load error with its way out, in one line. */
export function LoadError(props: { text: string; onRetry(): void }) {
  return (
    <div className="jr-error">
      <ResultLine kind="fail" role="alert">
        {props.text}
      </ResultLine>
      <Button size="sm" variant="ghost" onClick={props.onRetry}>
        {texts.retry}
      </Button>
    </div>
  );
}

export function JournalList(props: {
  lines: readonly JournalLine[];
  subjects: ReadonlyMap<string, string>;
  /** The action whose «Отменить» runs now. */
  busyAction: string;
  onUndo(actionId: string): void;
  /** The outcome of the last «Отменить»: it shows in the row of that action. */
  outcome?: UndoOutcome | null;
  /** The moment «Сегодня» is counted from; the clock when omitted. */
  now?: Date;
  /** A filter beyond the period is set: an empty list says the filters hide everything, with the way to drop them. */
  filtered?: boolean;
  onResetFilters?(): void;
}) {
  if (props.lines.length === 0) {
    if (props.filtered === true) {
      return (
        <div className="jr-none">
          <p className="muted">{texts.emptyFiltered}</p>
          {props.onResetFilters !== undefined && (
            <Button variant="ghost" onClick={props.onResetFilters}>
              {texts.resetFilters}
            </Button>
          )}
        </div>
      );
    }
    return <p className="muted">{texts.empty}</p>;
  }
  const outcome = props.outcome ?? null;
  return (
    <>
      {groupLines(props.lines, props.now ?? new Date()).map((g) => (
        <section key={g.key} className="jr-sec" aria-labelledby={`jr-h-${g.key}`}>
          <h2 className="jr-group" id={`jr-h-${g.key}`}>
            {g.title}
            <span className="cnt num">{g.items.length}</span>
          </h2>
          <Card className="rows">
            {g.items.map(({ line, index }) => {
              const m = lineModel(line, index, props.subjects, g.key !== "now" && g.key !== "none");
              const meta = [m.when, m.subject, m.chat === "" ? "" : `чат «${m.chat}»`, m.status].filter((x) => x !== "").join(" · ");
              const here = outcome !== null && m.actionId !== "" && outcome.actionId === m.actionId ? outcome : null;
              return (
                <Row
                  key={m.key}
                  title={m.title}
                  meta={meta}
                  status={m.reason !== "" ? <span className="small muted">{m.reason}</span> : undefined}
                  actions={
                    m.undoActionId !== "" ? (
                      <IconAction
                        icon="undo"
                        place="left"
                        label={props.busyAction === m.undoActionId ? texts.undoBusy : texts.undo}
                        busy={props.busyAction === m.undoActionId}
                        disabled={props.busyAction !== ""}
                        onClick={() => props.onUndo(m.undoActionId)}
                      />
                    ) : undefined
                  }
                  below={
                    here !== null ? (
                      <div className="jr-outcome">
                        <ResultLine kind={here.ok ? "ok" : "fail"} role={here.ok ? "status" : "alert"}>
                          {here.text}
                        </ResultLine>
                      </div>
                    ) : undefined
                  }
                />
              );
            })}
          </Card>
        </section>
      ))}
    </>
  );
}
