import { useId, useState } from "react";
import type { TicketSet } from "../bindings/tickets.js";
import {
  budgetLine,
  confirmGate,
  costConsentLabel,
  fileNote,
  isRunning,
  moves,
  needsCostConsent,
  percent,
  progressText,
  reportParts,
  setCaption,
  showReport,
  statusView,
  texts,
  ticketCount,
  ticketRows,
} from "../ticketsView.js";
import { Button } from "../ui/Button.js";
import { Card, Progress, ResultLine } from "../ui/controls.js";
import { StatusChip } from "../ui/StatusChip.js";

// Ticket titles come from the student's list: only text nodes here, never markup or links.

/** The tickets of a set with the coverage of each; a ticket without an answer is «Не покрыто» once the run ended. */
export function TicketList(props: { set: TicketSet }) {
  const rows = ticketRows(props.set);
  return (
    <ol className="tk-list" aria-label="Билеты">
      {rows.map((r) => (
        <li key={r.number} className="tk-item">
          <span className="tk-num num">{r.number}</span>
          <span className="tk-title">{r.title}</span>
          <StatusChip kind={r.chip.kind} icon={r.chip.icon} title={r.chip.hint || undefined}>
            {r.chip.label}
          </StatusChip>
        </li>
      ))}
    </ol>
  );
}

/** «5 покрыто · 2 частично · 1 не покрыто». */
export function CoverageReport(props: { set: TicketSet }) {
  const parts = reportParts(props.set.report);
  if (!showReport(props.set) || parts.length === 0) {
    return null;
  }
  return (
    <p className="small tk-report" aria-label="Покрытие билетов лекциями">
      {parts.join(" · ")}
    </p>
  );
}

/** The confirm card: the list was found, nothing is paid yet. A run over the cap needs the ticked consent. */
function ConfirmPanel(props: {
  set: TicketSet;
  restart: boolean;
  busy: boolean;
  onConfirm: (acceptCost: boolean) => void;
}) {
  const [ticked, setTicked] = useState(false);
  const checkId = useId();
  const over = !props.restart && needsCostConsent(props.set);
  const gate = confirmGate(props.set, ticked);
  return (
    <div className="tk-confirm">
      <p className="small">{props.restart ? texts.restartNote : budgetLine(props.set)}</p>
      {over && (
        <p className="tk-consent">
          <input id={checkId} type="checkbox" checked={ticked} disabled={props.busy} onChange={(e) => setTicked(e.target.checked)} />
          <label htmlFor={checkId}>{costConsentLabel(props.set)}</label>
        </p>
      )}
      <Button
        variant="primary"
        disabled={props.busy || !gate.allowed}
        disabledReason={over && !ticked ? texts.costConsent : undefined}
        onClick={() => props.onConfirm(gate.acceptCost)}
      >
        {props.restart ? texts.confirmAgain : texts.confirm}
      </Button>
    </div>
  );
}

/** One set: status, progress, the move buttons, the coverage report and the tickets. */
export function SetCard(props: {
  set: TicketSet;
  subjectName: string;
  /** An action of this very card runs. */
  busy: boolean;
  /** An action of any card runs: every button is disabled. */
  blocked: boolean;
  error: string;
  onConfirm: (acceptCost: boolean) => void;
  onCancel: () => void;
  onUndo: () => void;
}) {
  const s = props.set;
  const [open, setOpen] = useState(s.status === "proposed" || isRunning(s));
  const status = statusView(s);
  const m = moves(s);
  const caption = setCaption(s);
  return (
    <Card className="tk-card" label={`Набор: ${ticketCount(s.total)}`}>
      <div className="tk-head">
        <div>
          <h3 className="tk-name">{s.status === "proposed" ? `Найдено: ${ticketCount(s.total)}` : `Набор: ${ticketCount(s.total)}`}</h3>
          {caption !== "" && <p className="small muted tk-caption">{caption}</p>}
        </div>
        <StatusChip kind={status.kind} icon={status.icon}>
          {status.label}
        </StatusChip>
      </div>
      {status.hint !== "" && (
        <p className={status.kind === "fail" ? "small tk-hint s-fail" : "small muted tk-hint"} role={status.kind === "fail" ? "alert" : undefined}>
          {status.hint}
        </p>
      )}
      {(isRunning(s) || s.status === "done") && (
        <Progress value={percent(s)} label="Ход подбора ответов" text={progressText(s)} />
      )}
      <CoverageReport set={s} />
      {m.confirm !== "none" && (
        <ConfirmPanel key={s.version} set={s} restart={m.confirm === "restart"} busy={props.blocked || props.busy} onConfirm={props.onConfirm} />
      )}
      {s.status === "done" && (
        <div className="tk-done">
          <p className="small">{fileNote(props.subjectName)}</p>
          <p className="small muted">{texts.openNote}</p>
          <p className="small muted">{texts.generatedNote}</p>
        </div>
      )}
      <div className="acts start">
        {m.cancel && (
          <Button size="sm" variant="ghost" busy={props.busy} busyLabel={texts.cancelBusy} disabled={props.blocked || props.busy} onClick={props.onCancel}>
            {texts.cancel}
          </Button>
        )}
        {m.undo && (
          <Button size="sm" variant="ghost" disabled={props.blocked || props.busy} onClick={props.onUndo}>
            {texts.undo}
          </Button>
        )}
        {s.tickets.length > 0 && (
          <Button size="sm" variant="ghost" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
            {open ? texts.hide : texts.show}
          </Button>
        )}
      </div>
      {props.error !== "" && (
        <ResultLine kind="fail" role="alert">
          {props.error}
        </ResultLine>
      )}
      {open && <TicketList set={s} />}
    </Card>
  );
}
