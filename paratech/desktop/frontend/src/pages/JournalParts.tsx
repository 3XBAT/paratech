import type { JournalLine } from "../bindings/agent.js";
import {
  type FilterState,
  type KindChoice,
  groupLines,
  kindOptions,
  lineModel,
  periodOptions,
  statusOptions,
  texts,
} from "../journalView.js";
import { Button } from "../ui/Button.js";
import { Card, Row, Segmented, Select } from "../ui/controls.js";

// A journal line shows fixed phrases and the typed name of a subject, as text nodes only.

export function FilterBar(props: {
  filter: FilterState;
  subjects: readonly { id: string; name: string }[];
  onChange(next: FilterState): void;
}) {
  const { filter } = props;
  return (
    <div className="jr-filter">
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
    </div>
  );
}

export function JournalList(props: {
  lines: readonly JournalLine[];
  subjects: ReadonlyMap<string, string>;
  /** The action whose «Отменить» runs now. */
  busyAction: string;
  onUndo(actionId: string): void;
  /** The moment «Сегодня» is counted from; the clock when omitted. */
  now?: Date;
}) {
  if (props.lines.length === 0) {
    return <p className="muted">{texts.empty}</p>;
  }
  return (
    <>
      {groupLines(props.lines, props.now ?? new Date()).map((g) => (
        <section key={g.key} className="jr-sec" aria-label={g.title}>
          <h2 className="jr-group">
            {g.title}
            <span className="cnt num">{g.items.length}</span>
          </h2>
          <Card className="rows" label={g.title}>
            {g.items.map(({ line, index }) => {
              const m = lineModel(line, index, props.subjects);
              const meta = [m.when, m.subject, m.status].filter((x) => x !== "").join(" · ");
              return (
                <Row
                  key={m.key}
                  title={m.title}
                  meta={meta}
                  status={m.reason !== "" ? <span className="small muted">{m.reason}</span> : undefined}
                  actions={
                    m.undoActionId !== "" ? (
                      <Button size="sm" disabled={props.busyAction !== ""} onClick={() => props.onUndo(m.undoActionId)}>
                        {props.busyAction === m.undoActionId ? texts.undoBusy : texts.undo}
                      </Button>
                    ) : m.cannotUndo ? (
                      <Button size="sm" variant="ghost" disabled>
                        {texts.cannotUndo}
                      </Button>
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
