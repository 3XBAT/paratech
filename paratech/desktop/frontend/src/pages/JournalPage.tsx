import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type AgentApi, agentApi, type JournalLine } from "../bindings/agent.js";
import { libraryApi, type LibrarySubject } from "../bindings/library.js";
import { callByName } from "../bindings/runtime.js";
import { initialFilter, texts, toFilter, undoOutcome } from "../journalView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { FilterBar, JournalList } from "./JournalParts.js";

/**
 * «Журнал»: what paratech did and what was processed, newest first. The core builds the lines and decides which can be
 * undone; the page words them and sends «Отменить».
 */
export function JournalPage() {
  const api: AgentApi = useMemo(() => agentApi(callByName), []);
  const [filter, setFilter] = useState(initialFilter);
  const [lines, setLines] = useState<JournalLine[] | null>(null);
  const [subjects, setSubjects] = useState<LibrarySubject[]>([]);
  const [error, setError] = useState("");
  const [busyAction, setBusyAction] = useState("");
  const [outcome, setOutcome] = useState<{ ok: boolean; text: string } | null>(null);
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
        if (h.problem !== "") {
          setError(texts.loadFailed);
          setLines([]);
        } else {
          setError("");
          setLines(h.lines);
        }
      },
      () => active && setError(texts.loadFailed),
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
      setOutcome(undoOutcome(await api.undo(actionId)));
    } catch {
      setOutcome(undoOutcome({ problem: "failed", actionId, status: "", refusal: "" }));
    } finally {
      running.current = false;
      setBusyAction("");
      load();
    }
  }

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">{texts.lead}</span>
          <h1>{texts.title}</h1>
        </div>
        <div className="acts">
          <Button icon="retry" onClick={load}>
            {texts.refresh}
          </Button>
        </div>
      </div>
      <FilterBar filter={filter} subjects={subjects} onChange={setFilter} />
      {lines === null && !error && <p className="muted">{texts.loading}</p>}
      {error !== "" && (
        <ResultLine kind="fail" role="alert">
          {error}
        </ResultLine>
      )}
      {lines !== null && <JournalList lines={lines} subjects={names} busyAction={busyAction} onUndo={(id) => void undo(id)} />}
      {outcome !== null && (
        <ResultLine kind={outcome.ok ? "ok" : "fail"} role="status">
          {outcome.text}
        </ResultLine>
      )}
    </section>
  );
}
