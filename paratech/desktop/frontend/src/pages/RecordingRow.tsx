import type { ReactNode } from "react";
// The .js extensions let the Node test runner load the compiled row (test/recordingrow.test.mjs renders it).
import type { JobItem, RecordingItem, RecordingSound, RetryWork } from "../bindings/activity.js";
import { jobAssignment, jobTitle } from "../activityView.js";
import type { PageId } from "../nav.js";
import type { SummarySection } from "../openSummary.js";
import { deleteTexts, rowDeleteOffer } from "../recordingDeleteView.js";
import {
  type JobItemKey,
  type JobRowItem,
  jobActions,
  jobDetails,
  jobMeta,
  type RowItem,
  type RowItemKey,
  recordingRowView,
  rowActions,
} from "../recordingRowView.js";
import { importTexts } from "../recordingImportView.js";
import type { AdvicePending } from "../sourcePicker/advice.js";
import { SourceAdvice } from "../sourcePicker/SourceAdvice.js";
import { jobStatusView, showsProgress, type StatusView } from "../statusPresentation.js";
import { Button, IconButton } from "../ui/Button.js";
import { Progress, Row } from "../ui/controls.js";
import { RowMenu, type RowMenuItem } from "../ui/RowMenu.js";
import { SoundLevel } from "../ui/SoundLevel.js";
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
  /** The texts of the recording; without both the row offers none. */
  onOpenSummary?: (recordingId: string, section?: SummarySection) => void;
  onOpenTranscript?: (recordingId: string) => void;
  /** «Сделать конспект заново» in the «⋯» menu: the page asks first; redoing while the call runs. */
  redoing?: boolean;
  onRedoSummary?: () => void;
  /** The silent-source advice of the item (TASK-082): the click running and the problem of the last one. */
  advicePending?: AdvicePending;
  adviceProblem?: string;
  onSwitchSource?: () => void;
  onKeepSource?: () => void;
  /** «Удалить» in the «⋯» menu (TASK-095); without onDelete the menu has no deletion. */
  onDelete?: () => void;
  /** The checkbox of «Удалить выбранные»; without onSelect the row has none. */
  selected?: boolean;
  onSelect?: (selected: boolean) => void;
  /** The live sound of the running recording (TASK-099); the capsule shows only for this row's recording. */
  sound?: RecordingSound | null;
  /** The recording was imported from a file (TASK-162): the row says «импортирована». */
  imported?: boolean;
}

type MenuKey = RowItemKey | "delete";

