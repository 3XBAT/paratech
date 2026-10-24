import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type LibraryMaterial, type LibrarySubject, libraryApi } from "../bindings/library.js";
import { callByName } from "../bindings/runtime.js";
import { type TicketSet, ticketsApi, ticketsErrorText } from "../bindings/tickets.js";
import type { PageId } from "../nav.js";
import { needsPolling, texts, ticketsPollMs } from "../ticketsView.js";
import { Button } from "../ui/Button.js";
import { EmptyState, Field, ResultLine, Select } from "../ui/controls.js";
import { SetCard } from "./TicketsParts.js";

/**
 * «Билеты»: the sets of exam tickets of a subject. A list is pasted or read from a file of the subject; the set is
 * only proposed until the student confirms it (the paid run); the core owns every rule and number, the page shows
 * them. Model-made and student text is rendered as text only.
 */
export function TicketsPage(props: { onOpen: (id: PageId) => void }) {
  const lib = useMemo(() => libraryApi(callByName), []);
  const api = useMemo(() => ticketsApi(callByName), []);
  const [subjects, setSubjects] = useState<LibrarySubject[] | null>(null);
  const [subjectId, setSubjectId] = useState("");
  const [sets, setSets] = useState<TicketSet[] | null>(null);
  const [materials, setMaterials] = useState<LibraryMaterial[]>([]);
  const [materialId, setMaterialId] = useState("");
  const [text, setText] = useState("");
  const [loadError, setLoadError] = useState("");
  const [formError, setFormError] = useState("");
  const [setErrors, setSetErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const running = useRef(false); // a double click comes before the re-render that disables the button

  useEffect(() => {
    let active = true;
    lib.subjects().then(
      (list) => {
        if (!active) {
          return;
        }
        setSubjects(list);
        setSubjectId((id) => (id !== "" ? id : (list[0]?.id ?? "")));
      },
      () => active && setLoadError(texts.loadFailed),
    );
    return () => {
      active = false;
    };
  }, [lib]);

  const reload = useCallback(
    (id: string) =>
      api.list(id).then(
        (list) => {
          setSets(list);
          setLoadError("");
        },
        () => setLoadError(texts.loadFailed),
      ),
    [api],
  );

  useEffect(() => {
    if (subjectId === "") {
      return;
    }
    let active = true;
    setSets(null);
    setMaterialId("");
    api.list(subjectId).then(
      (list) => active && (setSets(list), setLoadError("")),
      () => active && setLoadError(texts.loadFailed),
    );
    lib.materials(subjectId).then(
      (m) => active && setMaterials(m.filter((x) => !x.removed)),
      () => active && setMaterials([]),
    );
    return () => {
      active = false;
    };
  }, [api, lib, subjectId]);

  const anyRunning = (sets ?? []).some(needsPolling);
  useEffect(() => {
    if (!anyRunning || subjectId === "") {
      return;
    }
    const timer = setInterval(() => void reload(subjectId), ticketsPollMs);
    return () => clearInterval(timer);
  }, [anyRunning, subjectId, reload]);

  const replace = (next: TicketSet) =>
    setSets((list) => {
      const rest = list ?? [];
      return rest.some((x) => x.id === next.id) ? rest.map((x) => (x.id === next.id ? next : x)) : [next, ...rest];
    });

  async function run(key: string, work: () => Promise<void>, onError: (text: string) => void) {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(key);
    onError("");
    try {
      await work();
    } catch (err) {
      onError(ticketsErrorText(err));
    } finally {
      running.current = false;
      setBusy("");
    }
  }

  const propose = (work: () => Promise<TicketSet>, after: () => void) =>
    run("propose", async () => {
      replace(await work());
      after();
    }, setFormError);

  const onSetAction = (id: string, work: () => Promise<TicketSet>) =>
    run(id, async () => {
      replace(await work());
      setSetErrors((e) => ({ ...e, [id]: "" }));
    }, (t) => setSetErrors((e) => ({ ...e, [id]: t })));

  const subject = subjects?.find((s) => s.id === subjectId) ?? null;
  const blocked = busy !== "";

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">{texts.label}</span>
          <h1>{texts.title}</h1>
        </div>
        <div className="acts">
          <Button icon="back" onClick={() => props.onOpen("subjects")}>
            {texts.back}
          </Button>
        </div>
      </div>
      {subjects === null && loadError === "" && <p className="muted">{texts.loading}</p>}
      {loadError !== "" && (
        <ResultLine kind="fail" role="alert">
          {loadError}
        </ResultLine>
      )}
      {subjects !== null && subjects.length === 0 && (
        <EmptyState label={texts.label} title={texts.title} actions={<Button onClick={() => props.onOpen("subjects")}>{texts.back}</Button>}>
          {texts.noSubjects}
        </EmptyState>
      )}
      {subject !== null && subjects !== null && (
        <>
          <div className="tk-pick">
            <Field label={texts.subject}>
              {(id) => (
                <Select id={id} value={subjectId} disabled={blocked} onChange={(e) => setSubjectId(e.target.value)}>
                  {subjects.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          </div>

          <form
            className="tk-form"
            onSubmit={(e) => {
              e.preventDefault();
              if (text.trim() !== "" && !blocked) {
                void propose(() => api.propose(subjectId, text), () => setText(""));
              }
            }}
          >
            <h2 className="group-cap">{texts.pasteTitle}</h2>
            <Field label={texts.pasteLabel} hint={texts.pasteHint}>
              {(id) => <textarea id={id} className="input tk-text" rows={6} value={text} disabled={blocked} onChange={(e) => setText(e.target.value)} />}
            </Field>
            <div className="acts start">
              <Button type="submit" variant="primary" disabled={blocked || text.trim() === ""} busy={busy === "propose"} busyLabel="Читается…">
                {texts.pasteAction}
              </Button>
            </div>
          </form>

          <div className="tk-form">
            <h2 className="group-cap">{texts.fileTitle}</h2>
            {materials.length === 0 ? (
              <p className="small muted">{texts.noFiles}</p>
            ) : (
              <>
                <Field label={texts.fileLabel}>
                  {(id) => (
                    <Select id={id} value={materialId} disabled={blocked} onChange={(e) => setMaterialId(e.target.value)}>
                      <option value="">Выбрать файл…</option>
                      {materials.map((m) => (
                        <option key={m.id} value={m.id}>
                          {m.name}
                        </option>
                      ))}
                    </Select>
                  )}
                </Field>
                <div className="acts start">
                  <Button
                    disabled={blocked || materialId === ""}
                    busy={busy === "propose"}
                    busyLabel="Читается…"
                    onClick={() => void propose(() => api.proposeFromMaterial(materialId), () => setMaterialId(""))}
                  >
                    {texts.fileAction}
                  </Button>
                </div>
              </>
            )}
          </div>
          {formError !== "" && (
            <ResultLine kind="fail" role="alert">
              {formError}
            </ResultLine>
          )}

          {sets === null && loadError === "" && <p className="muted">{texts.loading}</p>}
          {sets !== null && sets.length === 0 && <p className="small muted">{texts.noSets}</p>}
          {sets !== null && sets.length > 0 && (
            <div className="tk-sets">
              {sets.map((s) => (
                <SetCard
                  key={s.id}
                  set={s}
                  subjectName={subject.name}
                  busy={busy === s.id}
                  blocked={blocked}
                  error={setErrors[s.id] ?? ""}
                  onConfirm={(accept) => void onSetAction(s.id, () => api.confirm(s.id, accept))}
                  onCancel={() => void onSetAction(s.id, () => api.cancel(s.id))}
                  onUndo={() => void onSetAction(s.id, () => api.undo(s.id))}
                />
              ))}
            </div>
          )}
        </>
      )}
    </section>
  );
}
