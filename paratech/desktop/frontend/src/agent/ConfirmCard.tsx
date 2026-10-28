import { useEffect, useMemo, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled card.
import type { AgentConfirmApi, ConfirmCard as CardData, ConfirmTarget } from "../bindings/agentConfirm.js";
import type { TeacherRole } from "../bindings/recipient.js";
import type { RecordingDeleteApi } from "../bindings/recordingDelete.js";
import { type ConfirmCardModel, ConfirmFlow, confirmCardModel, confirmTexts, type FlowState, initialFlow } from "../agentConfirmView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { type RecipientNav, useRecipientNav } from "../recipientNav.js";
import { isRefusal, refusalAction } from "../recipientView.js";
import { RecipientName, RefusalButton, TeacherChoiceGroup } from "../pages/RecipientParts.js";
import { RecordingDeleteDialog } from "../pages/RecordingDeleteDialog.js";

// Every value of the card is a React text node: React escapes it, so markup in a title, an address or a file name
// stays visible text. No element, link or mailto is built from it.

/** The card as it looks for a given state of the flow (no hooks: the tests render it directly). */
export function ConfirmCardView(props: {
  model: ConfirmCardModel;
  busy: FlowState["busy"];
  error: string;
  onYes: () => void;
  onNo: () => void;
  /** «Кому отправить?»: the role clicked so far ("" none) and the click. Without onChoose the options are read-only. */
  chosenRole?: TeacherRole | "";
  onChoose?: (role: TeacherRole) => void;
  /** The way out of a refusal («Указать почту», «Открыть задание»); without it the buttons do nothing. */
  nav?: RecipientNav;
}) {
  const { model: m } = props;
  const off = props.busy !== "";
  const rv = m.recipient;
  return (
    <section className="ag-card ag-confirm">
      <h3 className="ag-card-h">
        <Icon name="warn" />
        {confirmTexts.caption} · {m.title}
      </h3>
      {m.rows.length > 0 && (
        <dl className="ag-conf-rows">
          {m.rows.map((r) => (
            <div key={r.label} className="ag-conf-row">
              <dt className="small muted">{r.label}</dt>
              <dd>{r.address === undefined ? r.value : <RecipientName name={r.value} address={r.address} />}</dd>
            </div>
          ))}
        </dl>
      )}
      {rv.kind === "choose" && (
        <TeacherChoiceGroup
          options={rv.options}
          value={props.chosenRole ?? ""}
          disabled={off || props.onChoose === undefined}
          onChange={(role) => props.onChoose?.(role)}
        />
      )}
      {m.items.length > 0 && (
        <ul className="ag-list small">
          {m.items.map((t, i) => (
            <li key={i}>{t}</li>
          ))}
        </ul>
      )}
      {m.notice !== "" && (
        <p className="ag-note">
          <Icon name="info" />
          <span>{m.notice}</span>
        </p>
      )}
      {m.outcome !== "" && (
        <p className="ag-conf-out" role="status">
          {m.outcome}
        </p>
      )}
      {m.outcome === "" && isRefusal(rv) && (
        <ResultLine kind="fail" role="alert">
          {rv.text}
        </ResultLine>
      )}
      {m.outcome === "" && !isRefusal(rv) && <p className="small muted">{m.note}</p>}
      {props.error !== "" && (
        <ResultLine kind="fail" role="alert">
          {props.error}
        </ResultLine>
      )}
      {isRefusal(rv) && props.nav !== undefined && (
        <div className="ag-actions">
          <RefusalButton action={refusalAction(rv, "card")} nav={props.nav} disabled={off} />
        </div>
      )}
      {(m.yes || m.no) && (
        <div className="ag-actions">
          {/* «Нет» first and nothing is focused: Enter never lands on «Да» by itself. */}
          {m.no && (
            <Button size="sm" variant="secondary" disabled={off} onClick={props.onNo}>
              {confirmTexts.no}
            </Button>
          )}
          {m.yes && (
            <Button
              size="sm"
              variant="danger"
              disabled={off || m.yesBlocked !== ""}
              disabledReason={m.yesBlocked !== "" ? m.yesBlocked : undefined}
              busy={props.busy === "confirm"}
              busyLabel={confirmTexts.working}
              onClick={props.onYes}
            >
              {confirmTexts.yes}
            </Button>
          )}
        </div>
      )}
    </section>
  );
}

/** Owns the flow of one card: loads it (or shows the one given), maps the buttons to the core. */
export function ConfirmCard(props: {
  api: AgentConfirmApi;
  deleteApi: RecordingDeleteApi;
  /** The target the agent's card names; read from the core. */
  target?: ConfirmTarget;
  /** A card the page already has (a proposal just made). */
  initial?: CardData;
  /** Called with every card the flow shows, the initial one and each reload (the work card tracks a sent work). */
  onCard?: (card: CardData) => void;
}) {
  const [state, setState] = useState<FlowState>(initialFlow);
  const nav = useRecipientNav();
  const { onCard } = props;
  const shown = state.card;
  useEffect(() => {
    if (shown !== null) {
      onCard?.(shown);
    }
  }, [shown, onCard]);
  const flow = useMemo(() => new ConfirmFlow(props.api, setState), [props.api]);
  const initial = useRef(props.initial);
  const targetKind = props.target?.kind;
  const targetId = props.target?.id;

  useEffect(() => {
    flow.attach();
    if (initial.current !== undefined) {
      flow.show(initial.current);
    } else if (targetKind !== undefined && targetId !== undefined) {
      void flow.load({ kind: targetKind, id: targetId });
    }
    return () => flow.dispose();
  }, [flow, targetKind, targetId]);

  if (state.card === null) {
    return (
      <section className="ag-card ag-confirm">
        <h3 className="ag-card-h">{confirmTexts.caption}</h3>
        {state.error === "" ? (
          <p className="muted">{confirmTexts.loading}</p>
        ) : (
          <>
            <ResultLine kind="fail" role="alert">
              {state.error}
            </ResultLine>
            {targetKind !== undefined && targetId !== undefined && (
              <div className="ag-actions">
                <Button size="sm" onClick={() => void flow.load({ kind: targetKind, id: targetId })}>
                  {confirmTexts.reload}
                </Button>
              </div>
            )}
          </>
        )}
      </section>
    );
  }
  const card = state.card;
  return (
    <>
      <ConfirmCardView
        model={confirmCardModel(card, state.chosenRole)}
        busy={state.busy}
        error={state.error}
        chosenRole={state.chosenRole}
        nav={nav}
        onChoose={(role) => flow.choose(role)}
        onYes={() => void flow.yes()}
        onNo={() => void flow.no()}
      />
      {state.deleteOpen && card.kind === "delete_recording" && (
        <RecordingDeleteDialog
          api={props.deleteApi}
          ids={[card.targetId]}
          onDone={(result) => (result.removed > 0 ? flow.deleted() : flow.closeDelete())}
          onCancel={() => flow.closeDelete()}
        />
      )}
    </>
  );
}
