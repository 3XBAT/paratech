import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type AgentApi, agentApi, type JournalLine } from "../bindings/agent.js";
import { libraryApi, type LibrarySubject } from "../bindings/library.js";
import { callByName } from "../bindings/runtime.js";
import {
  type FilterState,
  filtersNarrow,
  initialFilter,
  outcomeHasRow,
  resetFilters,
  texts,
  toFilter,
  undoOutcome,
  type UndoOutcome,
} from "../journalView.js";
import { ResultLine } from "../ui/controls.js";
import { FilterBar, JournalList, LoadError } from "./JournalParts.js";

/**
 * «Журнал»: what paratech did and what was processed, newest first. The core builds the lines and decides which can be
 * undone; the page words them and sends «Отменить». There is no refresh button: the list is read on arrival and after
 * each «Отменить»; a failed read shows «Повторить» next to its message. The outcome of «Отменить» shows in the row of
 * the action.
 */
export function JournalPage() {
  const api: AgentApi = useMemo(() => agentApi(callByName), []);
  const [filter, setFilter] = useState(initialFilter);
  const [lines, setLines] = useState<JournalLine[] | null>(null);
  const [subjects, setSubjects] = useState<LibrarySubject[]>([]);
  const [error, setError] = useState("");
  const [busyAction, setBusyAction] = useState("");
  const [outcome, setOutcome] = useState<UndoOutcome | null>(null);
  const [retrying, setRetrying] = useState(false); // «Повторить» was pressed and the answer has not come
  const running = useRef(false); // a double click comes before the re-render that disables the button

  useEffect(() => {
    libraryApi(callByName).subjects().then(setSubjects, () => undefined);
  }, []);

  const load = useCallback(() => {
    let active = true;
    api.history(toFilter(filter)).then(
      (h) => {
        if (!active) {
          return;
        }
        setRetrying(false);
        if (h.problem !== "") {
          setError(texts.loadFailed);
          setLines([]);
        } else {
          setError("");
          setLines(h.lines);
        }
      },
      () => {
        if (active) {
          setRetrying(false);
          setError(texts.loadFailed);
        }
      },
    );
    return () => {
      active = false;
    };
  }, [api, filter]);

  useEffect(load, [load]);

  const names = useMemo(() => new Map(subjects.map((s) => [s.id, s.name])), [subjects]);

  async function undo(actionId: string) {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusyAction(actionId);
    setOutcome(null);
    try {
      setOutcome({ actionId, ...undoOutcome(await api.undo(actionId)) });
    } catch {
      setOutcome({ actionId, ...undoOutcome({ problem: "failed", actionId, status: "", refusal: "" }) });
    } finally {
      running.current = false;
      setBusyAction("");
      load();
    }
  }

  // «Повторить»: the lines already shown stay; a small «Загрузка…» shows until the answer comes. Without lines the
  // list area itself says «Загрузка…».
  function retry() {
    setError("");
    setRetrying(true);
    if (lines === null || lines.length === 0) {
      setLines(null);
    }
    load();
  }

  // Another filter is another list: the outcome of an action of the old one goes.
  function changeFilter(next: FilterState) {
    setOutcome(null);
    setFilter(next);
  }

  // The row of the action is gone from the list (a filter drops what was undone): the outcome is said here instead.
  const strayOutcome = outcome !== null && lines !== null && !outcomeHasRow(lines, outcome.actionId) ? outcome : null;

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">{texts.lead}</span>
          <h1>{texts.title}</h1>
        </div>
      </div>
      <FilterBar filter={filter} subjects={subjects} onChange={changeFilter} />
      {strayOutcome !== null && (
        <div className="jr-stray">
          <ResultLine kind={strayOutcome.ok ? "ok" : "fail"} role={strayOutcome.ok ? "status" : "alert"}>
            {strayOutcome.text}
          </ResultLine>
        </div>
      )}
      {(lines === null ? error === "" : retrying) && <p className="small muted">{texts.loading}</p>}
      {error !== "" && <LoadError text={error} onRetry={retry} />}
      {lines !== null && !(error !== "" && lines.length === 0) && (
        <JournalList
          lines={lines}
          subjects={names}
          busyAction={busyAction}
          outcome={outcome}
          filtered={filtersNarrow(filter)}
          onResetFilters={() => changeFilter(resetFilters(filter))}
          onUndo={(id) => void undo(id)}
        />
      )}
    </section>
  );
}
