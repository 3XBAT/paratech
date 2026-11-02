import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type LibraryMaterial, type LibrarySubject, libraryApi, libraryErrorText, type ScheduleSubjectsReply, scheduleSubjects } from "../bindings/library.js";
import { libraryInboxApi } from "../bindings/libraryInbox.js";
import { callByName } from "../bindings/runtime.js";
import { fileTexts, type ImportElsewhere, type ImportOutcome, inboxScrollDue, inboxScrollSettled, scrollToInbox } from "../libraryInboxView.js";
import { addFileInto, CardDrop } from "../materialFlow.js";
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
import { addSubjectVariant, type Batch, type OfferState, offerChecking, offerStatus, showCheckFailed, type SubjectRef } from "../scheduleSubjectsView.js";
import { idleTeacher, TeacherFlow, type TeacherFocus, type TeacherState, teacherFocusStep, teacherStateFor } from "../teachersView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { AddFileLines, ErrorLine, UndoLines } from "./FileRows.js";
import {
  KeptUndoLine,
  offerButtonId,
  ScheduleCheckFailedLine,
  ScheduleOfferLine,
  ScheduleSubjectsReview,
  scheduleReviewApi,
  useKeptUndo,
} from "./ScheduleSubjectsReview.js";
import { NameForm, OverviewSections, SubjectDetail, SubjectsListBody, SubjectTree } from "./SubjectsParts.js";
import { type TeacherActions, TeachersBlock } from "./TeachersBlock.js";
import { useMaterialActions } from "./useMaterialActions.js";

type Form = "none" | "subject" | "folder";

/** Where the focus goes when the review closes and its opener is gone. */
const addSubjectId = "subjects-add";
const titleId = "subjects-title";

interface AddedState {
  note: string;
  elsewhere: ImportElsewhere | null;
  toInbox: boolean;
  /** The file that did not land and where it was meant to go (for «Разложить» in the line). */
  unplaced: ImportOutcome["unplaced"] | null;
  /** Where «Добавить файл» aimed (the folder resolved), for «Переместить сюда». */
  target: { subjectId: string; folderId: string } | null;
  /** The folder whose «+» started it, where the result is drawn; "" for the header button (the result is under the head). */
  origin: string;
}

