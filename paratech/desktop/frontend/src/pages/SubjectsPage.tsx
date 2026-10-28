import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type LibraryMaterial, type LibrarySubject, libraryApi, libraryErrorText } from "../bindings/library.js";
import { callByName } from "../bindings/runtime.js";
import {
  folderCounts,
  freshMarks,
  idleOverview,
  importNote,
  type OverviewState,
  scheduleLine,
  SubjectOverviewFlow,
  texts,
} from "../libraryView.js";
import type { PageId } from "../nav.js";
import { idleTeacher, TeacherFlow, type TeacherFocus, type TeacherState, teacherFocusStep } from "../teachersView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { InboxList, NameForm, OverviewSections, SubjectDetail, SubjectList, SubjectTree } from "./SubjectsParts.js";
import { type TeacherActions, TeachersBlock } from "./TeachersBlock.js";

type Form = "none" | "subject" | "folder";

/**
 * «Предметы»: the subjects as cards by semester, and the page of a subject (a tree on the left, the folders with their
 * files on the right). The core owns the rules (names, semesters, system folders); the page shows what it reports and
 * the text of its refusals. Which subject is open lives in the window route (TASK-202), so «Назад» restores it.
 * `reload` changes when files were added elsewhere (a drop on the window).
 */
export function SubjectsPage(props: {
  onOpen: (id: PageId) => void;
  reload: number;
  /** The open subject ("" shows the list) and the way to open another one. */
  subjectId: string;
  onSelect: (subjectId: string) => void;
  /** «Указать почту» of a refused send: the form of one teacher to open on arrival (taken once, then onTeacherFocusDone). */
  teacherFocus?: TeacherFocus | null;
  onTeacherFocusDone?: () => void;
}) {
  const api = useMemo(() => libraryApi(callByName), []);
  const openId = props.subjectId;
  const [subjects, setSubjects] = useState<LibrarySubject[] | null>(null);
  const [inbox, setInbox] = useState<LibraryMaterial[]>([]);
  const [materials, setMaterials] = useState<LibraryMaterial[] | null>(null);
  const [form, setForm] = useState<Form>("none");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [note, setNote] = useState("");
  const running = useRef(false); // a double click comes before the re-render that disables the button

  // The teachers of the open subject (TASK-238). Their version and data come from the list (Subjects()), which the flow
  // re-reads after a call that failed or found the data out of date.
  const [teacher, setTeacher] = useState<TeacherState>(idleTeacher);
  const shown = useRef<LibrarySubject | null>(null);
  const flow = useMemo(
    () =>
      new TeacherFlow(
        api,
        {
          subject: () => shown.current,
          replace: (s) => setSubjects((list) => (list === null ? list : list.map((x) => (x.id === s.id ? s : x)))),
          reread: () =>
            api.subjects().then(
              (s) => {
                setSubjects(s);
                return true;
              },
              () => false,
            ),
        },
        setTeacher,
      ),
    [api],
  );
  useEffect(() => {
    flow.attach();
    return () => flow.dispose();
  }, [flow]);
  const teacherActions = useMemo<TeacherActions>(
    () => ({
      open: (role) => flow.open(role),
      type: (patch) => flow.type(patch),
      copy: (source) => flow.copy(source),
      save: () => void flow.save(),
      cancel: () => flow.cancel(),
      clear: () => void flow.clear(),
    }),
    [flow],
  );

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

  // Another subject (or the list): the form, the error and the note of the previous one go away.
  useEffect(() => {
    setForm("none");
    setError("");
    setNote("");
    setMaterials(null);
    flow.reset();
  }, [openId, flow]);

  // The overview of the open subject: loaded once per opening, then the opening is recorded (see SubjectOverviewFlow).
  // It does not depend on `reload`, so a dropped file does not count as a new visit.
  const [overviewState, setOverviewState] = useState<OverviewState>(idleOverview);
  useEffect(() => {
    if (openId === "") {
      setOverviewState(idleOverview);
      return;
    }
    const flow = new SubjectOverviewFlow(api, setOverviewState);
    flow.open(openId);
    return () => flow.close();
  }, [api, openId]);

  const subject = subjects?.find((s) => s.id === openId) ?? null;
  const known = subject !== null;
  shown.current = subject;
  const overview = overviewState.overview;
  const schedule = overview ? scheduleLine(overview.schedule) : "";
  const fresh = useMemo(() => freshMarks(overview), [overview]);

  // A reload (a dropped file) only refetches the files; an open form stays.
  useEffect(() => {
    if (openId === "" || !known) {
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
  }, [api, openId, known, props.reload]);

  // «Указать почту»: once the subject is in the list, its form for that role opens (and takes the focus); taken once.
  const { teacherFocus, onTeacherFocusDone } = props;
  useEffect(() => {
    const step = teacherFocusStep(teacherFocus ?? null, openId, subjects !== null, known);
    if (step.role !== null) {
      flow.open(step.role);
    }
    if (step.done) {
      onTeacherFocusDone?.();
    }
  }, [teacherFocus, openId, subjects, known, flow, onTeacherFocusDone]);

  // The route points at a subject that does not exist (any more): back to the list.
  const { onSelect } = props;
  useEffect(() => {
    if (openId !== "" && subjects !== null && !known) {
      onSelect("");
    }
  }, [openId, subjects, known, onSelect]);

  const counts = useMemo(() => (subject && materials ? folderCounts(subject.folders, materials) : null), [subject, materials]);

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
      props.onSelect(s.id);
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

  const subjectForm = form === "subject" && (
    <NameForm label={texts.subjectName} busy={busy} error={error} onSubmit={(n) => void createSubject(n)} onCancel={() => setForm("none")} />
  );

  if (subject && subjects) {
    return (
      <section className="page">
        <div className="subj">
          <SubjectTree
            subjects={subjects}
            currentId={subject.id}
            counts={counts}
            freshFolders={fresh.folders}
            busy={busy}
            onOpen={props.onSelect}
            onAddSubject={() => setForm("subject")}
            onAddFolder={() => {
              setError("");
              setForm("folder");
            }}
          />
          <div className="subj-main">
            <div className="head">
              <div>
                <span className="label">{subject.semesterLabel || subject.semester}</span>
                <h1>{subject.name}</h1>
                {subject.aliases.length > 0 && (
                  <p className="small muted sub-line">
                    {texts.aliases}: {subject.aliases.join(", ")}
                  </p>
                )}
                {schedule !== "" && <p className="small muted sub-line">{schedule}</p>}
              </div>
              <div className="acts">
                <Button icon="back" onClick={() => props.onSelect("")}>
                  {texts.back}
                </Button>
              </div>
            </div>
            {subjectForm}
            {overviewState.status === "failed" && (
              <ResultLine kind="fail" role="alert">
                {overviewState.error}
              </ResultLine>
            )}
            <TeachersBlock teachers={subject.teachers} state={teacher} on={teacherActions} />
            {overview && <OverviewSections overview={overview} />}
            <SubjectDetail
              subject={subject}
              materials={materials ?? []}
              fresh={fresh}
              folderForm={form === "folder"}
              busy={busy}
              error={error}
              onCreateFolder={(n) => void createFolder(n)}
              onCancelFolder={() => setForm("none")}
            />
            {form !== "folder" && form !== "subject" && error !== "" && (
              <ResultLine kind="fail" role="alert">
                {error}
              </ResultLine>
            )}
          </div>
        </div>
      </section>
    );
  }

  if (openId !== "" && (subjects === null || !known)) {
    // Loading, or the unknown id being dropped by the effect above: no list flashes under an open route.
    return (
      <section className="page">
        {loadError !== "" ? (
          <ResultLine kind="fail" role="alert">
            {loadError}
          </ResultLine>
        ) : (
          <p className="muted">{texts.loading}</p>
        )}
      </section>
    );
  }

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">Библиотека</span>
          <h1>{texts.title}</h1>
        </div>
        <div className="acts">
          <Button icon="book" onClick={() => props.onOpen("knowledge")}>
            {texts.knowledge}
          </Button>
          <Button icon="doc" onClick={() => props.onOpen("tickets")}>
            Билеты
          </Button>
          <Button icon="up" disabled={busy} onClick={() => void addFile()}>
            {texts.addFile}
          </Button>
          <Button variant="primary" icon="plus" disabled={busy} onClick={() => setForm("subject")}>
            {texts.addSubject}
          </Button>
        </div>
      </div>
      {subjects === null && !loadError && <p className="muted">{texts.loading}</p>}
      {loadError !== "" && (
        <ResultLine kind="fail" role="alert">
          {loadError}
        </ResultLine>
      )}
      {subjectForm}
      {subjects !== null && (
        <>
          <SubjectList subjects={subjects} onOpen={props.onSelect} onAdd={() => setForm("subject")} />
          <InboxList materials={inbox} />
          {note !== "" && (
            <ResultLine kind="ok" role="status">
              {note}
            </ResultLine>
          )}
          {form !== "subject" && error !== "" && (
            <ResultLine kind="fail" role="alert">
              {error}
            </ResultLine>
          )}
        </>
      )}
    </section>
  );
}
