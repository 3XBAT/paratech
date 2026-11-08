import { Fragment, type ReactNode, useId, useRef, useState } from "react";
import type { AnswerInput, AssignmentLineView, DraftView, OfferView } from "../bindings/agent.js";
import { Button } from "../ui/Button.js";
import { AgentActions, UserActions } from "./MessageActions.js";
import { Icon } from "../ui/Icon.js";
import { IconAction } from "../ui/IconAction.js";
import type { AgentConfirmApi } from "../bindings/agentConfirm.js";
import type { RecordingDeleteApi } from "../bindings/recordingDelete.js";
import type { TurnkeyApi } from "../bindings/turnkey.js";
import type { AnswersApi } from "../bindings/answers.js";
import type { CitationOpening } from "../knowledgeView.js";
import { type CutReply, replyDraftText, splitDrafts } from "../agentState.js";
import { AnswerCard } from "./AnswerCard.js";
import { ConfirmCard } from "./ConfirmCard.js";
import { ScheduleSubjectsCard } from "./ScheduleCard.js";
import { type OfferResult, offerReasons } from "../agentOffer.js";
import type { RestoreCall } from "../subjectRemovalView.js";
import { WorkWritingCard } from "./WorkCard.js";
import { MessagePills } from "./MentionParts.js";
import { Elapsed } from "./Elapsed.js";
import { Waiting } from "./Waiting.js";
import { openTaskLabel, summaryToolTexts } from "../summaryView.js";
import { failedStepIndex, moreStepsLabel, planCollapseLimit, planTexts, planWindow, progressLabel, type StepMark } from "../planView.js";
import {
  addedAssignmentText,
  assignmentRow,
  type ButtonCard,
  cardButtonIcon,
  offerIcon,
  replyCopyText,
  replyStatus,
  type ButtonId,
  type ButtonModel,
  type CardModel,
  collectAnswers,
  type EntryModel,
  type OfferModel,
  skipAnswers,
  texts,
  undoableActions,
  undoIcon,
  type UndoNote,
} from "../agentView.js";

// Every text of the model reaches the page as a React text node: React escapes it, so markup in it stays visible text.
// No element is built from model text.

/** What a card can ask of the panel; the panel maps it to the call of the request the card belongs to. */
export interface CardActions {
  /** A button was pressed and the core has not answered yet: the buttons are off (double-click protection). */
  busy?: boolean;
  /** The core calls of the confirm cards; without them a confirm card only says it is unavailable. */
  confirm?: { api: AgentConfirmApi; deleteApi: RecordingDeleteApi };
  /** The core calls of the `work` cards; without them such a card shows nothing. */
  turnkey?: TurnkeyApi;
  /** Subject names by id, for the assignment list of a reply; an unknown subject is left out. */
  subjects?: Readonly<Record<string, string>>;
  /** «Сегодня» for the due dates; the current time when absent. */
  now?: Date;
  /** Opens the assignment on «Домашние задания». */
  openAssignment?: (id: string) => void;
  /** Opens the object of a mention pill under the student's text (kind and id as stored). */
  openMention?: (kind: string, id: string) => void;
  /**
   * The press of a page button an offer names («Начать запись» under a stopped recording, an offer_button card): the
   * panel runs the page's own binding for `offer.action` with `offer.target` (agentOffer.ts) and answers how it went;
   * a refusal is shown beside the button, which stays usable. Absent: no button is drawn.
   */
  offer?: (offer: OfferView) => void | Promise<OfferResult | void>;
  /** The wording of the action line «Отменить» (undo_action) would undo; "" when there is none. */
  undoTarget?: () => string;
  /** «Вернуть» of a skipped row of the schedule card, as the «Предметы» page does it. */
  restore?: (subjectId: string, version: number) => Promise<RestoreCall>;
  /** Subjects were created, taken back or restored by a card: the pages read them again. */
  libraryChanged?: () => void;
  /** The core call of the `answer` cards; without it a card lists no sources. */
  answers?: AnswersApi;
  /** Opens the place of a source on «База знаний», by ids. */
  openSource?: (o: CitationOpening) => void;
  /** Puts a message text on the clipboard («Копировать»); absent: the message actions offer no copy. */
  copy?: (text: string) => Promise<void>;
  press(id: ButtonId): void;
  revise(wish: string): void;
  answer(answers: AnswerInput[]): void;
  /** «Отменить» of an action line. */
  undo(actionId: string): void;
  /** «Отменить всё» of the request. */
  undoAll(): void;
  /** What «Отменить» of the action did, if it was pressed. */
  undoNote(actionId: string): UndoNote | undefined;
}

