import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type LibraryMaterial, type LibrarySubject, libraryApi, libraryErrorText } from "../bindings/library.js";
import { callByName } from "../bindings/runtime.js";
import { importNote, texts } from "../libraryView.js";
import type { PageId } from "../nav.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { InboxList, NameForm, SubjectDetail, SubjectList } from "./SubjectsParts.js";

type Form = "none" | "subject" | "folder";

/**
 * «Предметы»: semesters, subjects, and the page of a subject with its folders. The core owns the rules (names,
 * semesters, system folders); the page shows what it reports and the text of its refusals.
 * `reload` changes when files were added elsewhere (a drop on the window).
 */
export function SubjectsPage(props: { onOpen: (id: PageId) => void; reload: number; visit?: number }) {
  const api = useMemo(() => libraryApi(callByName), []);
  const [subjects, setSubjects] = useState<LibrarySubject[] | null>(null);
  const [inbox, setInbox] = useState<LibraryMaterial[]>([]);
  const [openId, setOpenId] = useState("");
  const [materials, setMaterials] = useState<LibraryMaterial[]>([]);
  const [form, setForm] = useState<Form>("none");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [note, setNote] = useState("");
  const firstVisit = useRef(props.visit);
  const running = useRef(false); // a double click comes before the re-render that disables the button

  const load = useCallback(() => {
    api.subjects().then(
      (s) => {
        setSubjects(s);
        setLoadError("");
      },
      () => setLoadError(texts.loadFailed),
    );
    api.materials("").then(setInbox, () => undefined);
  }, [api]);

  useEffect(load, [load, props.reload]);

  useEffect(() => {
    if (openId === "") {
      return;
    }
    let active = true;
    api.materials(openId).then(
      (m) => active && setMaterials(m),
      () => active && setError(texts.filesFailed),
    );
    return () => {
      active = false;
    };
  }, [api, openId, props.reload]);

  // A click on «Предметы» while a subject is open returns to the list.
  useEffect(() => {
    if (props.visit !== firstVisit.current) {
      firstVisit.current = props.visit;
      setOpenId("");
      setMaterials([]);
      setForm("none");
      setError("");
      setNote("");
    }
  }, [props.visit]);

  const subject = subjects?.find((s) => s.id === openId) ?? null;

  async function run(work: () => Promise<void>) {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
    setNote("");
    try {
      await work();
    } catch (err) {
      setError(libraryErrorText(err));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  const createSubject = (name: string) =>
    run(async () => {
      const s = await api.createSubject(name, "");
      setSubjects((list) => [...(list ?? []), s]);
      setForm("none");
      setOpenId(s.id);
      setMaterials([]);
    });

  const createFolder = (name: string) =>
    run(async () => {
      const f = await api.createFolder(openId, name);
      setSubjects((list) => (list ?? []).map((s) => (s.id === openId ? { ...s, folders: [...s.folders, f] } : s)));
      setForm("none");
    });

  const addFile = () =>
    run(async () => {
      const r = await api.importFile();
      setNote(importNote(r));
      load();
    });

  const open = (id: string) => {
    setOpenId(id);
    setMaterials([]);
    setForm("none");
    setError("");
  };

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">{subject ? texts.title : "Библиотека"}</span>
          <h1>{subject ? subject.name : texts.title}</h1>
        </div>
        <div className="acts">
          {subject ? (
            <Button icon="back" onClick={() => open("")}>
              {texts.back}
            </Button>
          ) : (
            <>
              <Button icon="book" onClick={() => props.onOpen("knowledge")}>
                {texts.knowledge}
              </Button>
              <Button icon="up" disabled={busy} onClick={() => void addFile()}>
                {texts.addFile}
              </Button>
              <Button variant="primary" icon="plus" disabled={busy} onClick={() => setForm("subject")}>
                {texts.addSubject}
              </Button>
            </>
          )}
        </div>
      </div>
      {subjects === null && !loadError && <p className="muted">{texts.loading}</p>}
      {loadError !== "" && (
        <ResultLine kind="fail" role="alert">
          {loadError}
        </ResultLine>
      )}
      {form === "subject" && !subject && (
        <NameForm label={texts.subjectName} busy={busy} error={error} onSubmit={(n) => void createSubject(n)} onCancel={() => setForm("none")} />
      )}
      {subjects !== null && !subject && (
        <>
          <SubjectList subjects={subjects} onOpen={open} onAdd={() => setForm("subject")} />
          <InboxList materials={inbox} />
          {note !== "" && <ResultLine kind="ok" role="status">{note}</ResultLine>}
          {form !== "subject" && error !== "" && (
            <ResultLine kind="fail" role="alert">
              {error}
            </ResultLine>
          )}
        </>
      )}
      {subject && (
        <SubjectDetail
          subject={subject}
          materials={materials}
          folderForm={form === "folder"}
          busy={busy}
          error={error}
          onAddFolder={() => {
            setError("");
            setForm("folder");
          }}
          onCreateFolder={(n) => void createFolder(n)}
          onCancelFolder={() => setForm("none")}
        />
      )}
    </section>
  );
}
