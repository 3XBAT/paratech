import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled page (test/list.test.mjs renders PreviewView).
import { callByName } from "../bindings/runtime.js";
import { scheduleSubjects } from "../bindings/library.js";
import { scheduleApi, type ImportPreview, type LessonText } from "../bindings/schedule.js";
import { CollapsibleList } from "../CollapsibleList.js";
import { allCollapsed, type ListStates, listTexts, setCollapsed, stateOf, withState } from "../listView.js";
import type { PageId } from "../nav.js";
import {
  applyLabel,
  canApply,
  changeSides,
  commitErrorText,
  conflictParts,
  csvTemplate,
  dayHeaders,
  detailParts,
  errorText,
  fillsDetails,
  groupTitles,
  hasChanges,
  lessonMarks,
  lessonTime,
  notes,
  type Part,
  type PreviewGroup,
  previewGroups,
  resultText,
  rowErrorParts,
  showEmptyState,
  summaryChips,
  weekHeaders,
  windowText,
} from "../scheduleView.js";
import { Button } from "../ui/Button.js";
import { offerGroups, reviewTexts } from "../scheduleSubjectsView.js";
import { Banner, Card, CountTiles, EmptyState } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

// Schedule import (PRD section 11.2): choose a file, see its errors or the preview, then apply it. Every text from the
// file is rendered as plain text, each piece in its own <bdi>.
export function SchedulePage(props: {
  onOpen?: (id: PageId) => void;
  /** «Завести предметы из расписания» of the hint: opens «Предметы» with the review already open. */
  onReview?: () => void;
}) {
  const api = useMemo(() => scheduleApi(callByName), []);
  // The subjects the schedule can still make, asked for after an import (a failure shows no hint).
  const [hint, setHint] = useState(0);
  const hintSeq = useRef(0);
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  // Kept here, by group, so choosing the file again keeps what the user folded.
  const [lists, setLists] = useState<ListStates>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const mounted = useRef(true);
  const running = useRef(false); // a double click must not start a second request before busy renders
  // The id of the shown preview, discarded in the core when the page closes. An id the core has dropped already (after
  // a commit or an error) is ignored there.
  const selection = useRef("");

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      discardSelection();
    };
  }, []);

  function show(p: ImportPreview) {
    selection.current = p.selectionId;
    setPreview(p);
  }

  function discardSelection() {
    const id = selection.current;
    selection.current = "";
    if (id !== "") {
      void api.discard(id).catch(() => undefined); // nothing to report: the preview is gone either way
    }
  }

  async function run(action: () => Promise<void>) {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
    setDone("");
    setHint(0);
    hintSeq.current++;
    try {
      await action();
    } finally {
      running.current = false;
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  function loadHint() {
    const seq = hintSeq.current;
    scheduleSubjects(callByName).then(
      (reply) => mounted.current && seq === hintSeq.current && setHint(offerGroups({ loaded: true, reply }) ?? 0),
      () => undefined,
    );
  }

  const choose = () =>
    run(async () => {
      try {
        const p = await api.chooseFile();
        if (p.cancelled) {
          return; // a closed dialog keeps the current preview
        }
        if (!mounted.current) {
          void api.discard(p.selectionId).catch(() => undefined); // the page closed while the dialog was open
          return;
        }
        show(p);
      } catch (err) {
        discardSelection(); // the preview goes away with the error
        if (mounted.current) {
          setPreview(null);
          setError(errorText(err));
        }
      }
    });

  const apply = () => {
    const p = preview;
    if (p === null || !canApply(p)) {
      return;
    }
    return run(async () => {
      // The core drops the selection after any commit outcome. Forget it now, or closing the page mid-commit would send
      // a discard that can reach the core first and cancel the commit.
      selection.current = "";
      try {
        const r = await api.commit(p.selectionId);
        if (mounted.current) {
          setDone(resultText(r));
          loadHint();
        }
      } catch (err) {
        if (mounted.current) {
          setError(commitErrorText(err));
        }
      } finally {
        // The core drops the selection after any commit, so the preview is no longer valid.
        if (mounted.current) {
          setPreview(null);
        }
      }
    });
  };

  const cancel = () => {
    discardSelection();
    setPreview(null);
    setError("");
  };

  const empty = showEmptyState(preview, error, done);

  return (
    <section className="page sched">
      <div className="head">
        <div>
          <span className="label">Импорт из файла</span>
          <h1>Расписание</h1>
        </div>
        {/* Always here, one node: keyboard focus stays on it when the empty state goes away. */}
        <div className="acts">
          <Button variant={preview === null ? "primary" : "secondary"} icon="up" disabled={busy} onClick={() => void choose()}>
            Выбрать файл…
          </Button>
        </div>
      </div>

      {error && (
        <Banner kind="fail" role="alert">
          {error}
        </Banner>
      )}
      {done && (
        <p className="result" role="status">
          <Icon name="ok" />
          {done}
        </p>
      )}
      {done && hint > 0 && (props.onReview || props.onOpen) && (
        // TASK-312: the next step after an import, a hint and not a popup; only when there is something to create.
        <ScheduleNextHint groups={hint} onClick={() => (props.onReview ? props.onReview() : props.onOpen?.("subjects"))} />
      )}
      {empty ? (
        <EmptyState label="Файл ICS, CSV или XLSX" title="Загрузите расписание из файла">
          {`${notes.choose} ${notes.formats}`}
        </EmptyState>
      ) : (
        <p className="muted">{notes.formats}</p>
      )}
      {preview !== null && (
        <PreviewView
          preview={preview}
          busy={busy}
          lists={lists}
          onLists={setLists}
          onApply={() => void apply()}
          onCancel={cancel}
          now={new Date()}
        />
      )}

      <Card className="box sched-format">
        <h2>Формат файла</h2>
        <p className="small muted">{notes.allDay}</p>
        <p className="small muted">{notes.uid}</p>
        <p className="small muted">{notes.details}</p>
        <p className="small muted">{notes.icsDetails}</p>
        <pre className="template num">{csvTemplate}</pre>
        <p className="small muted">{notes.noNotifications}</p>
      </Card>
    </section>
  );
}

/** «В расписании предметов без карточки: N» and the button that opens the review on «Предметы». */
export function ScheduleNextHint(props: { groups: number; onClick(): void }) {
  return (
    <p className="small muted sched-next">
      <span>{reviewTexts.nextHint(props.groups)}</span>
      <Button size="sm" onClick={props.onClick}>
        {reviewTexts.open}
      </Button>
    </p>
  );
}

export function PreviewView(props: {
  preview: ImportPreview;
  busy: boolean;
  lists: ListStates;
  onLists: (next: ListStates) => void;
  onApply: () => void;
  onCancel: () => void;
  /** The moment past and today's lessons are judged against. */
  now?: Date;
}) {
  const p = props.preview;
  const now = props.now ?? new Date();
  const groups = previewGroups(p);
  const folded = allCollapsed(props.lists, groups);

  function group<T>(
    kind: PreviewGroup,
    items: readonly T[],
    renderRow: (item: T, index: number) => ReactNode,
    extra: { more?: (hidden: number) => string; before?: ReactNode; after?: ReactNode } = {},
  ) {
    if (!groups.includes(kind)) {
      return null;
    }
    return (
      <CollapsibleList
        title={groupTitles[kind]}
        items={items}
        itemKey={(_, i) => i}
        renderItem={renderRow}
        state={stateOf(props.lists, kind)}
        onStateChange={(s) => props.onLists(withState(props.lists, kind, s))}
        labels={{ more: extra.more }}
        before={extra.before}
        after={extra.after}
        className={`clist-card preview-group g-${kind}`}
        classNames={{ body: "card lst", button: "btn ghost" }}
      />
    );
  }

  // A lesson line under its week divider and day header: the time, the title with its kind, subject and teacher, the
  // zone from the file (each file piece in its own <bdi>), the file line. Past lessons are muted; today's and the next one highlighted unless they are to be removed.
  function lessons(items: readonly (LessonText & { line: number })[], highlight: boolean) {
    const weeks = weekHeaders(items);
    const days = dayHeaders(items, now);
    const marks = lessonMarks(items, now, highlight);
    return (l: LessonText & { line: number }, i: number) => {
      const w = weeks[i];
      const d = days[i];
      const m = marks[i];
      const cls = ["ln-row", m.past && "is-past", m.today && "is-today", m.next && "is-next"].filter(Boolean).join(" ");
      const details = detailParts(l);
      return (
        <li className="ln">
          {w && (
            <span className="wk">
              <span>{w.label}</span>
              <span className="n num">{w.countText}</span>
            </span>
          )}
          {d && <span className={highlight && d === "Сегодня" ? "ln-day is-today" : "ln-day"}>{d}</span>}
          <span className={cls}>
            <span className="ln-when num">{lessonTime(l.start, l.end)}</span>
            <span className="ln-title">
              <Text parts={[{ text: l.title, file: true }]} />
              {details.length > 0 && (
                <span className="ln-meta small muted">
                  <Text parts={details} />
                </span>
              )}
            </span>
            <span className="ln-zone small muted">
              <Text parts={[{ text: l.zone, file: true }]} />
            </span>
            <span className="ln-line small muted num">{l.line > 0 ? `строка ${l.line}` : ""}</span>
          </span>
        </li>
      );
    };
  }

  const plainRow = (parts: readonly Part[]) => (
    <li className="ln-text">
      <Text parts={parts} />
    </li>
  );

  return (
    <div className="import-preview">
      <h2 className="group-title">
        Файл: <bdi>{p.fileName}</bdi>
      </h2>
      <CountTiles
        label="Итог проверки файла"
        tiles={summaryChips(p).map((c) => ({
          key: c.kind,
          label: c.label,
          count: c.count,
          className: c.kind === "errors" ? (c.count > 0 ? "bad" : "zero") : undefined,
        }))}
      />

      {p.errors.length > 0 ? (
        <Banner kind="fail">
          <p>{notes.rowErrors}</p>
        </Banner>
      ) : (
        <>
          {p.window !== null && <p className="muted">{windowText(p.window)}</p>}
          {!hasChanges(p) && <p>{notes.noChanges}</p>}
        </>
      )}

      {groups.length > 0 && (
        <div className="actions">
          <Button variant="ghost" onClick={() => props.onLists(setCollapsed(props.lists, groups, !folded))}>
            {folded ? listTexts.expandAll : listTexts.collapseAll}
          </Button>
        </div>
      )}
      {group("errors", p.errors, (e) => plainRow(rowErrorParts(e)), { more: () => listTexts.allErrors })}
      {group("added", p.added, lessons(p.added, true))}
      {group(
        "changed",
        p.changed,
        (c) => {
          const sides = changeSides(c);
          return (
            <li className="diff">
              <span className="ln-line small muted num">{c.line > 0 ? `строка ${c.line}` : ""}</span>
              <del>
                <Text parts={sides.before} />
              </del>
              <span className="diff-arrow">→</span>
              <ins>
                <Text parts={sides.after} />
              </ins>
            </li>
          );
        },
        { before: fillsDetails(p.changed) && <p className="muted">{notes.filledDetails}</p> },
      )}
      {group("removed", p.removed, lessons(p.removed, false))}
      {group("conflicts", p.conflicts, (c) => plainRow(conflictParts(c)), {
        before: (
          <Banner kind="wait">
            <p>{notes.conflicts}</p>
          </Banner>
        ),
        after: p.conflictsTruncated && <p className="warning">{notes.conflictsTruncated}</p>,
      })}

      {/* Pinned to the window edge: Apply stays visible below a long preview. */}
      <div className="actions sticky-actions">
        {canApply(p) && (
          <Button variant="primary" disabled={props.busy} onClick={props.onApply}>
            {applyLabel(p)}
          </Button>
        )}
        <Button disabled={props.busy} onClick={props.onCancel}>
          Отмена
        </Button>
      </div>
    </div>
  );
}

// Text renders parts as plain text; a piece from the file goes into its own <bdi>, isolated from the rest of the line.
function Text(props: { parts: readonly Part[] }) {
  return (
    <>
      {props.parts.map((p, i) => (p.file ? <bdi key={i}>{p.text}</bdi> : <span key={i}>{p.text}</span>))}
    </>
  );
}
