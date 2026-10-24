import { useId, useState, type ReactNode } from "react";
// The .js extensions let the Node test runner load the cards.
import type { TurnkeyApi, TurnkeyWork, WorkCardFields } from "../bindings/turnkey.js";
import {
  type AnswerDraft,
  answeredLine,
  askStepOutcome,
  cardDraft,
  cardRows,
  cardValue,
  checkCard,
  cleanCard,
  confirmCard,
  defaultTextHint,
  initialDrafts,
  isCardEdited,
  isFieldEdited,
  isLastRound,
  isPartial,
  stepQuestionCheck,
  toAnswers,
  turnkeyLimits,
  turnkeyTexts,
  workTypeOptions,
} from "../turnkeyView.js";
import type { StepResult } from "../workView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

// Questions, assumptions, requirements, steps, pitfalls and answers about a step were written by the reader or the
// student (untrusted). They reach the page as React text nodes only: no element, link or markdown is built from them,
// so markup, URLs, emails and paths stay visible text. Nothing is kept in browser storage.

/** What the work card hands to the cards below: the busy flag and the guarded calls of WorkBody. */
export interface CardContext {
  work: TurnkeyWork;
  api: TurnkeyApi;
  busy: boolean;
  /** Runs one call at a time; a failure becomes the error line of the card. */
  run(call: () => Promise<void>): Promise<void>;
  /** Applies the reply of a step; after a failure the error stays on the card and the work is re-read. */
  step(result: Promise<StepResult>): Promise<void>;
  onWork(w: TurnkeyWork): void;
  /** The error line of the last call ("" without one). */
  errorLine: ReactNode;
}

/** A list of reader lines as plain list items. */
function TextList(props: { items: readonly string[] }) {
  return (
    <ul className="ag-list">
      {props.items.map((t, i) => (
        <li key={i} className="ag-text">
          {t}
        </li>
      ))}
    </ul>
  );
}

// ---- Reader call that waits or failed ----

export function TriageCard(props: { ctx: CardContext; caption: string; line: string; problem: string }) {
  const { ctx } = props;
  return (
    <section className="ag-card">
      <h3 className="ag-card-h">
        <span className="ag-dot" aria-hidden="true" />
        {props.caption}
      </h3>
      {props.problem === "" ? (
        <p role="status">{props.line}</p>
      ) : (
        <ResultLine kind="wait" role="status">
          {props.problem}
        </ResultLine>
      )}
      {ctx.errorLine}
      {props.problem !== "" && (
        <div className="ag-actions">
          <Button size="sm" variant="primary" disabled={ctx.busy} onClick={() => void ctx.run(async () => ctx.onWork(await ctx.api.retry(ctx.work.id)))}>
            {turnkeyTexts.retry}
          </Button>
        </div>
      )}
    </section>
  );
}

export function FailedCard(props: { caption: string; text: string; message: string }) {
  return (
    <section className="ag-card">
      <h3 className="ag-card-h">{props.caption}</h3>
      <p>{props.text}</p>
      {props.message !== "" && <p className="small muted">{props.message}</p>}
    </section>
  );
}

// ---- Questions ----

/** The questions of a round: an input per question, «Ответить» and «Пропустить». The parent re-mounts it per round. */
export function QuestionsCard(props: { ctx: CardContext }) {
  const { ctx } = props;
  const { work } = ctx;
  const [drafts, setDrafts] = useState<AnswerDraft[]>(() => initialDrafts(work.questions));
  const base = useId();
  const set = (i: number, patch: Partial<AnswerDraft>) => setDrafts((ds) => ds.map((d, j) => (j === i ? { ...d, ...patch } : d)));

  return (
    <section className="ag-card ag-ask">
      <h3 className="ag-card-h">{turnkeyTexts.questionsCaption}</h3>
      <p className="small muted">{turnkeyTexts.roundLine(work.questionRound)}</p>
      {isLastRound(work) && <p className="small muted">{turnkeyTexts.lastRoundNote}</p>}

      {work.answered.length > 0 && (
        <details className="ag-wk-sec">
          <summary>{turnkeyTexts.answeredCaption}</summary>
          <ul className="ag-list small">
            {work.answered.map((a, i) => (
              <li key={i} className="ag-text">
                {a.question} — {answeredLine(a)}
              </li>
            ))}
          </ul>
        </details>
      )}

      {work.questions.map((q, i) => {
        const id = `${base}-${i}`;
        const d = drafts[i] ?? { option: 0, text: "" };
        return (
          <div key={i} className="ag-q">
            {q.kind === "choice" ? (
              <fieldset className="ag-q-set" disabled={ctx.busy}>
                <legend className="ag-text">{q.text}</legend>
                {q.options.map((o, j) => (
                  <label key={j} className="ag-q-opt">
                    <input type="radio" name={id} checked={d.option === j} onChange={() => set(i, { option: j })} />
                    <span className="ag-text">
                      {o}
                      {j === q.default && <span className="small muted"> · {turnkeyTexts.defaultOption}</span>}
                    </span>
                  </label>
                ))}
              </fieldset>
            ) : (
              <>
                <label className="ag-text" htmlFor={id}>
                  {q.text}
                </label>
                <input
                  id={id}
                  className="ag-inp"
                  value={d.text}
                  maxLength={turnkeyLimits.answerMax}
                  placeholder={defaultTextHint(q)}
                  disabled={ctx.busy}
                  onChange={(e) => set(i, { text: e.target.value })}
                />
              </>
            )}
            {q.why !== "" && (
              <p className="small muted ag-text">
                {turnkeyTexts.why}: {q.why}
              </p>
            )}
          </div>
        );
      })}

      {ctx.errorLine}
      <div className="ag-actions">
        <Button size="sm" variant="primary" disabled={ctx.busy} onClick={() => void ctx.run(async () => ctx.onWork(await ctx.api.answerQuestions(work.id, toAnswers(work.questions, drafts))))}>
          {turnkeyTexts.answer}
        </Button>
        <Button size="sm" variant="ghost" disabled={ctx.busy} onClick={() => void ctx.run(async () => ctx.onWork(await ctx.api.skipQuestions(work.id)))}>
          {turnkeyTexts.skip}
        </Button>
      </div>
    </section>
  );
}

