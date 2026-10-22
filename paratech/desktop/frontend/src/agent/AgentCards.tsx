import { useId, useState } from "react";
import type { AnswerInput } from "../bindings/agent.js";
import { Button } from "../ui/Button.js";
import { Icon } from "../ui/Icon.js";
import type { AgentConfirmApi } from "../bindings/agentConfirm.js";
import type { RecordingDeleteApi } from "../bindings/recordingDelete.js";
import { ConfirmCard } from "./ConfirmCard.js";
import {
  type ButtonId,
  type ButtonModel,
  type CardModel,
  collectAnswers,
  type EntryModel,
  skipAnswers,
  texts,
  undoableActions,
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

function Buttons(props: { buttons: ButtonModel[]; busy?: boolean; onPress: (id: ButtonId) => void }) {
  if (props.buttons.length === 0) {
    return null;
  }
  return (
    <div className="ag-actions">
      {props.buttons.map((b, i) => (
        <Button key={b.id} size="sm" variant={i === 0 ? "primary" : "secondary"} disabled={props.busy} onClick={() => props.onPress(b.id)}>
          {b.label}
        </Button>
      ))}
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

function PlanCard(props: { card: Extract<CardModel, { type: "plan" }>; actions: CardActions }) {
  const { card } = props;
  const [revising, setRevising] = useState(false);
  const [wish, setWish] = useState("");
  const wishId = useId();
  return (
    <section className="ag-card">
      <h3 className="ag-card-h">{card.caption}</h3>
      <p className="ag-lead">{card.summary}</p>
      {card.steps.length > 0 && <Lines items={card.steps} ordered />}
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
      <h3 className="ag-card-h">{card.caption}</h3>
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

function Card(props: { card: CardModel; actions: CardActions }) {
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
          </h3>
          <Buttons buttons={card.buttons} busy={props.actions.busy} onPress={props.actions.press} />
        </section>
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
          <p>{card.text}</p>
        </section>
      );
    case "reply":
      return (
        <section className="ag-card">
          <h3 className="ag-card-h">{card.caption}</h3>
          <p className="ag-text">{card.text}</p>
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
    case "action": {
      const note = props.actions.undoNote(card.actionId);
      return (
        <div className="ag-act">
          <Icon name={note?.done ? "back" : "ok"} />
          <span className="ag-act-tx">{note?.done ? texts.undoDone : card.text}</span>
          {!note?.done && (
            <Button size="sm" variant="ghost" disabled={!card.undo.enabled || note?.busy === true} onClick={() => props.actions.undo(card.actionId)}>
              {note?.busy ? texts.undoBusy : card.undo.label}
            </Button>
          )}
          {note && !note.done && !note.busy && note.text !== "" && (
            <span className="ag-act-note small muted" role="status">
              {note.text}
            </span>
          )}
        </div>
      );
    }
    case "stopped":
    case "problem":
      return (
        <div className="ag-stop" role="status">
          <Icon name={card.type === "stopped" ? "warn" : "info"} />
          <span className="ag-act-tx">{card.text}</span>
          {card.buttons.map((b) => (
            <Button key={b.id} size="sm" variant="ghost" disabled={props.actions.busy} onClick={() => props.actions.press(b.id)}>
              {b.label}
            </Button>
          ))}
        </div>
      );
  }
}

/** One request of the thread: the student's text, then the cards. */
export function AgentRequest(props: { model: EntryModel; actions: CardActions }) {
  const { model } = props;
  // «Отменить всё» only when there is more than one action left to undo.
  const open = undoableActions(model.cards).filter((id) => !props.actions.undoNote(id)?.done);
  return (
    <article className="ag-req">
      {model.userText !== "" && <p className="ag-user">{model.userText}</p>}
      {model.callLine !== "" && (
        <p className="ag-callline small muted" role="status">
          {model.callLine}
        </p>
      )}
      {model.cards.map((c, i) => (
        // Kind and caption (the question round) are in the key so a new card does not inherit the typed state of the old one.
        <Card key={`${c.type}:${"caption" in c ? c.caption : ""}:${i}`} card={c} actions={props.actions} />
      ))}
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
