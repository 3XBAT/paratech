import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type LibraryMaterial,
  type LibrarySubject,
  libraryApi,
  libraryErrorText,
  type RemovedName,
  removedNameOf,
  type ScheduleSubjectsReply,
  scheduleSubjects,
} from "../bindings/library.js";
import { libraryInboxApi } from "../bindings/libraryInbox.js";
import { notesApi as makeNotesApi } from "../bindings/notes.js";
import { callByName } from "../bindings/runtime.js";
import { fileTexts, type ImportElsewhere, type ImportOutcome } from "../libraryInboxView.js";
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
import { type Batch, type OfferState, offerChecking, offerStatus, showCheckFailed, type SubjectRef } from "../scheduleSubjectsView.js";
import {
  browserStore,
  homeSubjectId,
  inboxEntry,
  inboxFiles,
  inboxRoute,
  type Landing,
  landingFor,
  inboxFailedText,
  opensNewPlace,
  rememberSubject,
  resolveView,
  shownSubjectId,
  showsTiles,
  viewKey,
} from "../subjectsHome.js";
import { removedLineText, restoreNeedsReload, runRestore } from "../subjectRemovalView.js";
import { idleTeacher, TeacherFlow, type TeacherFocus, type TeacherState, teacherFocusStep, teacherStateFor } from "../teachersView.js";
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
import { RemovedNameLine, RemoveSubjectButton, RemoveSubjectDialog, RestoreRow } from "./SubjectRemoval.js";
import { EmptySubjects, InboxList, LibraryActions, NameForm, OverviewSections, SubjectDetail, SubjectTree } from "./SubjectsParts.js";
import { type TeacherActions, TeachersBlock } from "./TeachersBlock.js";
import { useMaterialActions } from "./useMaterialActions.js";
import { useSubjectRemoval } from "./useSubjectRemoval.js";

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
 * «Предметы» (TASK-341): the tree on the left («Входящие» first, then the subjects by semester) and the content on the
 * right, from the first moment: the last opened subject, else the first one. The tiles are only for a library with no
 * subjects. The core owns the rules (names, semesters, system folders); the page shows what it reports and the text of
 * its refusals. What is open lives in the window route (TASK-202), so «Назад» restores it: "" is «no choice yet»
 * (the remembered subject, no entry of its own), inboxRoute is «Входящие», anything else is a subject.
 * `reload` changes when files were added elsewhere (a drop on the window).
 */