// One recording with the status of its whole chain, and the silent-source advice of a running one. One main button by
// the state, the other actions in «⋯», a round «Повторить» next to the chip (TASK-096). The lesson title and the
// core's notes are untrusted text: React renders them as text only.
export function RecordingRow(props: RecordingRowProps) {
  const r = props.item;
  const v = recordingRowView(r, props.now);
  const del = rowDeleteOffer(r);
  const onSelect = props.onSelect;
  const onRetry = props.onRetry;
  const retry = v.retry;
  const texts = props.onOpenSummary !== undefined && props.onOpenTranscript !== undefined;
  const acts = rowActions(r, { texts, redo: props.onRedoSummary !== undefined });
  const title = v.titleIsTime ? <span className="num">{v.title}</span> : v.title;
  const sound = r.stage === "recording" && props.sound?.recordingId === r.id ? props.sound : null;

  const run = (key: MenuKey) => {
    const a = v.action;
    switch (key) {
      case "stop":
        props.onStop();
        break;
      case "runNow":
        if (a?.kind === "runNow") {
          props.onRunNow(a.jobId);
        }
        break;
      case "open":
        if (a?.kind === "open") {
          props.onOpen(a.page);
        }
        break;
      case "summary":
        props.onOpenSummary?.(r.id);
        break;
      case "lectureText":
        props.onOpenSummary?.(r.id, "lecture");
        break;
      case "transcript":
        props.onOpenTranscript?.(r.id);
        break;
      case "redo":
        props.onRedoSummary?.();
        break;
      case "delete":
        props.onDelete?.();
        break;
    }
  };

  const main = (i: RowItem) => {
    switch (i.key) {
      case "stop":
        return (
          <Button
            variant="danger"
            icon="stop"
            busy={props.stopping}
            busyLabel="Остановка…"
            disabled={props.stopping}
            onClick={props.onStop}
          >
            {i.label}
          </Button>
        );
      case "runNow":
        return (
          <Button
            icon="play"
            busy={props.starting}
            busyLabel="Запуск…"
            disabled={props.starting}
            onClick={() => run("runNow")}
          >
            {i.label}
          </Button>
        );
      default:
        return (
          <Button icon={i.key === "open" ? undefined : "doc"} onClick={() => run(i.key)}>
            {i.label}
          </Button>
        );
    }
  };

  // «Остановить» is always the main button, never in the menu.
  const busy = (key: MenuKey) => (key === "runNow" && props.starting) || (key === "redo" && (props.redoing ?? false));
  const menu: RowMenuItem<MenuKey>[] = acts.menu.map((i) => ({ key: i.key, label: i.label, disabled: busy(i.key) }));
  if (props.onDelete) {
    menu.push({ key: "delete", label: deleteTexts.menu, disabled: del.disabled, hint: del.hint });
  }

  return (
    <Row
      lead={
        onSelect && (
          <input
            type="checkbox"
            checked={props.selected ?? false}
            disabled={del.disabled}
            title={del.hint || undefined}
            aria-label={`Выбрать: ${v.title}`}
            onChange={(e) => onSelect(e.target.checked)}
          />
        )
      }
      title={title}
      meta={
        (v.lesson !== "" || v.meta !== "" || props.imported === true) && (
          <>
            {v.lesson !== "" && <span className="row-lesson">{v.lesson}</span>}
            {v.meta !== "" && <span className="num">{v.meta}</span>}
            {props.imported === true && <span className="row-imported">{importTexts.imported}</span>}
          </>
        )
      }
      status={
        <StatusLine
          view={v.status}
          notes={v.notes}
          details={v.details}
          aside={
            retry !== null &&
            onRetry && (
              <IconButton icon="retry" label={retry.label} busy={props.retrying} onClick={() => onRetry(retry.work)} />
            )
          }
        >
          {v.progress !== null && <Progress value={v.progress} label={v.status.label} text={false} />}
          {v.remaining !== "" && <span className="small muted num row-remaining">{v.remaining}</span>}
          <SoundLevel sound={sound} hint />
        </StatusLine>
      }
      actions={
        <>
          {acts.main !== null && main(acts.main)}
          <RowMenu label={`Ещё действия: ${v.title}`} title="Ещё действия" items={menu} onPick={run} />
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

export interface JobRowProps {
  item: JobItem;
  /** «Запустить сейчас» is on the way. */
  starting: boolean;
  onRunNow: () => void;
  /** «Открыть задание» of a draft (TASK-094); without it the row opens nothing. */
  onOpenAssignment?: (assignmentId: string) => void;
}

// One task of «Задачи» (a job without a recording), on the same grid as a recording row: one main button and the «⋯»
// cell, which stays empty without more actions (TASK-098). The job error and the attempts are technical: only in the
// chip's tooltip (TASK-096). The draft's title is model text: React renders it as text only.
export function JobRow(props: JobRowProps) {
  const j = props.item;
  const v = jobStatusView(j);
  const assignment = jobAssignment(j);
  const onOpenAssignment = props.onOpenAssignment;
  const acts = jobActions(j, { assignment: onOpenAssignment !== undefined });
  const title = jobTitle(j);
  const meta = jobMeta(j);

  const run = (key: JobItemKey) => {
    if (key === "runNow") {
      props.onRunNow();
    } else if (assignment !== "" && onOpenAssignment) {
      onOpenAssignment(assignment);
    }
  };

  const main = (i: JobRowItem) =>
    i.key === "runNow" ? (
      <Button icon="play" busy={props.starting} busyLabel="Запуск…" disabled={props.starting} onClick={() => run("runNow")}>
        {i.label}
      </Button>
    ) : (
      <Button icon="doc" onClick={() => run("assignment")}>
        {i.label}
      </Button>
    );

  const menu: RowMenuItem<JobItemKey>[] = acts.menu.map((i) => ({
    key: i.key,
    label: i.label,
    disabled: i.key === "runNow" && props.starting,
  }));

  return (
    <Row
      title={title}
      meta={meta !== "" && <span className="num">{meta}</span>}
      status={
        <StatusLine view={v} notes={[]} details={jobDetails(j)}>
          {showsProgress(j) && <Progress value={j.progressPercent} label={v.label} text={false} />}
        </StatusLine>
      }
      actions={
        <>
          {acts.main !== null && main(acts.main)}
          <RowMenu label={`Ещё действия: ${title}`} title="Ещё действия" items={menu} onPick={run} />
        </>
      }
    />
  );
}

// The chip, its hint and the core's notes about the row (a problem or the last error). details is the chip's tooltip;
// aside (the round «Повторить») sits right next to the chip.
export function StatusLine(props: {
  view: StatusView;
  notes: readonly string[];
  details?: string;
  aside?: ReactNode;
  children?: ReactNode;
}) {
  const v = props.view;
  const chip = (
    <StatusChip kind={v.kind} icon={v.icon} title={props.details || undefined}>
      {v.label}
      {v.value !== "" && <span className="num"> · {v.value}</span>}
    </StatusChip>
  );
  return (
    <>
      {props.aside ? (
        <span className="st-head">
          {chip}
          {props.aside}
        </span>
      ) : (
        chip
      )}
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
