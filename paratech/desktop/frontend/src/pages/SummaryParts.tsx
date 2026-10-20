// Parts of the summary viewer (TASK-087) that only show a view: the summary, the found homework and the state lines.
// Summary sections, key points and assignment titles are model output: React text nodes only, no HTML, no links from them.

// The .js extensions let the Node test runner load the compiled modules (test/summary.test.mjs renders these parts).
import type { SummarySection, SummaryView } from "../bindings/summary.js";
import {
  assignmentMeta,
  checkSoundLabel,
  earlierNote,
  homeworkLine,
  keyPointList,
  retryLabel,
  type RetryStep,
  showsEarlier,
  type StepLine,
  stepLine,
  summaryLine,
  summarySections,
  updatedText,
} from "../summaryView.js";
import { lessonText } from "../lessonLabel.js";
import { Button } from "../ui/Button.js";
import { Card, ResultLine, Row } from "../ui/controls.js";

export interface RetryProps {
  /** «Повторить» / «Сделать конспект заново» (TASK-086); without it the buttons stay hidden. */
  onRetry?: (step: RetryStep) => void;
  retrying?: RetryStep | "";
  /** Another action of the page (copy, save, a start) is running. */
  retryDisabled?: boolean;
}

/**
 * The state line of a step; also used by the lecture text card. Only the core's Russian message is text: the job
 * error is technical (English, for support) and stays in the tooltip, as on the recording rows (TASK-096, TASK-097).
 */
export function Line(props: { line: StepLine }) {
  const l = props.line;
  const title = l.detail || undefined;
  return l.kind === null ? (
    <p className="sm-state" role="status" title={title}>
      {l.message}
    </p>
  ) : (
    <ResultLine kind={l.kind} role={l.kind === "fail" ? "alert" : "status"} title={title}>
      {l.message}
    </ResultLine>
  );
}

function RetrySlot(props: RetryProps & { view: SummaryView; step: RetryStep }) {
  const label = retryLabel(props.view, props.step);
  const onRetry = props.onRetry;
  if (onRetry === undefined || label === null) {
    return null;
  }
  const busy = props.retrying === props.step;
  return (
    <div className="acts start sm-retry">
      <Button
        busy={busy}
        busyLabel="Запуск…"
        disabled={props.retryDisabled === true || (props.retrying ?? "") !== ""}
        onClick={() => onRetry(props.step)}
      >
        {label}
      </Button>
    </div>
  );
}

/** The summary's sections (TASK-101): a heading, paragraphs and points; focusSection marks one (TASK-091). */
export function SummarySections(props: { sections: readonly SummarySection[]; focusSection?: number }) {
  const shown = summarySections(props.sections);
  if (shown.length === 0) {
    return null;
  }
  return (
    <div className="sm-text" aria-label="Текст конспекта">
      {shown.map((s) => (
        <section key={s.index} className={s.index === props.focusSection ? "sm-sec kb-focus" : "sm-sec"}>
          {s.heading !== "" && <h3 className="sm-h">{s.heading}</h3>}
          {s.paragraphs.map((p, i) => (
            <p key={i}>{p}</p>
          ))}
          {s.points.length > 0 && (
            <ul className="sm-points">
              {s.points.map((p, i) => (
                <li key={i}>{p}</li>
              ))}
            </ul>
          )}
        </section>
      ))}
    </div>
  );
}

/**
 * The summary card: its state, the sections and the key points. focusSection (an index of summary.sections) and
 * focusPoint (of the listed key points) mark the place a search found (TASK-091).
 */
export function SummaryCard(props: RetryProps & { view: SummaryView; focusSection?: number; focusPoint?: number }) {
  const v = props.view;
  const line = summaryLine(v);
  const points = v.summary.ready ? keyPointList(v.summary.keyPoints) : [];
  const updated = updatedText(v);
  return (
    <Card className="box sm-summary">
      <h2>Конспект</h2>
      {line && <Line line={line} />}
      {showsEarlier(v) && <p className="small muted sm-detail">{earlierNote}</p>}
      <RetrySlot {...props} step="summary" />
      {v.summary.ready && <SummarySections sections={v.summary.sections} focusSection={props.focusSection} />}
      {points.length > 0 && (
        <>
          <h3 className="sm-h">Главное</h3>
          <ul className="sm-points">
            {points.map((p, i) => (
              <li key={i} className={i === props.focusPoint ? "kb-focus" : undefined}>
                {p}
              </li>
            ))}
          </ul>
        </>
      )}
      {updated !== "" && <p className="small muted num sm-detail">{updated}</p>}
    </Card>
  );
}

/**
 * A recording without speech (TASK-110): the core's message and «Проверить звук» instead of the parts, since nothing
 * was made from it and nothing is retried.
 */
export function NoSpeechCard(props: { view: SummaryView; onCheckSound?: () => void }) {
  const onCheckSound = props.onCheckSound;
  return (
    <Card className="box sm-summary">
      <Line line={stepLine(props.view.summary)} />
      {onCheckSound !== undefined && (
        <div className="acts start sm-retry">
          <Button onClick={onCheckSound}>{checkSoundLabel}</Button>
        </div>
      )}
    </Card>
  );
}

/** The homework found in the lesson, each with a jump to it in «Домашние задания». */
export function HomeworkCard(props: RetryProps & { view: SummaryView; onOpenAssignment: (id: string) => void }) {
  const v = props.view;
  const line = homeworkLine(v);
  const items = v.homework.assignments;
  return (
    <div className="sm-homework">
      <h2 className="group-title">Домашние задания</h2>
      {line && <Line line={line} />}
      <RetrySlot {...props} step="homework" />
      {items.length > 0 && (
        <div className="card rows" role="list">
          {items.map((a) => (
            <Row
              key={a.id}
              title={a.title}
              meta={
                <>
                  {lessonText(a) !== "" && <span className="sm-lesson">{lessonText(a)}</span>}
                  <span className="num">{assignmentMeta(a)}</span>
                  {a.description !== "" && <span className="sm-desc">{a.description}</span>}
                </>
              }
              actions={
                <Button variant="ghost" onClick={() => props.onOpenAssignment(a.id)}>
                  Открыть задание
                </Button>
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}
