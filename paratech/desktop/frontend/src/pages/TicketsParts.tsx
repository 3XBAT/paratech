import { useId, useState } from "react";
import type { TicketSet } from "../bindings/tickets.js";
import {
  budgetLine,
  canOpenDocx,
  confirmGate,
  costConsentLabel,
  fileNote,
  isRunning,
  moves,
  needsCostConsent,
  partsList,
  partsNote,
  percent,
  progressText,
  reportParts,
  setCaption,
  showReport,
  statusView,
  texts,
  ticketCount,
  ticketRows,
  undoLabel,
  type PartView,
} from "../ticketsView.js";
import { Button } from "../ui/Button.js";
import { Card, Progress, ResultLine } from "../ui/controls.js";
import { IconAction } from "../ui/IconAction.js";
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

type OpenResultView = { kind: "ok" | "fail"; text: string };

/** The line under an open button: a success or the binding's refusal text (a student's file name is never put into it). */
function OpenResult(props: { result: OpenResultView | undefined; /** Replaces the text of a success (the short line of a part). */ okText?: string }) {
  if (!props.result) {
    return null;
  }
  return (
    <ResultLine kind={props.result.kind} role={props.result.kind === "fail" ? "alert" : "status"}>
      {props.result.kind === "ok" && props.okText !== undefined ? props.okText : props.result.text}
    </ResultLine>
  );
}

/** The reason an icon is off while another file opens; nothing when no open runs or this very control is the one running. */
function otherOpens(openingId: string, own = ""): string | undefined {
  return openingId !== "" && openingId !== own ? texts.otherOpening : undefined;
}

/**
 * The parts of a split tickets file, in order: «Билеты 1–50», «Билеты 51–100». Each part has its own open icon and its own
 * result line; one running open blocks the others, and a failed part can be tried again.
 */
export function FilesList(props: {
  parts: PartView[];
  blocked: boolean;
  openingId: string;
  results: Readonly<Record<string, OpenResultView>>;
  onOpen: (materialId: string) => void;
}) {
  return (
    <ul className="tk-files" aria-label={texts.partsLabel}>
      {props.parts.map((p) => (
        <li key={p.materialId} className="tk-file">
          <span className="tk-file-name">{p.label}</span>
          <IconAction
            icon="open"
            size="sm"
            label={p.openLabel}
            busy={props.openingId === p.materialId}
            disabled={props.blocked}
            disabledReason={otherOpens(props.openingId, p.materialId)}
            onClick={() => props.onOpen(p.materialId)}
          />
          <div className="tk-file-result">
            <OpenResult result={props.results[p.materialId]} okText={texts.openedPart} />
          </div>
        </li>
      ))}
    </ul>
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
  /** Opens one filed .docx (a copy); only that part's material id goes to the core. */
  onOpen: (materialId: string) => void;
  /** The material whose opening runs ("" none); a part of another card is not this card's. */
  openingId?: string;
  /** The last open result of each material, by material id. */
  openResults?: Readonly<Record<string, OpenResultView>>;
}) {
  const s = props.set;
  const parts = partsList(s);
  const openingId = props.openingId ?? "";
  const openResults = props.openResults ?? {};
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
          {parts.length > 1 && <p className="small muted">{partsNote(parts.length)}</p>}
          <p className="small">{fileNote(props.subjectName, parts.length)}</p>
          {parts.length <= 1 && (
            <>
              {canOpenDocx(s) && <p className="small muted">{texts.openNote}</p>}
              <p className="small muted">{texts.generatedNote}</p>
            </>
          )}
          {parts.length > 1 && (
            <>
              <FilesList
                parts={parts}
                blocked={props.blocked || props.busy}
                openingId={openingId}
                results={openResults}
                onOpen={props.onOpen}
              />
              {/* The notes of the whole file sit under the list, said once. */}
              {canOpenDocx(s) && <p className="small muted">{texts.openNote}</p>}
              <p className="small muted">{texts.generatedNote}</p>
            </>
          )}
          {parts.length === 1 && (
            <>
              <div className="acts start">
                <Button
                  size="sm"
                  busy={openingId === parts[0].materialId}
                  busyLabel={texts.openBusy}
                  disabled={props.blocked || props.busy}
                  onClick={() => props.onOpen(parts[0].materialId)}
                >
                  {texts.open}
                </Button>
              </div>
              <OpenResult result={openResults[parts[0].materialId]} />
            </>
          )}
        </div>
      )}
      <div className="acts start">
        {m.cancel && s.status === "proposed" && (
          // The «no» of «Подтвердить»: a yes/no pair keeps its words.
          <Button size="sm" variant="ghost" busy={props.busy} busyLabel={texts.cancelBusy} disabled={props.blocked || props.busy} onClick={props.onCancel}>
            {texts.cancel}
          </Button>
        )}
        {m.cancel && s.status !== "proposed" && (
          <IconAction
            icon="x"
            variant="danger"
            label={props.busy ? texts.cancelBusy : texts.cancel}
            busy={props.busy}
            disabled={props.blocked}
            onClick={props.onCancel}
          />
        )}
        {m.undo && (
          <IconAction
            icon="undo"
            label={undoLabel(parts.length)}
            disabled={props.blocked || props.busy}
            disabledReason={parts.length > 1 ? otherOpens(openingId) : undefined}
            onClick={props.onUndo}
          />
        )}
        {s.tickets.length > 0 && (
          <IconAction icon={open ? "chevup" : "chev"} expanded={open} label={open ? texts.hide : texts.show} onClick={() => setOpen((o) => !o)} />
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
