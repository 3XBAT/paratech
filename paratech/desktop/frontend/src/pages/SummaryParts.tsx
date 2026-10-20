// Parts of the summary viewer (TASK-087) that only show a view: the summary, the found homework and the state lines.
// Summary text, key points and assignment titles are model output: React text nodes only, no HTML, no links from them.

// The .js extensions let the Node test runner load the compiled modules (test/summary.test.mjs renders these parts).
import type { SummaryView } from "../bindings/summary.js";
import {
  assignmentMeta,
  earlierNote,
  homeworkLine,
  keyPointList,
  retryLabel,
  type RetryStep,
  showsEarlier,
  type StepLine,
  summaryLine,
  summaryParagraphs,
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

/** The state line of a step; also used by the lecture text card. */
export function Line(props: { line: StepLine }) {
  const l = props.line;
  return (
    <>
      {l.kind === null ? (
        <p className="sm-state" role="status">
          {l.message}
        </p>
      ) : (
        <ResultLine kind={l.kind} role={l.kind === "fail" ? "alert" : "status"}>
          {l.message}
        </ResultLine>
      )}
      {l.detail !== "" && <p className="small muted sm-detail">{l.detail}</p>}
    </>
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

/** The summary card: its state, the text in paragraphs and the key points; focusPoint marks a listed one (TASK-091). */
export function SummaryCard(props: RetryProps & { view: SummaryView; focusPoint?: number }) {
  const v = props.view;
  const line = summaryLine(v);
  const paras = v.summary.ready ? summaryParagraphs(v.summary.text) : [];
  const points = v.summary.ready ? keyPointList(v.summary.keyPoints) : [];
  const updated = updatedText(v);
  return (
    <Card className="box sm-summary">
      <h2>Конспект</h2>
      {line && <Line line={line} />}
      {showsEarlier(v) && <p className="small muted sm-detail">{earlierNote}</p>}
      <RetrySlot {...props} step="summary" />
      {paras.length > 0 && (
        <div className="sm-text" aria-label="Текст конспекта">
          {paras.map((p, i) => (
            <p key={i}>{p}</p>
          ))}
        </div>
      )}
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
