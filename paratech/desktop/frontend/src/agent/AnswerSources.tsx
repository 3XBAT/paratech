import { useEffect, useState } from "react";
// The .js extensions let the Node test runner load the compiled component.
import { type AnswerSource, type AnswersApi, errorText } from "../bindings/answers.js";
import { type CitationOpening, citationLesson, citationOpening, citationPlace } from "../knowledgeView.js";

// The sources of an answer on its card (TASK-296). Every text (label, lesson title) is model, lesson or imported text:
// a React text node only. No element or link is built from it; a place opens by ids (CitationOpening), never by a path.

export const answerSourcesTexts = {
  list: "Источники ответа",
  loading: "Загрузка источников…",
  open: "Открыть",
} as const;

export type SourcesState = { kind: "loading" } | { kind: "failed"; text: string } | { kind: "ready"; sources: AnswerSource[] };

/** The list for a state (no hooks: the tests render it directly). A source that cannot open is plain text. */
export function AnswerSourcesView(props: { state: SourcesState; onOpen?: (o: CitationOpening) => void }) {
  const { state, onOpen } = props;
  if (state.kind === "loading") {
    return <p className="ag-src-note small muted">{answerSourcesTexts.loading}</p>;
  }
  if (state.kind === "failed") {
    return (
      <p className="ag-src-note small muted" role="status">
        {state.text}
      </p>
    );
  }
  if (state.sources.length === 0) {
    return null;
  }
  return (
    <ul className="ag-src" aria-label={answerSourcesTexts.list}>
      {state.sources.map((s, i) => {
        const place = citationPlace(s);
        const opening = citationOpening(s);
        if (opening === null || onOpen === undefined) {
          return (
            <li key={i} className="ag-src-row ag-src-gone muted">
              {place}
            </li>
          );
        }
        const lesson = citationLesson(s);
        return (
          <li key={i}>
            <button
              type="button"
              className="ag-src-row ag-src-btn"
              aria-label={`${answerSourcesTexts.open}: ${lesson === "" ? place : `${place}, ${lesson}`}`}
              onClick={() => onOpen(opening)}
            >
              <span className="ag-src-place">{place}</span>
              {lesson !== "" && <span className="ag-src-lesson small muted">{lesson}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * Reads the answer of the card once per answerId and lists its sources in the core's order. Without an id or the
 * core call it shows nothing; a failed read is one quiet line and the card stays.
 */
export function AnswerSources(props: { api?: AnswersApi; answerId: string; onOpen?: (o: CitationOpening) => void }) {
  const { api, answerId } = props;
  const [state, setState] = useState<SourcesState>({ kind: "loading" });

  useEffect(() => {
    if (api === undefined || answerId === "") {
      return;
    }
    let alive = true;
    setState({ kind: "loading" });
    api.answer(answerId).then(
      (v) => alive && setState({ kind: "ready", sources: v.sources }),
      (err) => alive && setState({ kind: "failed", text: errorText(err) }),
    );
    return () => {
      alive = false;
    };
  }, [api, answerId]);

  if (api === undefined || answerId === "") {
    return null;
  }
  return <AnswerSourcesView state={state} onOpen={props.onOpen} />;
}