/**
 * The page's own button an offer names, labelled from the template (never by the model). It calls `actions.offer`; until
 * the panel wires it nothing is drawn: a dead button helps nobody. A button the version does not have has no model at all.
 */
export function OfferButton(props: { offer: OfferModel; actions: CardActions; running?: boolean }) {
  const { offer, actions } = props;
  if (actions.offer === undefined) {
    return null;
  }
  // «Отменить» is a small secondary button: an icon. The other offers are the card's one main action and keep their words.
  const icon = offerIcon(offer.offer);
  if (icon !== null) {
    return <IconAction icon={icon} label={offer.label} busy={props.running === true} disabled={actions.busy === true} onClick={() => actions.offer?.(offer.offer)} />;
  }
  return (
    <Button size="sm" variant="secondary" disabled={actions.busy === true} onClick={() => actions.offer?.(offer.offer)}>
      {offer.label}
    </Button>
  );
}

/**
 * The offer button with the state of its press: off while the call runs (one click is one call), and the page's reason in
 * a small muted line beside it when the press did not work. The button stays usable after a refusal; after a job that is
 * done once (the undo of the latest action) it goes off.
 */
export function OfferControl(props: { offer: OfferModel; actions: CardActions }) {
  const [state, setState] = useState<{ busy: boolean; result: OfferResult | null }>({ busy: false, result: null });
  const running = useRef(false);
  const { actions } = props;
  const press = (offer: OfferView) => {
    if (running.current) {
      return;
    }
    running.current = true;
    setState({ busy: true, result: null });
    Promise.resolve()
      .then(() => actions.offer?.(offer))
      .then(
        (r) => setState({ busy: false, result: r ?? null }),
        () => setState({ busy: false, result: { ok: false, text: offerReasons.failed } }),
      )
      .finally(() => {
        running.current = false;
      });
  };
  return (
    <OfferControlView
      offer={props.offer}
      actions={{ ...actions, offer: actions.offer === undefined ? undefined : press }}
      busy={state.busy}
      result={state.result}
    />
  );
}

/** The offer button for a state of its press (no hooks: the tests render it directly). */
export function OfferControlView(props: { offer: OfferModel; actions: CardActions; busy: boolean; result: OfferResult | null }) {
  const { result } = props;
  const off: CardActions = { ...props.actions, busy: props.actions.busy === true || props.busy || result?.once === true };
  // The «Отменить» of an offer says what it would undo: the wording of the action line, until it is done.
  const target = props.offer.offer.action === "undo_action" && result?.once !== true ? (props.actions.undoTarget?.() ?? "") : "";
  return (
    <>
      <OfferButton offer={props.offer} actions={off} running={props.busy} />
      {target !== "" && props.actions.offer !== undefined && <span className="ag-act-note small muted">{target}</span>}
      {result !== null && result.text !== "" && (
        <span className="ag-act-note small muted" role="status">
          {result.text}
        </span>
      )}
    </>
  );
}

/** The buttons of a card: the main action keeps its word, the secondary ones are icons (cardButtonIcon). */
function Buttons(props: { buttons: ButtonModel[]; card: ButtonCard; busy?: boolean; onPress: (id: ButtonId) => void }) {
  if (props.buttons.length === 0) {
    return null;
  }
  // The words first (the first one is the primary), then the icons: a pencil between two words reads as a stray.
  const withIcon = props.buttons.map((b) => ({ b, icon: cardButtonIcon(b.id, props.card) }));
  return (
    <div className="ag-actions">
      {withIcon
        .filter((x) => x.icon === null)
        .map(({ b }, i) => (
          <Button key={b.id} size="sm" variant={i === 0 ? "primary" : "secondary"} disabled={props.busy} onClick={() => props.onPress(b.id)}>
            {b.label}
          </Button>
        ))}
      {withIcon.map(({ b, icon }) =>
        icon === null ? null : (
          <IconAction key={b.id} icon={icon} label={b.label} variant={b.id === "cancel" ? "danger" : "default"} disabled={props.busy} onClick={() => props.onPress(b.id)} />
        ),
      )}
    </div>
  );
}

