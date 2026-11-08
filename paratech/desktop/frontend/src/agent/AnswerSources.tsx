// The .js extensions let the Node test runner load the compiled component.
import type { AnswerSource } from "../bindings/answers.js";
import { type CitationOpening, citationLesson, citationOpening, citationPlace } from "../knowledgeView.js";

// The sources of an answer on its card (TASK-296). Every text (label, lesson title) is model, lesson or imported text:
// a React text node only. No element or link is built from it; a place opens by ids (CitationOpening), never by a path.
// The answer is read once by AnswerCard.tsx (useAnswer), which passes the sources here.

export const answerSourcesTexts = {
  list: "Источники ответа",
  loading: "Загрузка источников…",
  open: "Открыть",
  failed: "Источники не загрузились.",
} as const;

export type SourcesState = { kind: "loading" } | { kind: "failed" } | { kind: "ready"; sources: AnswerSource[] };

/** The list for a state (no hooks: the tests render it directly). A source that cannot open is plain text. */
export function AnswerSourcesView(props: { state: SourcesState; onOpen?: (o: CitationOpening) => void }) {
  const { state, onOpen } = props;
  if (state.kind === "loading") {
    return <p className="ag-src-note small muted">{answerSourcesTexts.loading}</p>;
  }
  if (state.kind === "failed") {
    return (
      <p className="ag-src-note small muted" role="status">
        {answerSourcesTexts.failed}
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
        if (opening === null) {
          return (
            <li key={i} className="ag-src-row ag-src-gone">
              {place}
            </li>
          );
        }
        if (onOpen === undefined) {
          return (
            <li key={i} className="ag-src-row ag-src-plain">
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
