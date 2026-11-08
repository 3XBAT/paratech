import { useEffect, useState } from "react";
// The .js extensions let the Node test runner load the compiled component.
import type { AnswerClaim, AnswersApi, AnswerView } from "../bindings/answers.js";
import type { CitationOpening } from "../knowledgeView.js";
import { answerParts, answerTexts, clampClaim } from "../answerView.js";
import { AnswerSourcesView } from "./AnswerSources.js";

// The body of the answer card (BUG-20261108-agent-cards): the claims, then what the lessons do not answer, then the sources.
// The claims, the missing parts and every source label are model or lesson text: React text nodes only, no markup parsed,
// no element or link built from them. One read of the answer feeds the text and the sources, so they appear together.
// Sources are listed in the core's order below the claims; the list has no visible numbers, so claims carry no [n] markers.

export type AnswerState = { kind: "loading" } | { kind: "failed" } | { kind: "ready"; view: AnswerView };

/** One core call for the answer; the result (or the failure) goes to `done` unless the returned stop was called first. */
export function startRead(api: AnswersApi, answerId: string, done: (id: string, state: AnswerState) => void): () => void {
  let alive = true;
  api.answer(answerId).then(
    (view) => alive && done(answerId, { kind: "ready", view }),
    () => alive && done(answerId, { kind: "failed" }),
  );
  return () => {
    alive = false;
  };
}

/**
 * Reads the answer once per (api, answerId). Null when there is nothing to read (no core call or no id): the card then
 * keeps its fixed text. A failed read is `failed`; the card stays.
 */
export function useAnswer(api: AnswersApi | undefined, answerId: string): AnswerState | null {
  const [read, setRead] = useState<{ id: string; state: AnswerState }>({ id: "", state: { kind: "loading" } });

  useEffect(() => {
    if (api === undefined || answerId === "") {
      return;
    }
    return startRead(api, answerId, (id, state) => setRead({ id, state }));
  }, [api, answerId]);

  if (api === undefined || answerId === "") {
    return null;
  }
  // A state read for another id is not shown for this one.
  return read.id === answerId ? read.state : { kind: "loading" };
}

/** One claim (no hooks: the tests render it directly). A claim that is not from the lessons says so in muted text in front of it. */
export function ClaimLineView(props: { claim: AnswerClaim; expanded: boolean; onToggle: () => void }) {
  const { claim, expanded } = props;
  const clamp = clampClaim(claim.text);
  const shown = expanded ? claim.text : clamp.text;
  return (
    <>
      <p className="ag-claim">
        {!claim.fromLessons && <span className="ag-claim-out muted">{answerTexts.notFromLessons} </span>}
        {shown}
      </p>
      {clamp.clamped && (
        <button type="button" className="ag-plan-more ag-claim-more" aria-expanded={expanded} onClick={props.onToggle}>
          {expanded ? answerTexts.collapse : answerTexts.showAll}
        </button>
      )}
    </>
  );
}

function ClaimLine(props: { claim: AnswerClaim }) {
  const [expanded, setExpanded] = useState(false);
  return <ClaimLineView claim={props.claim} expanded={expanded} onToggle={() => setExpanded((e) => !e)} />;
}

/** The body for a state (no hooks): `fallback` is the fixed line when there is no state to show. */
export function AnswerBodyView(props: { state: AnswerState | null; fallback: string; onOpen?: (o: CitationOpening) => void }) {
  const { state } = props;
  if (state === null) {
    return <p>{props.fallback}</p>;
  }
  if (state.kind === "loading") {
    return <p className="ag-src-note small muted">{answerTexts.loading}</p>;
  }
  if (state.kind === "failed") {
    return (
      <p className="ag-src-note small muted" role="status">
        {answerTexts.failed}
      </p>
    );
  }
  const parts = answerParts(state.view);
  return (
    <>
      {parts.claims.map((c, i) => (
        <ClaimLine key={i} claim={c} />
      ))}
      {parts.missing !== "" && <p className="ag-claim-miss small muted">{parts.missing}</p>}
      {parts.empty && <p className="muted">{answerTexts.empty}</p>}
      <AnswerSourcesView state={{ kind: "ready", sources: state.view.sources }} onOpen={props.onOpen} />
    </>
  );
}

/** The answer card body: reads the answer of the card once and shows the claims with the sources list. */
export function AnswerCard(props: { api?: AnswersApi; answerId: string; fallback: string; onOpen?: (o: CitationOpening) => void }) {
  const state = useAnswer(props.api, props.answerId);
  return <AnswerBodyView state={state} fallback={props.fallback} onOpen={props.onOpen} />;
}
