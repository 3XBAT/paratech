import type { ReactNode } from "react";
// The .js extensions let the Node test runner load the compiled row (test/recordingrow.test.mjs renders it).
import type { RecordingItem, RetryWork } from "../bindings/activity.js";
import type { PageId } from "../nav.js";
import type { SummarySection } from "../openSummary.js";
import { type RowAction, recordingRowView, rowLinks } from "../recordingRowView.js";
import type { AdvicePending } from "../sourcePicker/advice.js";
import { SourceAdvice } from "../sourcePicker/SourceAdvice.js";
import type { StatusView } from "../statusPresentation.js";
import { Button } from "../ui/Button.js";
import { Progress, Row } from "../ui/controls.js";
import { StatusChip } from "../ui/StatusChip.js";

export interface RecordingRowProps {
  item: RecordingItem;
  /** The clock for the elapsed time of a running recording (ms since epoch). */
  now: number;
  stopping: boolean;
  starting: boolean;
  onStop: () => void;
  onRunNow: (jobId: string) => void;
  onOpen: (page: PageId) => void;
  /** «Повторить» (TASK-086): the retry running, and the call; without onRetry the row offers no retry. */
  retrying?: boolean;
  onRetry?: (work: Exclude<RetryWork, "">) => void;
  /** More actions of the row, after the core's one (for example opening the transcript). */
  extraActions?: ReactNode;
  /** The silent-source advice of the item (TASK-082): the click running and the problem of the last one. */
  advicePending?: AdvicePending;
  adviceProblem?: string;
  onSwitchSource?: () => void;
  onKeepSource?: () => void;
}

// One recording with the status of its whole chain, and the silent-source advice of a running one. The lesson title and the core's notes are untrusted text: React
// renders them as text only.
export function RecordingRow(props: RecordingRowProps) {
  const r = props.item;
  const v = recordingRowView(r, props.now);
  const button = (a: RowAction | null, variant: "secondary" | "ghost") => {
    switch (a?.kind) {
      case "runNow":
        return (
          <Button
            icon="play"
            busy={props.starting}
            busyLabel="Запуск…"
            disabled={props.starting}
            onClick={() => props.onRunNow(a.jobId)}
          >
            {a.label}
          </Button>
        );
      case "open":
        return (
          <Button variant="ghost" onClick={() => props.onOpen(a.page)}>
            {a.label}
          </Button>
        );
      case "retry": {
        const onRetry = props.onRetry;
        const retrying = props.retrying ?? false;
        return (
          onRetry && (
            <Button
              variant={variant}
              icon="play"
              busy={retrying}
              busyLabel="Запуск…"
              disabled={retrying}
              onClick={() => onRetry(a.work)}
            >
              {a.label}
            </Button>
          )
        );
      }
      default:
        return null;
    }
  };
  return (
    <Row
      title={v.titleIsTime ? <span className="num">{v.title}</span> : v.title}
      meta={
        (v.lesson !== "" || v.meta !== "") && (
          <>
            {v.lesson !== "" && <span className="row-lesson">{v.lesson}</span>}
            {v.meta !== "" && <span className="num">{v.meta}</span>}
          </>
        )
      }
      status={
        <StatusLine view={v.status} notes={v.notes}>
          {v.progress !== null && <Progress value={v.progress} label={v.status.label} text={false} />}
          {v.remaining !== "" && <span className="small muted num row-remaining">{v.remaining}</span>}
        </StatusLine>
      }
      actions={
        <>
          {r.stoppable && (
            <Button
              variant="danger"
              icon="stop"
              busy={props.stopping}
              busyLabel="Остановка…"
              disabled={props.stopping}
              onClick={props.onStop}
            >
              Остановить
            </Button>
          )}
          {button(v.action, "secondary")}
          {button(v.secondary, "ghost")}
          {props.extraActions}
        </>
      }
      below={
        r.advice !== null &&
        props.onSwitchSource &&
        props.onKeepSource && (
          <SourceAdvice
            advice={r.advice}
            pending={props.advicePending ?? null}
            problem={props.adviceProblem ?? ""}
            onSwitch={props.onSwitchSource}
            onKeep={props.onKeepSource}
          />
        )
      }
    />
  );
}

/**
 * The links of a row to the texts of the recording: «Открыть текст лекции» once it is ready (TASK-090), then
 * «Открыть конспект», and the transcript: «Открыть расшифровку», or the small «Исходная расшифровка» next to a lecture
 * text.
 */
export function RecordingLinks(props: {
  item: RecordingItem;
  onOpenSummary: (recordingId: string, section?: SummarySection) => void;
  onOpenTranscript: (recordingId: string) => void;
}) {
  const r = props.item;
  const l = rowLinks(r);
  return (
    <>
      {l.lectureText && (
        <Button icon="doc" onClick={() => props.onOpenSummary(r.id, "lecture")}>
          Открыть текст лекции
        </Button>
      )}
      {l.summary !== null && (
        <Button variant={l.summary === "main" ? "secondary" : "ghost"} icon="doc" onClick={() => props.onOpenSummary(r.id)}>
          Открыть конспект
        </Button>
      )}
      {l.transcript === "full" && (
        <Button variant="ghost" icon="doc" onClick={() => props.onOpenTranscript(r.id)}>
          Открыть расшифровку
        </Button>
      )}
      {l.transcript === "source" && (
        <Button variant="ghost" className="row-source" onClick={() => props.onOpenTranscript(r.id)}>
          Исходная расшифровка
        </Button>
      )}
    </>
  );
}

// The chip, its hint and the core's notes about the row (a problem or the last error).
export function StatusLine(props: { view: StatusView; notes: readonly string[]; children?: ReactNode }) {
  const v = props.view;
  return (
    <>
      <StatusChip kind={v.kind} icon={v.icon}>
        {v.label}
        {v.value !== "" && <span className="num"> · {v.value}</span>}
      </StatusChip>
      {props.children}
      {v.hint !== "" && <span className="small muted">{v.hint}</span>}
      {props.notes.map((n) => (
        <span key={n} className="small row-problem">
          {n}
        </span>
      ))}
    </>
  );
}