function Lines(props: { items: string[]; ordered?: boolean }) {
  const Tag = props.ordered ? "ol" : "ul";
  return (
    <Tag className="ag-list">
      {props.items.map((t, i) => (
        <li key={i}>{t}</li>
      ))}
    </Tag>
  );
}

/** The assignments a reply names: title and subject are plain text nodes; a row opens the assignment page. */
export function AssignmentList(props: { items: AssignmentLineView[]; actions: CardActions }) {
  if (props.items.length === 0) {
    return null;
  }
  const now = props.actions.now ?? new Date();
  const subjects = props.actions.subjects ?? {};
  return (
    <ul className="ag-asg" aria-label={texts.assignmentsLabel}>
      {props.items.map((a, i) => {
        const r = assignmentRow(a, subjects, now);
        return (
          <li key={`${a.id}:${i}`}>
            <button type="button" className="ag-asg-row" onClick={() => props.actions.openAssignment?.(r.id)}>
              <span className="ag-asg-main">
                <span className="ag-asg-t">{r.title}</span>
                {r.subject !== "" && <span className="ag-asg-s small muted">{r.subject}</span>}
              </span>
              {r.due !== "" && <span className={`ag-asg-due small num${r.overdue ? " ag-asg-late" : ""}`}>{r.due}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/** The marker in front of a step of an executing plan: an icon or the dot, and a text for readers of the screen. */
function StepMarker(props: { mark: StepMark }) {
  switch (props.mark) {
    case "done":
      return <Icon name="ok" className="ag-plan-ok" />;
    case "failed":
      return <Icon name="x" className="ag-plan-bad" />;
    case "current":
      return <span className="ag-dot ag-plan-dot" aria-hidden="true" />;
    default:
      return null;
  }
}

function StepMarkText(props: { mark: StepMark }) {
  switch (props.mark) {
    case "done":
      return (
        <>
          {" "}
          <span className="ag-vh">{planTexts.done}</span>
        </>
      );
    case "failed":
      return (
        <>
          {" "}
          <span className="ag-plan-bad">{planTexts.failed}</span>
        </>
      );
    case "current":
      return (
        <>
          {" "}
          <span className="ag-vh">{planTexts.running}</span>
        </>
      );
    default:
      return null;
  }
}

/**
 * The steps of a plan. A plan of more than 8 steps shows the first 8 and a quiet «Ещё N шагов»; opened, the list
 * scrolls inside the card. A failed step beyond the first 8 opens the list (and takes the toggle away).
 */
export function PlanSteps(props: { steps: readonly string[]; marks?: readonly StepMark[]; expanded: boolean; onToggle: () => void }) {
  const listId = useId();
  const marks = props.marks ?? [];
  const w = planWindow(props.steps.length, props.expanded, failedStepIndex(marks));
  if (props.steps.length === 0) {
    return null;
  }
  const list = (
    <ol id={listId} className={marks.length > 0 ? "ag-list ag-plan-steps" : "ag-list"}>
      {props.steps.slice(0, w.shown).map((t, i) => (
        <li key={i} className={marks[i] === "failed" ? "ag-plan-fail" : undefined}>
          {marks.length > 0 && <StepMarker mark={marks[i] ?? "pending"} />}
          {t}
          {marks.length > 0 && <StepMarkText mark={marks[i] ?? "pending"} />}
        </li>
      ))}
    </ol>
  );
  if (props.steps.length <= planCollapseLimit) {
    return list;
  }
  return (
    <>
      <div className={w.scroll ? "ag-plan-scroll" : undefined} {...(w.scroll ? { tabIndex: 0, role: "region", "aria-label": planTexts.stepsLabel } : {})}>
        {list}
      </div>
      {w.canToggle && (
        <button type="button" className="ag-plan-more" aria-expanded={w.expanded} aria-controls={listId} onClick={props.onToggle}>
          {w.expanded ? planTexts.collapse : moreStepsLabel(w.hidden)}
        </button>
      )}
    </>
  );
}

function PlanCard(props: { card: Extract<CardModel, { type: "plan" }>; actions: CardActions }) {
  const { card } = props;
  const [revising, setRevising] = useState(false);
  const [wish, setWish] = useState("");
  const [expanded, setExpanded] = useState(false);
  const wishId = useId();
  const head = <h3 className="ag-card-h">{card.caption}</h3>;
  return (
    <section className="ag-card">
      {card.progress ? (
        <div className="ag-plan-head">
          {head}
          <span className="ag-plan-prog small muted">{progressLabel(card.progress)}</span>
        </div>
      ) : (
        head
      )}
      <p className="ag-lead">{card.summary}</p>
      <PlanSteps steps={card.steps} marks={card.marks} expanded={expanded} onToggle={() => setExpanded((e) => !e)} />
      {card.notes.map((n, i) => (
        <p key={i} className="ag-note">
          <Icon name="info" />
          <span>{n}</span>
        </p>
      ))}
      {revising ? (
        <form
          className="ag-revise"
          onSubmit={(e) => {
            e.preventDefault();
            const w = wish.trim();
            if (w !== "") {
              props.actions.revise(w);
              setRevising(false);
              setWish("");
            }
          }}
        >
          <label className="small muted" htmlFor={wishId}>
            {texts.reviseLabel}
          </label>
          <input id={wishId} className="ag-inp" value={wish} maxLength={300} onChange={(e) => setWish(e.target.value)} />
          <Button size="sm" variant="primary" type="submit" disabled={wish.trim() === "" || props.actions.busy}>
            {texts.reviseSend}
          </Button>
        </form>
      ) : (
        <Buttons
          buttons={card.buttons}
          card="plan"
          busy={props.actions.busy}
          onPress={(id) => {
            if (id === "revise") {
              setRevising(true);
            } else {
              props.actions.press(id);
            }
          }}
        />
      )}
    </section>
  );
}

function QuestionsCard(props: { card: Extract<CardModel, { type: "questions" }>; actions: CardActions }) {
  const { card } = props;
  const [picked, setPicked] = useState<Record<number, number>>({});
  const [typed, setTyped] = useState<Record<number, string>>({});
  const answers = collectAnswers(card.questions, picked, typed);
  return (
    <section className="ag-card ag-ask">
      <h3 className="ag-card-h">
        <Icon name="help" />
        {card.caption}
      </h3>
      {card.questions.map((q, i) => (
        <fieldset key={i} className="ag-q" disabled={!card.active}>
          <legend>{q.text}</legend>
          {q.kind === "choice" ? (
            q.options.map((o, j) => (
              <label key={j} className="ag-opt">
                <input
                  type="radio"
                  name={`ag-q-${i}`}
                  checked={(picked[i] ?? q.default) === j}
                  onChange={() => setPicked({ ...picked, [i]: j })}
                />
                <span>{o}</span>
              </label>
            ))
          ) : (
            <input
              className="ag-inp"
              aria-label={texts.textAnswerLabel}
              value={typed[i] ?? ""}
              maxLength={300}
              onChange={(e) => setTyped({ ...typed, [i]: e.target.value })}
            />
          )}
        </fieldset>
      ))}
      {card.active && (
        <div className="ag-actions">
          <Button size="sm" variant="primary" disabled={answers === null || props.actions.busy} onClick={() => answers && props.actions.answer(answers)}>
            {card.submit}
          </Button>
          <Button size="sm" variant="ghost" disabled={props.actions.busy} onClick={() => props.actions.answer(skipAnswers(card.questions))}>
            {card.skip}
          </Button>
        </div>
      )}
    </section>
  );
}

/**
 * `typing`: the reply is being typed above, so the waiting row keeps the elapsed time and «Стоп» (the text is the progress).
 * `startedAt`: when the request started running, for the timer; `clock` is the time source of the timer (tests).
 */
function Card(props: { card: CardModel; actions: CardActions; typing?: boolean; startedAt?: number; clock?: () => number }) {
  const { card } = props;
  switch (card.type) {
    case "plan":
      return <PlanCard card={card} actions={props.actions} />;
    case "questions":
      return <QuestionsCard card={card} actions={props.actions} />;
    case "progress":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">
            <span className="ag-dot" aria-hidden="true" />
            {card.caption}
            {props.startedAt !== undefined && <Elapsed since={props.startedAt} clock={props.clock} />}
          </h3>
          <Buttons buttons={card.buttons} card="progress" busy={props.actions.busy} onPress={props.actions.press} />
        </section>
      );
    case "waiting":
      return (
        <div className="ag-think">
          {!props.typing && <Waiting />}
          {props.startedAt !== undefined && <Elapsed since={props.startedAt} clock={props.clock} />}
          <Buttons buttons={card.buttons} card="waiting" busy={props.actions.busy} onPress={props.actions.press} />
        </div>
      );
    case "result":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{card.caption}</h3>
          {card.artifact ? (
            <p className="ag-file">
              <Icon name="doc" />
              <span className="ag-file-kind">{card.artifact.kind}</span>
              <span className="ag-file-name">{card.artifact.name}</span>
            </p>
          ) : (
            <p className="muted">{card.fallback}</p>
          )}
        </section>
      );
    case "answer":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{card.caption}</h3>
          {/* card.text is the fixed line for a card that cannot read its answer (no core call or no id). */}
          <AnswerCard api={props.actions.answers} answerId={card.answerId} fallback={card.text} onOpen={props.actions.openSource} />
        </section>
      );
    case "reply":
      return (
        <section className="ag-card">
          <p className="ag-text">{card.text}</p>
          <AssignmentList items={card.assignments} actions={props.actions} />
        </section>
      );
    case "cannot_do":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{card.caption}</h3>
          {card.yourPart.length > 0 && (
            <>
              <p className="ag-sub">{card.yourTitle}</p>
              <Lines items={card.yourPart} />
            </>
          )}
          {card.ourPart.length > 0 && (
            <>
              <p className="ag-sub">{card.ourTitle}</p>
              <Lines items={card.ourPart} />
            </>
          )}
        </section>
      );
    case "confirm":
      if (card.target !== null && props.actions.confirm !== undefined) {
        return <ConfirmCard key={`${card.target.kind}:${card.target.id}`} api={props.actions.confirm.api} deleteApi={props.actions.confirm.deleteApi} target={card.target} />;
      }
      return (
        <section className="ag-card ag-confirm">
          <h3 className="ag-card-h">{card.caption}</h3>
          <p className="muted">{card.text}</p>
        </section>
      );
    case "work":
      return props.actions.turnkey ? <WorkWritingCard key={card.workId} api={props.actions.turnkey} workId={card.workId} /> : null;
    case "action": {
      const note = props.actions.undoNote(card.actionId);
      return (
        <div className="ag-act">
          <Icon name={note?.done ? "back" : "ok"} />
          <span className="ag-act-tx">
            {note?.done ? card.undoneText : card.assignment ? addedAssignmentText(card.assignment, props.actions.subjects ?? {}, props.actions.now ?? new Date()) : card.text}
            {/* The saved text is the student's own: plain lines, muted, under the sentence; gone once undone. */}
            {!note?.done && card.detail.map((t, i) => (
              <span key={i} className="ag-act-ex small muted">
                {t}
              </span>
            ))}
          </span>
          {!note?.done && card.assignment && props.actions.openAssignment && (
            <IconAction icon="chevr" label={openTaskLabel(card.assignment.title)} tipText={summaryToolTexts.openAssignment} onClick={() => card.assignment && props.actions.openAssignment?.(card.assignment.id)} />
          )}
          {card.offer !== null && <OfferControl offer={card.offer} actions={props.actions} />}
          {/* «Остановить» was pressed: the line says «Запись остановлена» and offers a new recording. */}
          {note?.done === true && card.afterUndoOffer !== null && <OfferControl offer={card.afterUndoOffer} actions={props.actions} />}
          {!note?.done && card.undo.shown && (
            <IconAction
              icon={undoIcon(card.undo.kind)}
              variant={card.undo.kind === "stop" ? "danger" : "default"}
              label={note?.busy ? card.undo.busyLabel : card.undo.label}
              busy={note?.busy === true}
              disabled={!card.undo.enabled}
              onClick={() => props.actions.undo(card.actionId)}
            />
          )}
          {note && !note.done && !note.busy && note.text !== "" && (
            <span className="ag-act-note small muted" role="status">
              {note.text}
            </span>
          )}
        </div>
      );
    }
    case "schedule":
      // The check of the «Предметы» review inside the chat; the card reads its rows from the core by the token.
      return props.actions.confirm !== undefined ? (
        <ScheduleSubjectsCard
          key={card.token}
          api={props.actions.confirm.api}
          token={card.token}
          subjects={props.actions.subjects}
          restore={props.actions.restore}
          onChanged={props.actions.libraryChanged}
        />
      ) : (
        <section className="ag-card">
          <p className="muted">{texts.confirmPending}</p>
        </section>
      );
    case "offer":
      // One button with the page's own label and no heading; until the panel wires the press there is nothing to show.
      return props.actions.offer === undefined ? null : (
        <div className="ag-actions ag-offer">
          <OfferControl offer={card.offer} actions={props.actions} />
        </div>
      );
    case "unknown":
      // A card kind of a newer core: said honestly and quietly, with nothing to press.
      return (
        <div className="ag-stop" role="status">
          <Icon name="info" />
          <span className="ag-act-tx">{card.text}</span>
        </div>
      );
    case "stopped":
    case "problem":
      return (
        <div className="ag-stop" role="status">
          <Icon name={card.type === "stopped" ? "warn" : "info"} />
          <span className="ag-act-tx">{card.text}</span>
          {card.buttons.map((b) => {
            const icon = cardButtonIcon(b.id, card.type);
            return icon !== null ? (
              <IconAction key={b.id} icon={icon} label={b.label} variant={b.id === "cancel" ? "danger" : "default"} disabled={props.actions.busy} onClick={() => props.actions.press(b.id)} />
            ) : (
              <Button key={b.id} size="sm" variant="ghost" disabled={props.actions.busy} onClick={() => props.actions.press(b.id)}>
                {b.label}
              </Button>
            );
          })}
        </div>
      );
  }
}

/** Unchecked answer text while it is read: plain text nodes only, muted, the waiting dots after the last line. */
export function AnswerDraft(props: { draft: DraftView; dots: boolean }) {
  return (
    <div className="ag-draft" aria-live="off">
      <p className="ag-draft-cap small muted">{texts.draftCaption}</p>
      {props.draft.texts.map((t, i) => (
        <p key={i} className="ag-draft-line muted">
          {t}
        </p>
      ))}
      {props.dots && <Waiting />}
    </div>
  );
}

/**
 * The reply being typed: the same card as the final reply (no caption, no jump when it replaces this one), the whole text
 * so far as one plain text node, a cursor after it. Silent for screen readers: the text is never in a live region, the
 * request's own status line («Ответ печатается…» / «Ответ готов») tells that it is typing and that it is ready.
 */
export function ReplyDraft(props: { text: string }) {
  return (
    <section className="ag-card ag-reply-draft" aria-live="off">
      <p className="ag-text">
        {props.text}
        <span className="ag-cursor" aria-hidden="true" />
      </p>
    </section>
  );
}

/**
 * What the student had read when the request ended: muted plain text, no cursor. UI only. After «Стоп» a small caption says
 * so; after a break-off (`caption` false) the stop line under it is the only words.
 */
export function StoppedReply(props: { text: string; caption?: boolean }) {
  return (
    <section className="ag-card ag-reply-kept">
      <p className="ag-text muted">{props.text}</p>
      {props.caption !== false && <p className="ag-draft-cap small muted">{texts.replyStopped}</p>}
    </section>
  );
}

/**
 * The one polite status of a request, in place from the start of a running one so its later texts are announced; it
 * never carries the reply itself. It stays mounted for the whole life of the request, so it remembers that a draft was
 * shown: «Ответ готов» only then, once the final reply card has arrived.
 */
function ReplyStatus(props: { typing: boolean; replyCard: boolean; running: boolean }) {
  const wasTyping = useRef(false);
  const status = replyStatus({ ...props, wasTyping: wasTyping.current });
  if (props.typing) {
    wasTyping.current = true;
  }
  return status === null ? null : (
    <span className="ag-vh" role="status">
      {status}
    </span>
  );
}

/** One request of the thread: the student's text, then the cards. `cut`: its reply was typing when it stopped. */
export function AgentRequest(props: {
  model: EntryModel;
  actions: CardActions;
  drafts?: readonly DraftView[];
  cut?: CutReply;
  underUser?: ReactNode;
  /** When the request started running (ms): the elapsed time shows beside the dots. Absent: no timer. */
  startedAt?: number;
  /** The time source of the timer; the current time when absent. */
  clock?: () => number;
}) {
  const { model } = props;
  const { reply, answers } = splitDrafts(props.drafts ?? []);
  // The kept text goes right above the first stop line (or the problem line); with neither, after the cards: it never vanishes.
  const keptText = props.cut?.text ?? "";
  const lineAt = model.cards.findIndex((c) => c.type === "stopped");
  const problemAt = model.cards.findIndex((c) => c.type === "problem");
  const keptAt = keptText === "" ? -1 : lineAt >= 0 ? lineAt : problemAt >= 0 ? problemAt : model.cards.length;
  const kept = <StoppedReply text={keptText} caption={props.cut?.reason === "user"} />;
  // «Отменить всё» only when there is more than one action left to undo.
  const open = undoableActions(model.cards).filter((id) => !props.actions.undoNote(id)?.done);
  const hasWaitingCard = model.cards.some((c) => c.type === "waiting");
  return (
    <article className="ag-req">
      {model.userText !== "" && <p className="ag-user">{model.userText}</p>}
      <MessagePills pills={model.mentions} onOpen={props.actions.openMention} />
      <UserActions text={model.userText} copy={props.actions.copy}>
        {props.underUser}
      </UserActions>
      {model.waiting && (model.queued || props.startedAt !== undefined ? (
        <div className="ag-think">
          {model.queued ? <Waiting label={texts.waitingQueued} queued /> : <Waiting />}
          {model.queued ? (
            <>
              <span className="ag-queued small muted">{texts.queued}</span>
              {/* The running request's stop, on the row of the one that waits for it. */}
              <IconAction icon="stop" label={texts.stopPrevious} disabled={props.actions.busy === true} onClick={() => props.actions.press("stop")} />
            </>
          ) : (
            props.startedAt !== undefined && <Elapsed since={props.startedAt} clock={props.clock} />
          )}
        </div>
      ) : (
        <Waiting />
      ))}
      {model.callLine !== "" && (
        <p className="ag-callline small muted" role="status">
          {model.callLine}
        </p>
      )}
      {model.cards.map((c, i) =>
        // Kind and caption (the question round) are in the key so a new card does not inherit the typed state of the old one.
        // The draft goes before the waiting card, so its dots (with «Стоп») stay after the last line.
        c.type === "waiting" ? null : (
          <Fragment key={`${c.type}:${"caption" in c ? c.caption : ""}:${i}`}>
            {i === keptAt && kept}
            <Card card={c} actions={props.actions} startedAt={props.startedAt} clock={props.clock} />
          </Fragment>
        ),
      )}
      {keptAt === model.cards.length && kept}
      <ReplyStatus typing={reply !== null} replyCard={model.cards.some((c) => c.type === "reply")} running={hasWaitingCard} />
      {/* The reply sits where its final card will be, before the answer drafts, so it does not jump over them. */}
      {reply !== null && <ReplyDraft text={replyDraftText(reply)} />}
      {answers.map((d, i) => (
        <AnswerDraft key={d.callId || i} draft={d} dots={!hasWaitingCard && reply === null && i === answers.length - 1} />
      ))}
      {model.cards.map((c, i) => (c.type === "waiting" ? <Card key={`w:${i}`} card={c} actions={props.actions} typing={reply !== null} startedAt={props.startedAt} clock={props.clock} /> : null))}
      {/* No copy while the reply is still typing: the final card replaces the draft. */}
      <AgentActions
        text={reply === null ? replyCopyText(model.cards) : ""}
        copy={props.actions.copy}
        rerun={model.rerun}
        busy={props.actions.busy === true}
        onRerun={() => props.actions.press("rerun")}
      />
      {open.length > 1 && (
        <div className="ag-actions">
          <Button size="sm" variant="ghost" disabled={open.some((id) => props.actions.undoNote(id)?.busy)} onClick={props.actions.undoAll}>
            {texts.undoAll}
          </Button>
        </div>
      )}
    </article>
  );
}