// ---- «Как понято задание» ----

export function UnderstandingCard(props: { ctx: CardContext }) {
  const { ctx } = props;
  const { work } = ctx;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<WorkCardFields>(() => cardDraft(work.card));
  const base = useId();

  const check = checkCard(draft);
  const edited = isCardEdited(draft, work.card);
  const partial = isPartial(work);

  return (
    <section className="ag-card">
      <h3 className="ag-card-h">{turnkeyTexts.cardCaption}</h3>
      <p className="small muted">{turnkeyTexts.cardHint}</p>

      {!editing ? (
        <dl className="ag-kv">
          {cardRows.map((row) => (
            <div key={row.key} className="ag-kv-row">
              <dt className="small muted">{row.label}</dt>
              <dd className="ag-text">
                {cardValue(work.card, row.key)}
                {isFieldEdited(work, row) && <span className="ag-wk-verify"> · {turnkeyTexts.edited}</span>}
              </dd>
            </div>
          ))}
        </dl>
      ) : (
        <div className="ag-wk-editor">
          {cardRows.map((row) => {
            const id = `${base}-${row.key}`;
            return (
              <div key={row.key} className="ag-kv-row">
                <label className="small muted" htmlFor={id}>
                  {row.label}
                </label>
                {row.key === "workType" ? (
                  <select id={id} className="ag-inp" value={draft.workType} disabled={ctx.busy} aria-invalid={check.bad.workType || undefined} onChange={(e) => setDraft({ ...draft, workType: e.target.value })}>
                    {!workTypeOptions.some((o) => o.value === draft.workType) && <option value={draft.workType}>{turnkeyTexts.notSet}</option>}
                    {workTypeOptions.map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    id={id}
                    className="ag-inp"
                    value={draft[row.key]}
                    aria-invalid={check.bad[row.key] || undefined}
                    disabled={ctx.busy}
                    onChange={(e) => setDraft({ ...draft, [row.key]: e.target.value })}
                  />
                )}
              </div>
            );
          })}
          {!check.ok && check.problem !== "" && (
            <p className="small muted" role="status">
              {check.problem}
            </p>
          )}
        </div>
      )}

      {partial && (
        <div className="ag-wk-sec">
          <p className="ag-sub">{turnkeyTexts.partialCaption}</p>
          {work.feasibilityReason !== "" && <p className="ag-text small">{work.feasibilityReason}</p>}
          {work.helper !== null && work.helper.placeholders.length > 0 && (
            <>
              <p className="ag-sub">{turnkeyTexts.placeholders}</p>
              <TextList items={work.helper.placeholders} />
            </>
          )}
        </div>
      )}

      {work.assumptions.length > 0 && (
        <div className="ag-wk-sec">
          <p className="ag-sub">{turnkeyTexts.assumptions}</p>
          <p className="small muted">{turnkeyTexts.assumptionsHint}</p>
          <TextList items={work.assumptions} />
        </div>
      )}

      {ctx.errorLine}
      <div className="ag-actions">
        <Button size="sm" variant="primary" disabled={ctx.busy || (editing && !check.ok)} onClick={() => void ctx.run(() => ctx.step(confirmCard(ctx.api, work.id, draft, editing && edited)))}>
          {turnkeyTexts.confirm}
        </Button>
        {!editing && (
          <Button size="sm" variant="secondary" disabled={ctx.busy} onClick={() => setEditing(true)}>
            {turnkeyTexts.edit}
          </Button>
        )}
        {editing && (
          <>
            <Button
              size="sm"
              variant="secondary"
              disabled={ctx.busy || !check.ok || !edited}
              onClick={() =>
                void ctx.run(async () => {
                  const w = await ctx.api.editUnderstanding(work.id, cleanCard(draft));
                  ctx.onWork(w);
                  setDraft(cardDraft(w.card));
                })
              }
            >
              {turnkeyTexts.save}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={ctx.busy}
              onClick={() => {
                setDraft(cardDraft(work.card));
                setEditing(false);
              }}
            >
              {turnkeyTexts.cancel}
            </Button>
          </>
        )}
      </div>
    </section>
  );
}

// ---- Helper mode ----

/** One step of the helper with «Спросить по шагу». The answer is plain text; «пока недоступны» is a calm note. */
function HelperStep(props: { ctx: CardContext; index: number; text: string }) {
  const { ctx, index } = props;
  const [open, setOpen] = useState(false);
  const [question, setQuestion] = useState("");
  const [answer, setAnswer] = useState("");
  const [note, setNote] = useState("");
  const [failure, setFailure] = useState("");
  const [asking, setAsking] = useState(false);
  const id = useId();
  const check = stepQuestionCheck(question);

  const ask = async () => {
    if (!check.ok || asking) {
      return;
    }
    setAsking(true);
    setAnswer("");
    setNote("");
    setFailure("");
    const r = await askStepOutcome(ctx.api, ctx.work.id, index, check.text);
    if (r.kind === "answer") {
      setAnswer(r.text);
    } else if (r.kind === "unavailable") {
      setNote(r.text);
    } else {
      setFailure(r.text);
    }
    setAsking(false);
  };

  return (
    <li>
      <span className="ag-text">{props.text}</span>
      <div className="ag-actions">
        <Button size="sm" variant="ghost" aria-expanded={open} disabled={asking} onClick={() => setOpen(!open)}>
          {open ? turnkeyTexts.askClose : turnkeyTexts.askStep}
        </Button>
      </div>
      {open && (
        <div className="ag-revise">
          <label className="small muted" htmlFor={id}>
            {turnkeyTexts.askLabel}
          </label>
          <input id={id} className="ag-inp" value={question} maxLength={turnkeyLimits.stepQuestionMax} disabled={asking} onChange={(e) => setQuestion(e.target.value)} />
          {question !== "" && !check.ok && (
            <p className="small muted" role="status">
              {check.problem}
            </p>
          )}
          <div className="ag-actions">
            <Button size="sm" variant="primary" disabled={asking || !check.ok} onClick={() => void ask()}>
              {turnkeyTexts.askSend}
            </Button>
          </div>
          {note !== "" && (
            <p className="ag-note" role="status">
              <Icon name="info" />
              <span>{note}</span>
            </p>
          )}
          {failure !== "" && (
            <ResultLine kind="fail" role="alert">
              {failure}
            </ResultLine>
          )}
          {answer !== "" && (
            <>
              <p className="ag-sub">{turnkeyTexts.answerLabel}</p>
              <p className="ag-text" role="status">
                {answer}
              </p>
            </>
          )}
        </div>
      )}
    </li>
  );
}

export function HelperCard(props: { ctx: CardContext }) {
  const { ctx } = props;
  const { work } = ctx;
  const h = work.helper;
  const empty = h === null || (h.steps.length === 0 && h.submit === "" && h.pitfalls.length === 0 && h.readyParts.length === 0 && h.placeholders.length === 0);

  return (
    <section className="ag-card">
      <h3 className="ag-card-h">{turnkeyTexts.helperCaption}</h3>
      <p className="small muted">{turnkeyTexts.helperHint}</p>
      {work.feasibilityReason !== "" && (
        <p className="ag-text">
          <span className="ag-sub">{turnkeyTexts.reason}: </span>
          {work.feasibilityReason}
        </p>
      )}
      {empty || h === null ? (
        <p className="small muted">{turnkeyTexts.helperEmpty}</p>
      ) : (
        <>
          {h.steps.length > 0 && (
            <>
              <p className="ag-sub">{turnkeyTexts.steps}</p>
              <ol className="ag-list">
                {h.steps.map((s, i) => (
                  <HelperStep key={i} ctx={ctx} index={i} text={s} />
                ))}
              </ol>
            </>
          )}
          {h.submit !== "" && (
            <>
              <p className="ag-sub">{turnkeyTexts.submit}</p>
              <p className="ag-text">{h.submit}</p>
            </>
          )}
          {h.pitfalls.length > 0 && (
            <>
              <p className="ag-sub">{turnkeyTexts.pitfalls}</p>
              <TextList items={h.pitfalls} />
            </>
          )}
          {h.readyParts.length > 0 && (
            <>
              <p className="ag-sub">{turnkeyTexts.readyParts}</p>
              <TextList items={h.readyParts} />
            </>
          )}
          {h.placeholders.length > 0 && (
            <>
              <p className="ag-sub">{turnkeyTexts.placeholdersHelper}</p>
              <TextList items={h.placeholders} />
            </>
          )}
        </>
      )}
      {ctx.errorLine}
    </section>
  );
}