const noAdded: AddedState = { note: "", elsewhere: null, toInbox: false, unplaced: null, target: null, origin: "" };

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
  /** «Завести предметы из расписания» of the schedule page: the review opens on arrival (taken once, then onReviewTaken). */
  openReview?: boolean;
  onReviewTaken?: () => void;
}) {
  const api = useMemo(() => libraryApi(callByName), []);
  const inboxApi = useMemo(() => libraryInboxApi(callByName), []);
  const openId = props.subjectId;
  const [subjects, setSubjects] = useState<LibrarySubject[] | null>(null);
  const [inbox, setInbox] = useState<LibraryMaterial[]>([]);
  const [inboxLoaded, setInboxLoaded] = useState(false); // the first answer for «Входящие» (a failure counts: nothing to wait for)
  const [materials, setMaterials] = useState<LibraryMaterial[] | null>(null);
  const [form, setForm] = useState<Form>("none");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [note, setNote] = useState("");
  // The file being dragged from «Входящие» (the cards of the list show the cue) and the refusal of a drop on a card.
  const [dragging, setDragging] = useState("");
  const [dropError, setDropError] = useState("");
  // Changes made here (a file placed, deleted, added into a subject): the files of the open subject are read again.
  const [changes, setChanges] = useState(0);
  // The result of «Добавить файл» on the page of a subject, and the refusal (with how to repeat it).
  const [added, setAdded] = useState<AddedState>(noAdded);
  const [addError, setAddError] = useState<{ text: string; again: () => void; origin: string } | null>(null);
  // «Перейти во «Входящие»» from a subject page: the list page opens first, then the scroll (see inboxScrollDue).
  const [inboxScroll, setInboxScroll] = useState(false);
  const running = useRef(false); // a double click comes before the re-render that disables the button

  // Subjects from the schedule (TASK-312): the proposal asked for after the subjects (a failure shows «Не удалось
  // проверить расписание»), and the review opened over the list with the proposal it started from.
  const [offer, setOffer] = useState<OfferState>(offerChecking);
  const [review, setReview] = useState<{ reply: ScheduleSubjectsReply; batches: readonly Batch[] } | null>(null);
  const reviewApi = useMemo(() => scheduleReviewApi(callByName), []);
  const subjectRefs = useMemo<SubjectRef[]>(() => (subjects ?? []).map((s) => ({ id: s.id, name: s.name, aliases: s.aliases })), [subjects]);
  const reviewWasOpen = useRef(false);
  useEffect(() => {
    // The panel closed: the focus goes back to the button that opened it, else to «Предмет», else to the title.
    if (review === null && reviewWasOpen.current) {
      (document.getElementById(offerButtonId) ?? document.getElementById(addSubjectId) ?? document.getElementById(titleId))?.focus();
    }
    reviewWasOpen.current = review !== null;
  }, [review]);

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
      restore: () => void flow.restore(),
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
    api.materials("").then(
      (m) => {
        setInbox(m);
        setInboxLoaded(true);
      },
      () => setInboxLoaded(true),
    );
    scheduleSubjects(callByName).then(
      (reply) => setOffer({ loaded: true, reply }),
      () => setOffer({ loaded: true, reply: null }),
    );
  }, [api]);

  useEffect(load, [load, props.reload]);

  const refresh = useCallback(() => {
    setDropError("");
    load();
    setChanges((n) => n + 1);
  }, [load]);

  // «Отменить всё» stays on the page after the panel is closed; opening the panel again takes the batches back, so the
  // undo still goes newest first. A change of subjects reloads the page the same way as a file action does.
  const kept = useKeptUndo(subjectRefs, reviewApi, refresh);
  // The page's own «Отменить всё» runs: the review may not open over it (it would take the batches under the undo).
  const undoBusy = kept.state.phase !== "idle";
  const openReview = () => {
    if (offer.reply !== null && !undoBusy) {
      setReview({ reply: offer.reply, batches: kept.state.batches });
      kept.adopt([]);
    }
  };
  const retryOffer = () => {
    setOffer(offerChecking);
    load();
  };
  // «Завести предметы из расписания» of the schedule page: the review opens as soon as the proposal has loaded (an empty
  // one shows its own notice in it); taken once. When the check failed the request waits (`pendingReview`) behind the
  // line «Не удалось проверить расписание» / «Повторить», and a retry that succeeds opens the review.
  const { openReview: reviewWanted, onReviewTaken } = props;
  const [pendingReview, setPendingReview] = useState(false);
  useEffect(() => {
    if (openId !== "") {
      setPendingReview(false);
      return;
    }
    if (!(reviewWanted || pendingReview) || !offer.loaded || subjects === null || undoBusy) {
      return;
    }
    if (offerStatus(offer) === "failed") {
      if (reviewWanted) {
        setPendingReview(true);
        onReviewTaken?.();
      }
      return;
    }
    setPendingReview(false);
    openReview();
    if (reviewWanted) {
      onReviewTaken?.();
    }
  }, [reviewWanted, pendingReview, offer, subjects, openId, undoBusy]);

  // Another subject (or the list): the form, the error and the note of the previous one go away.
  useEffect(() => {
    setForm("none");
    setError("");
    setNote("");
    setAdded(noAdded);
    setAddError(null);
    setDropError("");
    setDragging("");
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
  const mat = useMaterialActions({ api, inboxApi, subjects, setSubjects, inbox, hintsOn: openId === "", refresh });
  useEffect(() => {
    if (inboxScrollDue(inboxScroll, openId, subjects !== null)) {
      const scrolled = scrollToInbox(typeof document === "undefined" ? undefined : document);
      if (inboxScrollSettled(scrolled, inboxLoaded)) {
        setInboxScroll(false);
      }
    }
  }, [inboxScroll, openId, subjects, inbox, inboxLoaded]);
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
  }, [api, openId, known, props.reload, changes]);

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

  // «Добавить файл» on the page of a subject: the file lands in the subject (folder "" is «Материалы»). A refusal is
  // shown with «Повторить» or «Обновить список» (see ErrorLine); a cancelled dialog says nothing.
  const addFileTo = async (folderId: string) => {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
    setAdded(noAdded);
    setAddError(null);
    try {
      const r = await addFileInto(inboxApi, openId, folderId, subjects ?? []);
      if (r.kind === "done") {
        setAdded({ ...r.outcome, unplaced: r.outcome.unplaced ?? null, target: r.target, origin: folderId });
        refresh();
      } else if (r.kind === "refused") {
        setAddError({ text: r.text, again: () => void addFileTo(folderId), origin: folderId });
      }
    } finally {
      running.current = false;
      setBusy(false);
    }
  };

  // A row of «Входящие» dropped on a card: the same as «Разложить» into «Материалы», with the same undo line.
  const live = useRef({ inbox, place: mat.actions.place });
  live.current = { inbox, place: mat.actions.place };
  const cardDrop = useMemo(
    () =>
      new CardDrop({
        find: (id) => live.current.inbox.find((x) => x.id === id),
        place: (m, subjectId, folderId) => live.current.place(m, subjectId, folderId),
        setError: setDropError,
      }),
    [],
  );

  // «Переместить сюда»: the file the import found filed elsewhere goes into the target; «Вернуть» puts it back.
  const moveHere = async () => {
    const e = added.elsewhere;
    const t = added.target;
    if (e === null || t === null || running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setAddError(null);
    try {
      const text = await mat.placeInto(e.material, t.subjectId, t.folderId);
      if (text === "") {
        setAdded(noAdded);
      } else {
        setAddError({ text, again: () => void moveHere(), origin: added.origin });
      }
    } finally {
      running.current = false;
      setBusy(false);
    }
  };

  // «Разложить» in the line of a file that did not land: the same placing as «Разложить» in «Входящие» (with «Вернуть»).
  const placeUnplaced = async (subjectId: string, folderId: string): Promise<string> => {
    const m = added.unplaced?.material;
    if (!m || running.current) {
      return "";
    }
    running.current = true;
    setBusy(true);
    try {
      const text = await mat.placeInto(m, subjectId, folderId);
      if (text === "") {
        setAdded(noAdded);
      }
      return text;
    } finally {
      running.current = false;
      setBusy(false);
    }
  };

  // The result of «Добавить файл»: under the head for the header button, inside the section of the folder for its «+».
  const addLines = (origin: string) => (
    <AddFileLines
      note={added.origin === origin ? added.note : ""}
      elsewhere={added.origin === origin ? added.elsewhere : null}
      toInbox={added.origin === origin && added.toInbox}
      unplaced={added.origin === origin ? added.unplaced : null}
      placing={{ subjects: subjects ?? [], place: placeUnplaced, createSubject: mat.actions.createSubject }}
      busy={busy}
      error={addError !== null && addError.origin === origin ? addError.text : ""}
      onMoveHere={() => void moveHere()}
      onOpenSubject={props.onSelect}
      onOpenInbox={() => {
        setInboxScroll(true);
        props.onSelect("");
      }}
      onRetry={() => addError?.again()}
      onRefresh={refresh}
    />
  );

  const undoLines = <UndoLines items={mat.undo} busy={mat.undoBusy} onUndo={(it) => void mat.undoOne(it)} onRefresh={mat.refreshUndo} />;
  const inboxIds = useMemo(() => new Set(inbox.filter((m) => !m.removed).map((m) => m.id)), [inbox]);
  // The dragged row left «Входящие» (the drop took it): no dragend may come from a row that is gone.
  useEffect(() => {
    if (dragging !== "" && !inboxIds.has(dragging)) {
      setDragging("");
    }
  }, [dragging, inboxIds]);

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
                <Button icon="up" disabled={busy} title={fileTexts.addToMaterialsTip} onClick={() => void addFileTo("")}>
                  {fileTexts.addFileTo}
                </Button>
                <Button icon="back" onClick={() => props.onSelect("")}>
                  {texts.back}
                </Button>
              </div>
            </div>
            {addLines("")}
            {subjectForm}
            {overviewState.status === "failed" && (
              <ResultLine kind="fail" role="alert">
                {overviewState.error}
              </ResultLine>
            )}
            <TeachersBlock key={subject.id} teachers={subject.teachers} state={teacherStateFor(teacher, subject.id)} on={teacherActions} />
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
              actions={mat.actions}
              onAddFile={(folderId) => void addFileTo(folderId)}
              folderExtra={(folderId) => addLines(folderId)}
            />
            {form !== "folder" && form !== "subject" && error !== "" && (
              <ResultLine kind="fail" role="alert">
                {error}
              </ResultLine>
            )}
            {undoLines}
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

  const scheduleGroups = offer.reply !== null && offerStatus(offer) === "groups" ? offer.reply.groups.length : 0;

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">Библиотека</span>
          <h1 id={titleId} tabIndex={-1}>
            {texts.title}
          </h1>
        </div>
        <div className="acts">
          <Button icon="book" onClick={() => props.onOpen("knowledge")}>
            {texts.knowledge}
          </Button>
          <Button icon="doc" onClick={() => props.onOpen("tickets")}>
            Билеты
          </Button>
          <Button icon="up" disabled={busy} title={fileTexts.addToInboxTip} onClick={() => void addFile()}>
            {texts.addFile}
          </Button>
          {/* One accent: with no subjects the empty state below has the primary action, an open review its own button. */}
          <Button id={addSubjectId} variant={addSubjectVariant(subjects?.length ?? null, review !== null)} icon="plus" disabled={busy} onClick={() => setForm("subject")}>
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
      {review !== null && (
        <ScheduleSubjectsReview
          reply={review.reply}
          initialBatches={review.batches}
          subjects={subjectRefs}
          api={reviewApi}
          onChanged={refresh}
          onClose={(batches) => {
            kept.adopt(batches);
            setReview(null);
          }}
          autoFocus
        />
      )}
      {subjects !== null && (
        <>
          {review === null && <KeptUndoLine state={kept.state} onUndo={kept.undo} onDismiss={kept.dismiss} onForget={kept.forget} />}
          {review === null && subjects.length > 0 && scheduleGroups > 0 && (
            <ScheduleOfferLine groups={scheduleGroups} disabled={undoBusy} onClick={openReview} />
          )}
          {review === null && showCheckFailed(offer, subjects.length) && <ScheduleCheckFailedLine onRetry={retryOffer} />}
          <SubjectsListBody
            subjects={subjects}
            inbox={inbox}
            inboxActions={{ ...mat.actions, dragStart: setDragging, dragEnd: () => setDragging("") }}
            onOpen={props.onSelect}
            onAdd={() => setForm("subject")}
            drop={{
              dragging: dragging !== "",
              inboxIds,
              onDrop: (s, id) => void cardDrop.drop(s.id, id),
              onEnd: () => setDragging(""),
            }}
            offer={{ state: offer, onReview: openReview, onImport: () => props.onOpen("schedule"), onRetry: retryOffer, disabled: undoBusy }}
            hideList={review !== null && subjects.length === 0}
          />
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
          {dropError !== "" && (
            <ErrorLine
              text={dropError}
              onRefresh={refresh}
              onRetry={() => void cardDrop.again()}
            />
          )}
          {undoLines}
        </>
      )}
    </section>
  );
}