export function SubjectsPage(props: {
  onOpen: (id: PageId) => void;
  reload: number;
  /** The route's choice: "" for none, inboxRoute for «Входящие», else a subject id; and the way to choose another. */
  subjectId: string;
  onSelect: (subjectId: string) => void;
  /** A row of «Ближайшее» opens its assignment (TASK-347); without it the rows only show the deadlines. */
  onOpenAssignment?: (assignmentId: string) => void;
  /** Counts the rail clicks: a click reads the remembered subject again (see landingFor). */
  visit?: number;
  /** «Указать почту» of a refused send: the form of one teacher to open on arrival (taken once, then onTeacherFocusDone). */
  teacherFocus?: TeacherFocus | null;
  onTeacherFocusDone?: () => void;
  /** «Завести предметы из расписания» of the schedule page: the review opens on arrival (taken once, then onReviewTaken). */
  openReview?: boolean;
  onReviewTaken?: () => void;
}) {
  const api = useMemo(() => libraryApi(callByName), []);
  const inboxApi = useMemo(() => libraryInboxApi(callByName), []);
  const notesClient = useMemo(() => makeNotesApi(callByName), []);
  const openId = props.subjectId;
  const [subjects, setSubjects] = useState<LibrarySubject[] | null>(null);
  const [inbox, setInbox] = useState<LibraryMaterial[]>([]);
  const [inboxLoaded, setInboxLoaded] = useState(false); // the first answer for «Входящие» (a failure counts: nothing to wait for)
  const [inboxError, setInboxError] = useState(false); // the last read of «Входящие» failed: its state is unknown
  const [materials, setMaterials] = useState<LibraryMaterial[] | null>(null);
  const [filesFailed, setFilesFailed] = useState(false); // the last read of the open subject's files failed
  const [form, setForm] = useState<Form>("none");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The name just typed belongs to a removed subject: «Предмет с таким названием удалён» and «Вернуть» (TASK-289).
  const [removedName, setRemovedName] = useState<RemovedName | null>(null);
  const [loadError, setLoadError] = useState("");
  const [note, setNote] = useState("");
  // The file being dragged from «Входящие» (the tree shows the cue) and the refusal of a drop on a subject.
  const [dragging, setDragging] = useState("");
  const [dropError, setDropError] = useState("");
  // Changes made here (a file placed, deleted, added into a subject): the files of the open subject are read again.
  const [changes, setChanges] = useState(0);
  // The result of «Добавить файл» on the page of a subject, and the refusal (with how to repeat it).
  const [added, setAdded] = useState<AddedState>(noAdded);
  const [addError, setAddError] = useState<{ text: string; again: () => void; origin: string } | null>(null);
  const running = useRef(false); // a double click comes before the re-render that disables the button
  const typedName = useRef(""); // the last name sent to createSubject: «Повторить» of a restore that found the subject changed

  // What is on screen. "" shows the subject remembered for this arrival (read again at a rail click, not at every render,
  // so «Назад» to "" shows the subject it showed before).
  const landing = useRef<Landing | null>(null);
  if (subjects !== null) {
    landing.current = landingFor(landing.current, props.visit ?? 0, browserStore());
  }
  const view = resolveView(openId, subjects, landing.current?.id ?? "");
  const shownId = shownSubjectId(view);
  const key = viewKey(view, openId);
  useEffect(() => rememberSubject(browserStore(), shownId), [shownId]);

  // Subjects from the schedule (TASK-312): the proposal asked for after the subjects (a failure shows «Не удалось
  // проверить расписание»), and the review opened over the screen with the proposal it started from.
  const [offer, setOffer] = useState<OfferState>(offerChecking);
  const [review, setReview] = useState<{ reply: ScheduleSubjectsReply; batches: readonly Batch[] } | null>(null);
  const reviewApi = useMemo(() => scheduleReviewApi(callByName), []);
  const subjectRefs = useMemo<SubjectRef[]>(() => (subjects ?? []).map((s) => ({ id: s.id, name: s.name, aliases: s.aliases })), [subjects]);
  const reviewWasOpen = useRef(false);
  useEffect(() => {
    // The panel closed: the focus goes back to the button that opened it, else to «Предмет» of the tree, else to the title.
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
        setInboxError(false);
        setInboxLoaded(true);
      },
      () => {
        setInboxError(true);
        setInboxLoaded(true);
      },
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
  // line «Не удалось проверить расписание» / «Повторить», and a retry that succeeds opens the review. It opens over the
  // subject the route's "" shows, but not over a subject or «Входящие» the student chose.
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

  // Another subject (or «Входящие»): the form, the error and the note of the previous one go away.
  useEffect(() => {
    setForm("none");
    setError("");
    setRemovedName(null);
    setNote("");
    setAdded(noAdded);
    setAddError(null);
    setDropError("");
    setDragging("");
    setMaterials(null);
    setFilesFailed(false);
    flow.reset();
  }, [key, flow]);

  // The overview of the open subject: loaded once per opening, then the opening is recorded (see SubjectOverviewFlow).
  // It does not depend on `reload`, so a dropped file does not count as a new visit.
  const [overviewState, setOverviewState] = useState<OverviewState>(idleOverview);
  useEffect(() => {
    if (shownId === "") {
      setOverviewState(idleOverview);
      return;
    }
    const flow = new SubjectOverviewFlow(api, setOverviewState);
    flow.open(shownId);
    return () => flow.close();
  }, [api, shownId]);

  const subject = subjects?.find((s) => s.id === shownId) ?? null;
  const known = subjects?.some((s) => s.id === openId) ?? false; // the route's own subject (for «Указать почту»)
  shown.current = subject;
  const mat = useMaterialActions({ api, inboxApi, subjects, setSubjects, inbox, hintsOn: view.kind === "inbox" || view.kind === "empty", refresh });
  const overview = overviewState.overview;
  const schedule = overview ? scheduleLine(overview.schedule) : "";
  const fresh = useMemo(() => freshMarks(overview), [overview]);

  // A reload (a dropped file) only refetches the files; an open form stays.
  useEffect(() => {
    if (subject === null) {
      return;
    }
    let active = true;
    api.materials(subject.id).then(
      (m) => {
        if (active) {
          setMaterials(m);
          setFilesFailed(false);
        }
      },
      () => active && setFilesFailed(true),
    );
    return () => {
      active = false;
    };
  }, [api, subject?.id, props.reload, changes]);

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

  const counts = useMemo(() => (subject && materials ? folderCounts(subject.folders, materials) : null), [subject, materials]);

  // A restored subject joins the list at once; the reload that follows brings the rest.
  const putSubject = (s: LibrarySubject) => setSubjects((list) => (list === null || list.some((x) => x.id === s.id) ? list : [...list, s]));

  // «Удалить предмет» (TASK-289): the question, the line with «Вернуть» that replaces the subject, the restore.
  const removal = useSubjectRemoval({
    api,
    notes: notesClient,
    // null while the files are read is "loading" for the dialog (its counts wait); null after a failure is "unknown".
    materials: filesFailed ? null : materials === null ? "loading" : materials,
    subjects,
    // The subject is gone: out of the list, and its page gives way to the list with the home subject open (a click in the
    // tree would do the same). Without subjects the route is the empty one and the tiles show.
    onGone: (id) => {
      const rest = (subjects ?? []).filter((s) => s.id !== id);
      setSubjects((list) => (list === null ? list : list.filter((s) => s.id !== id)));
      props.onSelect(homeSubjectId(rest, ""));
    },
    onReload: refresh,
    // «Вернуть» on the line leads back to the subject, as «Вернуть» in the create form does.
    onRestored: (s) => {
      putSubject(s);
      props.onSelect(s.id);
    },
  });

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

  // A click on the subject that is on screen is not a new place: no entry in the history.
  const openSubject = (id: string) => {
    if (opensNewPlace(view, id)) {
      props.onSelect(id);
    }
  };
  const openInbox = () => openSubject(inboxRoute);

  const createSubject = (name: string) =>
    run(async () => {
      typedName.current = name;
      setRemovedName(null);
      try {
        const s = await api.createSubject(name, "");
        setSubjects((list) => [...(list ?? []), s]);
        setForm("none");
        props.onSelect(s.id);
      } catch (err) {
        // The name belongs to a removed subject: «Вернуть» instead of «уже есть»; any other refusal is shown as it is.
        const removed = removedNameOf(err);
        if (removed === null) {
          throw err;
        }
        setRemovedName(removed);
      }
    });

  const createFolder = (name: string) =>
    run(async () => {
      const f = await api.createFolder(shownId, name);
      setSubjects((list) => (list ?? []).map((s) => (s.id === shownId ? { ...s, folders: [...s.folders, f] } : s)));
      setForm("none");
    });

  // «Добавить файл» in «Входящие» (and on the screen without subjects): the file lands in «Входящие».
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
      const r = await addFileInto(inboxApi, shownId, folderId, subjects ?? []);
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

  // A row of «Входящие» dropped on a subject of the tree: the same as «Разложить» into «Материалы», with the same undo line.
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
      placing={{ subjects: subjects ?? [], place: placeUnplaced, createSubject: mat.actions.createSubject, restoreSubject: mat.actions.restoreSubject }}
      busy={busy}
      error={addError !== null && addError.origin === origin ? addError.text : ""}
      onMoveHere={() => void moveHere()}
      onOpenSubject={openSubject}
      onOpenInbox={openInbox}
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
    <NameForm
      label={texts.subjectName}
      busy={busy}
      error={error}
      removed={
        removedName === null ? undefined : (
          <RemovedNameLine
            removed={removedName}
            name={typedName.current}
            // After `changed`: the create runs again and brings the removed subject with its fresh version.
            onChanged={() => void createSubject(typedName.current)}
            onDismiss={() => setRemovedName(null)}
            restore={(r) => runRestore(api, r.subjectId, r.version)}
            onSettled={(call) => {
              if (call.state.phase === "done" && call.subject !== null) {
                putSubject(call.subject);
                setRemovedName(null);
                setForm("none");
                props.onSelect(call.subject.id);
                refresh();
              } else if (restoreNeedsReload(call.state)) {
                refresh();
              }
            }}
          />
        )
      }
      onEdit={() => setRemovedName(null)}
      onSubmit={(n) => void createSubject(n)}
      onCancel={() => {
        setForm("none");
        setRemovedName(null);
      }}
    />
  );
  const inboxActions = { ...mat.actions, dragStart: setDragging, dragEnd: () => setDragging("") };
  // The subjects list did not load: the reason and the way on, in the same words wherever it shows.
  const loadFailed = loadError !== "" && (
    <ErrorLine
      text={loadError}
      onRefresh={refresh}
      onRetry={() => {
        setLoadError("");
        load();
      }}
    />
  );

  if (subjects === null) {
    return <section className="page">{loadError !== "" ? loadFailed : <p className="muted">{texts.loading}</p>}</section>;
  }

  const scheduleGroups = offer.reply !== null && offerStatus(offer) === "groups" ? offer.reply.groups.length : 0;
  const reviewPanel = review !== null && (
    <ScheduleSubjectsReview
      key="schedule-review"
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
  );
  // The schedule lines stay reachable on every screen of the section: above the tree, not inside a subject.
  const scheduleLines = review === null && (
    <>
      <KeptUndoLine state={kept.state} onUndo={kept.undo} onDismiss={kept.dismiss} onForget={kept.forget} />
      {subjects.length > 0 && scheduleGroups > 0 && <ScheduleOfferLine groups={scheduleGroups} disabled={undoBusy} onClick={openReview} />}
      {showCheckFailed(offer, subjects.length) && <ScheduleCheckFailedLine onRetry={retryOffer} />}
    </>
  );

  // No subjects: the tiles with the ways to make the first ones, and «Входящие» above them when it holds files. «Предмет»
  // is not in the head (EmptySubjects owns «Добавить предмет»), nor are «База знаний» and «Билеты» (both are empty here).
  const tiles = showsTiles(view);
  const tilesHead = tiles && (
    <div className="head">
      <div>
        <span className="label">Библиотека</span>
        <h1 id={titleId} tabIndex={-1}>
          {texts.title}
        </h1>
      </div>
      <LibraryActions onOpen={props.onOpen} onAddFile={() => void addFile()} addTip={fileTexts.addToInboxTip} busy={busy} links={false} />
    </div>
  );
  const tilesBody = (
    <>
      {subjectForm}
      {inboxError && <ErrorLine text={inboxFailedText} onRefresh={load} onRetry={load} />}
      <InboxList materials={inbox} actions={inboxActions} />
      {review === null && <EmptySubjects onAdd={() => setForm("subject")} offer={{ state: offer, onReview: openReview, onImport: () => props.onOpen("schedule"), onRetry: retryOffer, disabled: undoBusy }} />}
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
      {dropError !== "" && <ErrorLine text={dropError} onRefresh={refresh} onRetry={() => void cardDrop.again()} />}
      {undoLines}
    </>
  );

  const tree = (
    <SubjectTree
      subjects={subjects}
      currentId={shownId}
      counts={counts}
      freshFolders={fresh.folders}
      busy={busy}
      onOpen={openSubject}
      onAddSubject={() => setForm("subject")}
      onAddFolder={() => {
        setError("");
        setForm("folder");
      }}
      inbox={inboxEntry(inboxFiles(inbox), view.kind === "inbox", inboxLoaded && !inboxError)}
      onOpenInbox={openInbox}
      drop={
        // Only «Входящие» holds draggable rows, so only there the tree reserves the line for the cue.
        view.kind === "inbox"
          ? {
              dragging: dragging !== "",
              inboxIds,
              onDrop: (s, id) => void cardDrop.drop(s.id, id),
              onEnd: () => setDragging(""),
            }
          : undefined
      }
      addId={addSubjectId}
    />
  );

  const treeView = (
    <div className="subj">
      {tree}
      <div className="subj-main">
        {view.kind === "inbox" && (
          <>
            <div className="head">
              <div>
                <h1>{texts.inbox}</h1>
              </div>
              <LibraryActions onOpen={props.onOpen} onAddFile={() => void addFile()} addTip={fileTexts.addToInboxTip} busy={busy} />
            </div>
            {subjectForm}
            {inboxError && <ErrorLine text={inboxFailedText} onRefresh={load} onRetry={load} />}
            {note !== "" && (
              <ResultLine kind="ok" role="status">
                {note}
              </ResultLine>
            )}
            <InboxList materials={inbox} actions={inboxActions} bare loading={!inboxLoaded} failed={inboxError} />
            {form !== "subject" && error !== "" && (
              <ResultLine kind="fail" role="alert">
                {error}
              </ResultLine>
            )}
            {dropError !== "" && <ErrorLine text={dropError} onRefresh={refresh} onRetry={() => void cardDrop.again()} />}
            {undoLines}
          </>
        )}
        {view.kind === "subject" && subject !== null && (
          <>
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
              <LibraryActions onOpen={props.onOpen} onAddFile={() => void addFileTo("")} addTip={fileTexts.addToMaterialsTip} busy={busy}>
                <RemoveSubjectButton disabled={busy} onClick={(opener) => removal.open(subject, opener)} />
              </LibraryActions>
            </div>
            {filesFailed && <ErrorLine text={texts.filesFailed} onRefresh={refresh} onRetry={refresh} />}
            {addLines("")}
            {subjectForm}
            {overviewState.status === "failed" && (
              <ResultLine kind="fail" role="alert">
                {overviewState.error}
              </ResultLine>
            )}
            <TeachersBlock key={subject.id} teachers={subject.teachers} state={teacherStateFor(teacher, subject.id)} on={teacherActions} />
            {overview && <OverviewSections overview={overview} onOpenAssignment={props.onOpenAssignment} />}
            <SubjectDetail
              subject={subject}
              materials={materials}
              failed={filesFailed}
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
          </>
        )}
      </div>
    </div>
  );

  // A removed subject takes the place of its page: the line stays (above the list) until closed or the page is left. The
  // question is part of the block, so it keeps its place when the view changes.
  const removalBlock = (
    <>
      {removal.removed.map((r) => (
        <div className="sched-kept" key={r.id}>
          <RestoreRow
            idleText={removedLineText(r.name)}
            doneText=""
            restore={() => removal.restore(r)}
            onSettled={(call) => removal.settled(r, call)}
            onClose={() => removal.closeLine(r.id)}
            autoFocus
          />
        </div>
      ))}
      {removal.dialog !== null && (
        <RemoveSubjectDialog view={removal.dialog} returnTo={removal.returnTo} onAction={removal.act} onDismiss={removal.dismiss} />
      )}
    </>
  );

  // One return for both views: the schedule review sits at the same place of the same element, so it keeps its state
  // when the first subjects are made from it (the empty view turns into the tree view without remounting it).
  return (
    <section className="page">
      {tilesHead}
      {loadFailed}
      {reviewPanel}
      {scheduleLines}
      {removalBlock}
      {tiles ? tilesBody : treeView}
    </section>
  );
}
