import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { formatTime } from "../activityView";
import { CollapsibleList } from "../CollapsibleList";
import { assignmentAfterDone, defaultState, doneTexts, listAfterDone, listEmpty } from "../assignmentDoneView";
import {
  type AssignmentDetail,
  type AssignmentItem,
  type AssignmentState,
  type ChangeAssignmentResult,
  type DeliveryItem,
  type GenerationStatus,
  type HomeworkApi,
  homeworkApi,
  type SendFormat,
  type SendPreparation,
  type VersionText,
} from "../bindings/homework";
import { libraryApi } from "../bindings/library";
import { callByName } from "../bindings/runtime";
import { turnkeyApi } from "../bindings/turnkey";
import {
  addTexts,
  assignmentCounts,
  assignmentDeliveryView,
  type AssignmentFilter,
  changeOutcome,
  changeTexts,
  flightGuard,
  removeLabel,
  filterAssignments,
  blockedByEmail,
  type ChipView,
  defaultSendFormat,
  deliveriesInFlight,
  deliveryPollMs,
  deliveryView,
  docxSaveView,
  draftPollMs,
  draftStatusStep,
  dueText,
  errorText,
  type FollowedGeneration,
  followGeneration,
  generateOffer,
  generatingLabel,
  generationGoesOn,
  generationPollMs,
  generationRunning,
  generationView,
  isStale,
  sendFormatChange,
  versionTitle,
} from "../homeworkView";
import { lessonText } from "../lessonLabel";
import { initialListState, type ListState } from "../listView";
import { useRecipientNav } from "../recipientNav";
import { isRefusal, type RecipientView, recipientView, refusalAction } from "../recipientView";
import { newestOnly } from "../todayView";
import { Button } from "../ui/Button";
import { Banner, Card, EmptyState, Field, Input, ResultLine, type ResultKind, Row } from "../ui/controls";
import { StatusChip } from "../ui/StatusChip";
import { Toast } from "../ui/Toast";
import { AssignmentRowView, DoneButton, DoneIssueLine, DoneStamp } from "./AssignmentDoneParts";
import { AssignmentStateSwitch, FilterCounters, filterEmpty, filterReset } from "./AssignmentFilter";
import { AddAssignmentDialog, EditAssignmentDialog, RemovedLine, SendAgainDialog, SendDialog, type SendResult } from "./HomeworkDialogs";
import { HomeworkTurnkey } from "./HomeworkTurnkey";
import { LessonCorrection } from "./LessonCorrection";
import { RefusalButton } from "./RecipientParts";
import { TranscriptLinks } from "./TranscriptLinks";
import { doneToastProps, useDoneFocus } from "./useDoneFocus";
import { useDoneRunner } from "./useDoneRunner";

// «Домашние задания» (PRD section 10): the assignments, and for the opened one the draft editor, the extra
// instructions with «Сгенерировать заново», the version history, the export to a file and the send by email through
// the confirmation dialog (AC-13, AC-14, AC-15). The core decides what can be sent and what to offer for an email; the
// page shows it. Whether a draft is being generated comes from the core too (TASK-094): the page asks it while open,
// so a generation started earlier or elsewhere and a version made meanwhile show up without opening the page again. Assignment, draft and instruction texts may be model output: they are plain React text and textarea
// values only, never HTML (hard rule 3).
// openAssignment: the assignment shown (a jump from the summary, TASK-087, or a card opened from the list); "" shows the
// list. It comes from the window route, so «Назад» and «Вперёд» restore it (TASK-202); onSelect asks the route to change.
// Without onSelect (a test, a bare page) the page keeps the choice itself.
// editAssignment: the assignment whose «Изменить» opens on arrival (TASK-238, «Выбрать предмет» of the agent's card);
// onEditTaken tells the window the request was taken, so it fires once.
export function HomeworkPage(props: { openAssignment?: string; onSelect?: (id: string) => void; editAssignment?: string; onEditTaken?: () => void }) {
  const api = useMemo(() => homeworkApi(callByName), []);
  const [own, setOwn] = useState(props.openAssignment ?? "");
  const openId = props.onSelect ? (props.openAssignment ?? "") : own;
  const select = props.onSelect ?? setOwn;

  return openId === "" ? (
    <AssignmentList api={api} onOpen={select} />
  ) : (
    <AssignmentEditor
      api={api}
      id={openId}
      editOnOpen={props.editAssignment === openId}
      onEditTaken={props.onEditTaken}
      onBack={() => select("")}
    />
  );
}

function Chip(props: { view: ChipView }) {
  return (
    <StatusChip kind={props.view.kind} icon={props.view.icon}>
      {props.view.label}
    </StatusChip>
  );
}

function MessageLine(props: { message: Message }) {
  const m = props.message;
  return (
    <ResultLine kind={m.kind} role={m.kind === "fail" ? "alert" : "status"}>
      {m.text}
    </ResultLine>
  );
}

/** The chip of a row is its newest email; a failed load leaves the row without a chip. */
async function fetchDeliveries(api: HomeworkApi, as: readonly AssignmentItem[]) {
  const ds = await Promise.allSettled(as.map((a) => api.deliveries(a.id)));
  const byId: Record<string, DeliveryItem[]> = {};
  ds.forEach((r, i) => {
    if (r.status === "fulfilled") {
      byId[as[i].id] = r.value;
    }
  });
  // When every call was rejected the split is unknown, not «all active»: the counters stay hidden.
  return { byId, loaded: ds.length === 0 || ds.some((r) => r.status === "fulfilled") };
}

function AssignmentList(props: { api: HomeworkApi; onOpen: (id: string) => void }) {
  const { api } = props;
  const [items, setItems] = useState<AssignmentItem[] | null>(null);
  // Deleted in this visit: the stored assignment at its new version, which «Вернуть» sends back.
  const [removed, setRemoved] = useState<Readonly<Record<string, AssignmentItem>>>({});
  const [restoredId, setRestoredId] = useState("");
  const [busyId, setBusyId] = useState("");
  const [rowProblem, setRowProblem] = useState<{ id: string; text: string } | null>(null);
  const removedRef = useRef(removed);
  removedRef.current = removed;
  const guard = useRef(flightGuard());
  const [deliveries, setDeliveries] = useState<Readonly<Record<string, DeliveryItem[]>>>({});
  const [deliveriesLoaded, setDeliveriesLoaded] = useState(false);
  const [filter, setFilter] = useState<AssignmentFilter>("all");
  const [error, setError] = useState("");
  const [list, setList] = useState<ListState>(initialListState);
  const [adding, setAdding] = useState(false);
  const library = useMemo(() => libraryApi(callByName), []);
  // «Открытые / Выполненные / Все» (TASK-316). A slow answer of an earlier request never replaces a newer one.
  const [state, setState] = useState<AssignmentState>(defaultState);
  const stateRef = useRef(state);
  stateRef.current = state;
  const itemsRef = useRef(items);
  itemsRef.current = items;
  const requests = useRef(newestOnly());
  // Whether the student has any assignment at all (null: not known): an empty «Открытые» is not the first run.
  const [anyExists, setAnyExists] = useState<boolean | null>(null);
  const [shownOnce, setShownOnce] = useState(false);

  useEffect(() => {
    let live = true;
    const token = requests.current.next();
    void (async () => {
      try {
        const as = await api.listAssignments(state);
        if (!live || !requests.current.isNewest(token)) {
          return;
        }
        if (as.length > 0) {
          setAnyExists(true);
        } else if (state === "open") {
          // Nothing open: ask once whether anything exists, to tell «все выполнены» from the first run.
          const everything = await api.listAssignments("all").then(
            (all) => all.length > 0,
            () => null,
          );
          if (!live || !requests.current.isNewest(token)) {
            return;
          }
          setAnyExists(everything);
        } else if (state === "all") {
          setAnyExists(false);
        }
        setItems(as);
        setShownOnce(true);
        setError("");
        const d = await fetchDeliveries(api, as);
        if (!live || !requests.current.isNewest(token)) {
          return;
        }
        setDeliveries(d.byId);
        setDeliveriesLoaded(d.loaded);
      } catch (err) {
        if (live && requests.current.isNewest(token)) {
          setError(errorText(err));
        }
      }
    })();
    return () => {
      live = false;
    };
  }, [api, state]);

  // Another mark: the list of that mark is read; until it comes the rows of the old one are not shown.
  function changeState(next: AssignmentState) {
    if (next === state) {
      return;
    }
    setState(next);
    setItems(null);
    setDeliveries({});
    setDeliveriesLoaded(false);
    setList(initialListState);
    setRowProblem(null);
    // The email filter is about sending: it starts again with «Все», and a refusal about a gone row is not carried over.
    setFilter("all");
    doneRun.dismissIssue();
  }

  // After a refusal that says the list is out of date, the list is read again (the removed lines stay).
  async function reloadItems() {
    const token = requests.current.next();
    try {
      const as = await api.listAssignments(stateRef.current);
      if (!requests.current.isNewest(token)) {
        return;
      }
      // An assignment that is live in the fresh list was restored elsewhere: its «Вернуть» line goes.
      if (as.some((x) => Object.hasOwn(removedRef.current, x.id))) {
        setRemoved((cur) => Object.fromEntries(Object.entries(cur).filter(([rid]) => !as.some((y) => y.id === rid))));
      }
      // The core no longer lists the assignments deleted in this visit; their lines stay for «Вернуть».
      setItems((prev) => [
        ...as,
        ...(prev ?? []).filter((x) => Object.hasOwn(removedRef.current, x.id) && !as.some((y) => y.id === x.id)),
      ]);
      setShownOnce(true);
      // This call took the token of a load that may still be on its way (it would have set the chips): the chips and
      // counters are read here too, or they would stay hidden.
      const d = await fetchDeliveries(api, as);
      if (requests.current.isNewest(token)) {
        setDeliveries(d.byId);
        setDeliveriesLoaded(d.loaded);
      }
    } catch (err) {
      // The old list stays; the refusal text already asks to repeat. Without an old list (it was being read when this
      // call took over) the reason is shown instead of an endless «Загрузка…».
      if (itemsRef.current === null && requests.current.isNewest(token)) {
        setError(errorText(err));
      }
    }
  }

  // «Выполнено» of a row (TASK-316): the row takes the stored state at once, the list is read again after it.
  const rootRef = useRef<HTMLElement>(null);
  const focus = useDoneFocus(rootRef);
  const doneRun = useDoneRunner(api, {
    apply: (res) => {
      const cur = itemsRef.current;
      if (cur === null) {
        return;
      }
      const next = listAfterDone(cur, stateRef.current, res, new Date().toISOString());
      // A row that leaves takes the focus with it: it goes to the next row's check.
      focus.rowsLeaving(cur.filter((a) => !next.some((n) => n.id === a.id)).map((a) => a.id));
      itemsRef.current = next;
      setItems(next);
    },
    refresh: reloadItems,
  });

  async function change(a: AssignmentItem, call: () => Promise<ChangeAssignmentResult>, done: (next: AssignmentItem) => void) {
    if (guard.current.has(a.id)) {
      return; // a second click while the first call runs: it must not touch the first one's busy state
    }
    setRowProblem(null);
    setBusyId(a.id);
    try {
      const r = await guard.current.run(a.id, call);
      if (r === null) {
        return;
      }
      const out = changeOutcome(r);
      if (out.assignment !== null) {
        done(out.assignment);
        return;
      }
      setRowProblem({ id: a.id, text: out.text });
      if (out.reload) {
        await reloadItems();
      }
    } finally {
      setBusyId((cur) => (cur === a.id ? "" : cur));
    }
  }

  const remove = (a: AssignmentItem) =>
    change(
      a,
      () => api.deleteAssignment(a.id, a.version),
      (next) => {
        setRestoredId("");
        setRemoved((cur) => ({ ...cur, [a.id]: next }));
      },
    );

  const restore = (a: AssignmentItem, gone: AssignmentItem) =>
    change(
      a,
      () => api.restoreAssignment(a.id, gone.version),
      (next) => {
        setRemoved((cur) => {
          const { [a.id]: _gone, ...rest } = cur;
          return rest;
        });
        setItems((cur) => (cur === null ? cur : cur.map((x) => (x.id === a.id ? next : x))));
        setRestoredId(a.id);
      },
    );

  const row = (a: AssignmentItem) => {
    const problem = rowProblem !== null && rowProblem.id === a.id ? rowProblem.text : "";
    const problemLine = problem !== "" && (
      <ResultLine kind="fail" role="alert">
        {problem}
      </ResultLine>
    );
    if (Object.hasOwn(removed, a.id)) {
      return (
        <Row
          key={a.id}
          title={<RemovedLine busy={busyId === a.id} onRestore={() => void restore(a, removed[a.id])} />}
          below={problemLine}
        />
      );
    }
    const chip = assignmentDeliveryView(Object.hasOwn(deliveries, a.id) ? deliveries[a.id] : undefined);
    // A call of this row runs: the small text says which. Deleting and marking do not run together.
    const doneBusy = doneRun.busyId === a.id;
    const busyText = doneBusy ? doneTexts.saving : busyId === a.id ? changeTexts.removing : "";
    const doneIssue = doneRun.issue !== null && doneRun.issue.ids.includes(a.id) ? doneRun.issue : null;
    return (
      <AssignmentRowView
        key={a.id}
        assignment={a}
        status={chip && <Chip view={chip} />}
        busyText={busyText}
        focusOpen={restoredId === a.id}
        onOpen={() => props.onOpen(a.id)}
        onToggleDone={() => {
          if (busyText === "") {
            void doneRun.mark(a.id, !a.done, a.title);
          }
        }}
        onRemove={() => void remove(a)}
        below={
          <>
            {problemLine}
            {doneIssue !== null && (
              <DoneIssueLine issue={doneIssue} where="list" onRetry={doneIssue.retry} onOpen={props.onOpen} />
            )}
          </>
        }
      />
    );
  };

  // The way out of an empty email group.
  const resetFilter = () => {
    setFilter("all");
    setList(initialListState);
  };

  const live = items === null ? [] : items.filter((a) => !Object.hasOwn(removed, a.id));
  const shown = items === null ? [] : filterAssignments(items, filter, deliveries, deliveriesLoaded);
  // A refusal about a row that is no longer in the list (it was deleted elsewhere) stays above the list.
  const strayIssue =
    doneRun.issue !== null && !(items ?? []).some((x) => doneRun.issue?.ids.includes(x.id)) ? doneRun.issue : null;
  const empty = items !== null && items.length === 0 ? listEmpty(state, anyExists) : null;

  return (
    <section className="page hw" tabIndex={-1} ref={rootRef}>
      <div className="head">
        <h1>Домашние задания</h1>
        <div className="acts">
          <Button icon="plus" onClick={() => setAdding(true)}>
            {addTexts.button}
          </Button>
        </div>
      </div>
      {adding && (
        <AddAssignmentDialog
          api={api}
          library={library}
          onClose={() => setAdding(false)}
          onCreated={(a) => {
            setAdding(false);
            setItems((cur) => (cur === null ? cur : [a, ...cur.filter((x) => x.id !== a.id)]));
            props.onOpen(a.id);
          }}
        />
      )}
      {error && (
        <Banner kind="fail" role="alert">
          {error}
        </Banner>
      )}
      {strayIssue !== null && (
        <DoneIssueLine
          issue={strayIssue}
          where="list"
          onRetry={strayIssue.retry}
          onOpen={props.onOpen}
          onClose={doneRun.dismissIssue}
        />
      )}
      {!shownOnce ? (
        items === null && !error && <p className="muted">Загрузка…</p>
      ) : empty !== null && empty.kind === "first-run" ? (
        <EmptyState label="Заданий пока нет" title="Здесь появятся домашние задания">
          Задание находится само в записи пары, после конспекта. Здесь можно сгенерировать по нему черновик, поправить его
          и отправить преподавателю по почте.
        </EmptyState>
      ) : (
        <>
          <div className="hw-filters">
            <AssignmentStateSwitch value={state} onChange={changeState} />
            {/* The email filter is about sending: it makes no sense for the done ones. */}
            {items !== null && items.length > 0 && state !== "done" && (
              <FilterCounters
                counts={assignmentCounts(live, deliveries, deliveriesLoaded)}
                value={filter}
                onChange={(f) => {
                  setFilter(f);
                  setList(initialListState);
                }}
              />
            )}
          </div>
          {items === null ? (
            !error && <p className="muted">Загрузка…</p>
          ) : empty !== null && empty.kind === "note" ? (
            <div className="hw-none">
              <p className="muted">{empty.text}</p>
              {empty.hint !== "" && <p className="small muted">{empty.hint}</p>}
              {empty.action !== null && (
                <Button
                  onClick={() => {
                    if (empty.action !== null) {
                      changeState(empty.action.state);
                    }
                  }}
                >
                  {empty.action.label}
                </Button>
              )}
            </div>
          ) : shown.length === 0 ? (
            <div className="hw-none">
              <p className="muted">{filterEmpty}</p>
              <Button onClick={resetFilter}>{filterReset}</Button>
            </div>
          ) : (
            <CollapsibleList
              items={shown}
              itemKey={(a) => a.id}
              renderItem={row}
              renderContainer={(rows) => (
                <div className="card rows" role="list">
                  {rows}
                </div>
              )}
              state={list}
              onStateChange={setList}
              className="clist-card"
              classNames={{ button: "btn ghost" }}
            />
          )}
        </>
      )}
      <Toast {...doneToastProps(doneRun, focus)} />
    </section>
  );
}

interface Message {
  kind: ResultKind;
  text: string;
  /** Shown under the draft or under the emails, next to the button that led to it. */
  at: "draft" | "mail";
}

/**
 * The open send dialog: the core's preparation and the version written to the attached file; switching while another
 * format is prepared, problem why the last switch failed ("" none).
 */
interface SendOpen {
  prep: SendPreparation;
  version: number;
  switching: boolean;
  problem: string;
}

function AssignmentEditor(props: { api: HomeworkApi; id: string; editOnOpen?: boolean; onEditTaken?: () => void; onBack: () => void }) {
  const { api, id, editOnOpen, onEditTaken } = props;
  const turnkey = useMemo(() => turnkeyApi(callByName), []);
  const [detail, setDetail] = useState<AssignmentDetail | null>(null);
  const [loadError, setLoadError] = useState("");
  // The version shown in the editor (its saved text) and the text in the fields.
  const [shown, setShown] = useState<VersionText | null>(null);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [instruction, setInstruction] = useState("");
  const [gen, setGen] = useState<FollowedGeneration | null>(null);
  const [deliveries, setDeliveries] = useState<DeliveryItem[] | null>(null);
  const [deliveriesError, setDeliveriesError] = useState("");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<Message | null>(null);
  const [sendOpen, setSendOpen] = useState<SendOpen | null>(null);
  // A send the core refused for its recipient (no address, no subject): the text and the way out, under the buttons.
  const [refusal, setRefusal] = useState<Extract<RecipientView, { kind: "no_email" | "no_subject" }> | null>(null);
  const nav = useRecipientNav();
  // The attachment the next send dialog starts with: the student's last choice on this page.
  const [sendFormat, setSendFormat] = useState<SendFormat>(defaultSendFormat);
  const [sendAgain, setSendAgain] = useState<DeliveryItem | null>(null);
  const [correcting, setCorrecting] = useState(false);
  // The dialog is drawn once the assignment is loaded, so a request made at arrival simply starts it open.
  const [editing, setEditing] = useState(editOnOpen === true);
  useEffect(() => {
    if (editOnOpen === true) {
      onEditTaken?.();
    }
  }, [editOnOpen, onEditTaken]);
  const [gone, setGone] = useState<AssignmentItem | null>(null);
  const [changing, setChanging] = useState(false);
  const [restored, setRestored] = useState(false); // «Вернуть» was pressed: «Удалить» takes the focus
  const [changeProblem, setChangeProblem] = useState("");
  const library = useMemo(() => libraryApi(callByName), []);
  const mounted = useRef(true);
  const acting = useRef(false); // one action at a time; a double click must not start a second one
  const loadingDeliveries = useRef(false);
  const deliveriesAgain = useRef(false);
  // The send the open dialog shows: cancelled in the core if the page goes away without an answer.
  const pendingSend = useRef("");
  const switchingFormat = useRef(false);
  // What the draft poll compares with and how it may reload; updated on every render.
  const known = useRef({ latest: 0, job: "", keepText: false, keepShown: false });
  // Asks the core at once instead of at the next tick (after a refused second generation).
  const pollDraftNow = useRef<() => void>(() => undefined);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      const p = pendingSend.current;
      pendingSend.current = "";
      if (p !== "") {
        void api.cancelSend(p).catch(() => undefined);
      }
    };
  }, [api]);

  // keepText: the fields keep what the student typed; otherwise they show the latest version. keepShown: the shown
  // version and the fields stay (an older version is open or a send dialog shows this one).
  const reload = useCallback(
    async (keepText: boolean, keepShown = false) => {
      const d = await api.openAssignment(id);
      if (!mounted.current) {
        return;
      }
      setDetail(d);
      // The core opened it, so it is live: a «Вернуть» line left from a deletion would only loop on «changed».
      setGone(null);
      const running = d.generation;
      if (running !== null) {
        setGen((g) => (g !== null && g.jobId === running.jobId ? g : followGeneration(running)));
      }
      if (keepShown) {
        return;
      }
      setShown(d.latest);
      if (!keepText) {
        setTitle(d.latest?.title ?? "");
        setBody(d.latest?.body ?? "");
      }
    },
    [api, id],
  );

  // One load at a time: a refresh asked for during a load runs once more after it.
  const loadDeliveries = useCallback(async () => {
    if (loadingDeliveries.current) {
      deliveriesAgain.current = true;
      return;
    }
    loadingDeliveries.current = true;
    try {
      do {
        deliveriesAgain.current = false;
        try {
          const ds = await api.deliveries(id);
          if (mounted.current) {
            setDeliveries(ds);
            setDeliveriesError("");
          }
        } catch (err) {
          if (mounted.current) {
            setDeliveriesError(errorText(err));
          }
        }
      } while (deliveriesAgain.current && mounted.current);
    } finally {
      loadingDeliveries.current = false;
    }
  }, [api, id]);

  useEffect(() => {
    reload(false).catch((err) => {
      if (mounted.current) {
        setLoadError(errorText(err));
      }
    });
    void loadDeliveries();
  }, [reload, loadDeliveries]);

  // Emails on their way change by themselves.
  const inFlight = deliveries !== null && deliveriesInFlight(deliveries);
  useEffect(() => {
    if (!inFlight) {
      return;
    }
    const timer = setInterval(() => void loadDeliveries(), deliveryPollMs);
    return () => clearInterval(timer);
  }, [inFlight, loadDeliveries]);

  // A running generation is asked for again only after the previous answer, so calls never overlap.
  const jobId = gen?.jobId ?? "";
  useEffect(() => {
    if (jobId === "") {
      return;
    }
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
      let status: GenerationStatus;
      try {
        status = await api.generationStatus(jobId);
      } catch (err) {
        if (!stopped) {
          setGen(null);
          setMessage({ kind: "fail", text: errorText(err), at: "draft" });
        }
        return;
      }
      if (stopped) {
        return;
      }
      setGen({ jobId, status });
      if (generationGoesOn(status.state)) {
        timer = setTimeout(() => void tick(), generationPollMs);
      } else if (status.state === "done") {
        setInstruction("");
        try {
          await reload(false);
        } catch (err) {
          if (!stopped) {
            setMessage({ kind: "fail", text: errorText(err), at: "draft" });
          }
        }
      }
    };
    timer = setTimeout(() => void tick(), generationPollMs);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [api, jobId, reload]);

  // While the page is open it asks the core for a newer version and for a generation it does not follow yet. One call
  // at a time; a failed one is retried at the next tick.
  useEffect(() => {
    let stopped = false;
    let inFlight = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
      inFlight = true;
      try {
        const st = await api.draftStatus(id);
        if (stopped) {
          return;
        }
        const k = known.current;
        const step = draftStatusStep(st, k.latest, k.job);
        if (step.follow !== null) {
          setGen(step.follow);
        }
        if (step.reload) {
          await reload(k.keepText, k.keepShown);
        }
      } catch {
        // Nothing to show: the page keeps what it has and asks again.
      } finally {
        inFlight = false;
      }
      if (!stopped) {
        timer = setTimeout(() => void tick(), draftPollMs);
      }
    };
    pollDraftNow.current = () => {
      if (!stopped && !inFlight) {
        clearTimeout(timer);
        void tick();
      }
    };
    timer = setTimeout(() => void tick(), draftPollMs);
    return () => {
      stopped = true;
      clearTimeout(timer);
      pollDraftNow.current = () => undefined;
    };
  }, [api, id, reload]);

  async function act(key: string, action: () => Promise<void>, at: Message["at"] = "draft") {
    if (acting.current) {
      return;
    }
    acting.current = true;
    setBusy(key);
    setMessage(null);
    setRefusal(null);
    try {
      await action();
    } catch (err) {
      if (mounted.current) {
        // A refused second generation is no failure: the running one is picked up and shown.
        const running = generationRunning(err);
        setMessage({ kind: running ? "wait" : "fail", text: errorText(err), at });
        if (running) {
          pollDraftNow.current();
        }
        if (isStale(err)) {
          // Unsaved text stays in the fields, now over the new latest version.
          await reload(dirty).catch(() => undefined);
        }
        if (blockedByEmail(err)) {
          void loadDeliveries();
        }
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setBusy("");
      }
    }
  }

  const latest = detail?.latest ?? null;
  const isLatest = shown !== null && latest !== null && shown.number === latest.number;
  const dirty = isLatest && (title !== shown.title || body !== shown.body);
  const generating = gen !== null && generationGoesOn(gen.status.state);
  const readOnly = !isLatest || generating;
  const idle = busy === "" && sendOpen === null && sendAgain === null;
  const savedReason = dirty ? "Сначала сохраните или отмените правку." : undefined;
  const offer = generateOffer({ hasDraft: latest !== null, generating, olderShown: shown !== null && !isLatest, dirty });
  known.current = {
    latest: latest?.number ?? 0,
    job: gen?.jobId ?? "",
    keepText: dirty,
    keepShown: (shown !== null && !isLatest) || sendOpen !== null,
  };

  const save = () =>
    act("save", async () => {
      if (shown === null) {
        return;
      }
      // The base is the shown version: the core refuses the edit if a newer one appeared meanwhile.
      const v = await api.editDraft(id, shown.number, title, body);
      await reload(false);
      setMessage({ kind: "ok", text: `Правка сохранена: версия ${v.number}.`, at: "draft" });
    });

  const revert = () => {
    if (shown !== null) {
      setTitle(shown.title);
      setBody(shown.body);
    }
  };

  const generate = () =>
    act("generate", async () => {
      // A new version always starts from the latest one; the button is off while an older one is shown.
      const g = await api.generate(id, latest?.number ?? 0, instruction);
      setGen({ jobId: g.jobId, status: { state: "generating", message: "", percent: 0 } });
    });

  const showVersion = (n: number) =>
    act("version", async () => {
      const v = latest !== null && n === latest.number ? latest : await api.draftVersion(id, n);
      setShown(v);
      setTitle(v.title);
      setBody(v.body);
    });

  // An older text continues as an edit of the latest version: saving it adds a new version.
  const takeText = () => {
    if (latest !== null) {
      setShown(latest);
    }
  };

  const exportFile = () =>
    act("export", async () => {
      if (shown === null) {
        return;
      }
      const r = await api.exportDraft(id, shown.number);
      setMessage({ kind: "ok", text: `Версия ${r.version} сохранена в файл «${r.fileName}».`, at: "draft" });
    });

  // «Сохранить в Word (.docx)»: the core asks where in the native save dialog and answers with a fixed text. The email
  // attachment is not touched here: prepareSendAs exports the shown version right before the send dialog.
  const saveWord = () =>
    act("docx", async () => {
      if (shown === null) {
        return;
      }
      const r = await api.saveDocx(id, shown.number);
      if (mounted.current) {
        const v = docxSaveView(r);
        setMessage({ kind: v.kind, text: v.text, at: "draft" });
      }
    });

  // Version n goes to the file the email attaches in format; the core then checks the send and fills the dialog. A
  // refused export (exportLocked: an earlier email is unresolved) throws here, so no send is prepared.
  async function prepareSendAs(format: SendFormat, n: number): Promise<SendOpen> {
    const exported = format === "docx" ? await api.exportDocx(id, n) : await api.exportDraft(id, n);
    const prep = await api.prepareSend(id, format);
    return { prep, version: exported.version, switching: false, problem: "" };
  }

  // A refused preparation: a refusal for its recipient comes with its way out, any other reason is the core's message.
  const showRefused = (prep: SendPreparation) => {
    const v = recipientView(prep);
    if (isRefusal(v)) {
      setRefusal(v);
    } else {
      setMessage({ kind: "fail", text: prep.message, at: "draft" });
    }
  };

  const startSend = () =>
    act("send", async () => {
      if (shown === null) {
        return;
      }
      const open = await prepareSendAs(sendFormat, shown.number);
      const prep = open.prep;
      if (!mounted.current) {
        if (prep.status === "ready") {
          void api.cancelSend(prep.proposalId).catch(() => undefined); // nobody saw this dialog
        }
        return;
      }
      if (prep.status === "ready") {
        pendingSend.current = prep.proposalId;
        setSendOpen(open);
      } else {
        showRefused(prep);
      }
    });

  // Another attachment format in the dialog: a new export and preparation of the same version. Only a ready one
  // replaces the shown send, which is then cancelled; otherwise the dialog keeps it and says why.
  async function switchFormat(format: SendFormat) {
    const open = sendOpen;
    if (open === null || switchingFormat.current || !sendFormatChange(open.prep.attachmentFormat, format, open.switching)) {
      return;
    }
    switchingFormat.current = true;
    setSendOpen({ ...open, switching: true, problem: "" });
    try {
      const next = await prepareSendAs(format, open.version);
      if (!mounted.current) {
        if (next.prep.status === "ready") {
          void api.cancelSend(next.prep.proposalId).catch(() => undefined); // nobody saw this dialog
        }
        return;
      }
      if (next.prep.status === "ready") {
        const old = pendingSend.current;
        pendingSend.current = next.prep.proposalId;
        setSendOpen(next);
        setSendFormat(format);
        if (old !== "") {
          void api.cancelSend(old).catch(() => undefined);
        }
      } else {
        setSendOpen({ ...open, switching: false, problem: next.prep.message });
      }
    } catch (err) {
      if (mounted.current) {
        setSendOpen({ ...open, switching: false, problem: errorText(err) });
        if (blockedByEmail(err)) {
          void loadDeliveries();
        }
      }
    } finally {
      switchingFormat.current = false;
    }
  }

  const sendClosed = (result: SendResult) => {
    pendingSend.current = "";
    setSendOpen(null);
    // After a failed confirmation the dialog has shown the reason; the list of emails tells the rest.
    if (result === "sent") {
      setMessage({ kind: "ok", text: "Письмо отправляется: его состояние — в списке «Письма» ниже.", at: "draft" });
    } else if (result === "cancelled") {
      setMessage({ kind: "wait", text: "Отправка отменена: письмо не отправлено.", at: "draft" });
    }
    void loadDeliveries();
  };

  const sendAgainClosed = (sent: boolean) => {
    setSendAgain(null);
    if (sent) {
      void loadDeliveries();
    }
  };

  const markSent = (d: DeliveryItem) =>
    act(
      `mark:${d.id}`,
      async () => {
        await api.markSent(d.id);
        await loadDeliveries();
      },
      "mail",
    );

  // «Удалить» and «Вернуть» of this assignment; gone is the stored assignment at its new version, shown until the
  // student leaves the page. The guard keeps a double click to one call.
  const removeGuard = useRef(flightGuard());
  async function changeThis(call: () => Promise<ChangeAssignmentResult>, done: (next: AssignmentItem) => void) {
    if (removeGuard.current.has(id)) {
      return; // a second click while the first call runs: it must not touch the first one's busy state
    }
    setChangeProblem("");
    setChanging(true);
    try {
      const r = await removeGuard.current.run(id, call);
      if (r === null || !mounted.current) {
        return;
      }
      const out = changeOutcome(r);
      if (out.assignment !== null) {
        done(out.assignment);
        return;
      }
      setChangeProblem(out.text);
      if (out.reload) {
        await reload(true).catch(() => undefined);
      }
    } finally {
      if (mounted.current) {
        setChanging(false);
      }
    }
  }

  // «Выполнено» / «Вернуть в работу» of this assignment (TASK-316): the page takes the stored state at once and reads
  // the assignment again; the toast and a refusal belong to this page.
  // The re-read keeps the version the student is looking at (keepShown): marking done is not a change of the draft.
  const rootRef = useRef<HTMLElement>(null);
  const focus = useDoneFocus(rootRef);
  const doneRun = useDoneRunner(api, {
    apply: (res) =>
      setDetail((d) =>
        d === null ? d : { ...d, assignment: assignmentAfterDone(d.assignment, res, new Date().toISOString()) },
      ),
    refresh: () => reload(true, true).catch(() => undefined),
  });

  if (detail === null) {
    return (
      <section className="page hw">
        <div className="hw-back">
          <Button variant="ghost" onClick={props.onBack}>
            Все задания
          </Button>
        </div>
        {loadError ? (
          <Banner kind="fail" role="alert">
            {loadError}
          </Banner>
        ) : (
          <p className="muted">Загрузка…</p>
        )}
      </section>
    );
  }

  const a = detail.assignment;
  const lesson = lessonText(a);
  const versions = [...detail.versions].reverse(); // newest first
  const genView = gen && generationView(gen.status);

  if (gone !== null) {
    return (
      <section className="page hw">
        <div className="hw-back">
          <Button variant="ghost" onClick={props.onBack}>
            Все задания
          </Button>
        </div>
        <RemovedLine
          busy={changing}
          onRestore={() =>
            void changeThis(
              () => api.restoreAssignment(id, gone.version),
              (next) => {
                setDetail((d) => (d === null ? d : { ...d, assignment: next }));
                setGone(null);
                setRestored(true);
              },
            )
          }
        />
        {changeProblem && (
          <ResultLine kind="fail" role="alert">
            {changeProblem}
          </ResultLine>
        )}
      </section>
    );
  }

  // While the mark is saved, «Изменить» and «Удалить» wait: a delete would go out with a version the mark is changing.
  const doneBusy = doneRun.busyId === id;

  return (
    <section className="page hw" tabIndex={-1} ref={rootRef}>
      <div className="hw-back">
        <Button variant="ghost" onClick={props.onBack}>
          Все задания
        </Button>
      </div>
      <div className="head">
        <h1 className="hw-title">{a.title}</h1>
        <span className="muted num">{dueText(a.due)}</span>
        <div className="acts">
          <DoneButton
            done={a.done}
            busy={doneBusy}
            disabled={changing}
            onToggle={() => void doneRun.mark(id, !a.done, a.title)}
          />
          <Button disabled={changing || doneBusy} onClick={() => setEditing(true)}>
            {changeTexts.edit}
          </Button>
          <Button
            variant="danger"
            aria-label={removeLabel(a.title)}
            autoFocus={restored}
            busy={changing}
            busyLabel={changeTexts.removing}
            disabled={changing || doneBusy}
            onClick={() =>
              void changeThis(
                () => api.deleteAssignment(id, a.version),
                (next) => {
                  setEditing(false);
                  setRestored(false);
                  setGone(next);
                },
              )
            }
          >
            {changeTexts.remove}
          </Button>
        </div>
      </div>
      <DoneStamp assignment={a} />
      {changeProblem && (
        <ResultLine kind="fail" role="alert">
          {changeProblem}
        </ResultLine>
      )}
      {doneRun.issue !== null && (
        <DoneIssueLine
          issue={doneRun.issue}
          where="page"
          onRetry={doneRun.issue.retry}
          onOpen={() => reload(true, true).catch(() => undefined)}
          onBack={props.onBack}
        />
      )}
      {editing && (
        <EditAssignmentDialog
          api={api}
          library={library}
          assignment={a}
          onClose={() => setEditing(false)}
          onSaved={(next) => {
            setDetail((d) => (d === null ? d : { ...d, assignment: next }));
            setEditing(false);
          }}
          onStale={() => reload(true).catch(() => undefined)}
        />
      )}
      <div className="hw-lesson-head">
        {lesson !== "" && <span className="hw-lesson">{lesson}</span>}
        {!correcting && (
          <Button variant="ghost" onClick={() => setCorrecting(true)}>
            Поправить предмет и преподавателя
          </Button>
        )}
      </div>
      {correcting && (
        <LessonCorrection
          assignment={a}
          correct={api.correctLesson}
          onCorrected={(next) => {
            setDetail((d) => (d === null ? d : { ...d, assignment: next }));
            setCorrecting(false);
          }}
          onClose={() => setCorrecting(false)}
        />
      )}
      {a.description !== "" && <p className="hw-desc">{a.description}</p>}
      <TranscriptLinks lessonId={a.lessonId} />

      <Card className="box">
        <h2>Быстрый черновик</h2>
        {shown === null ? (
          <p className="small muted box-desc">Черновика ещё нет: его подготовит сервис конспектов по тексту задания.</p>
        ) : (
          <>
            <span className="small muted box-desc num">
              {versionTitle(shown)} · {formatTime(shown.createdAt)}
              {isLatest ? " · последняя" : ""}
            </span>
            {!isLatest && (
              <Banner kind="wait">
                Это старая версия, она только для просмотра. Её можно сохранить в файл или отправить, а чтобы продолжить с
                её текстом — «Взять этот текст».
              </Banner>
            )}
            <Field label="Заголовок">
              {(fid) => <Input id={fid} value={title} readOnly={readOnly} onChange={(e) => setTitle(e.target.value)} />}
            </Field>
            <Field label="Текст">
              {(fid) => (
                <textarea
                  id={fid}
                  className="input hw-text"
                  rows={14}
                  value={body}
                  readOnly={readOnly}
                  onChange={(e) => setBody(e.target.value)}
                />
              )}
            </Field>
            <div className="acts start">
              {isLatest && dirty && (
                <>
                  <Button variant="primary" busy={busy === "save"} busyLabel="Сохранение…" disabled={!idle} onClick={save}>
                    Сохранить правку
                  </Button>
                  <Button variant="ghost" disabled={!idle} onClick={revert}>
                    Отменить правку
                  </Button>
                </>
              )}
              {!isLatest && (
                <>
                  <Button disabled={!idle || generating} onClick={takeText}>
                    Взять этот текст
                  </Button>
                  <Button variant="ghost" disabled={!idle} onClick={() => latest && void showVersion(latest.number)}>
                    К последней версии
                  </Button>
                </>
              )}
              <Button
                icon="down"
                busy={busy === "export"}
                busyLabel="Сохранение…"
                disabled={!idle || dirty}
                disabledReason={savedReason}
                onClick={exportFile}
              >
                Сохранить в файл
              </Button>
              <Button icon="down" busy={busy === "docx"} busyLabel="Сохранение…" disabled={!idle || dirty} onClick={saveWord}>
                Сохранить в Word (.docx)
              </Button>
              <Button
                icon="mail"
                busy={busy === "send"}
                busyLabel="Подготовка…"
                disabled={!idle || dirty}
                onClick={startSend}
              >
                Отправить по почте…
              </Button>
            </div>
            {isLatest && !dirty && <p className="small muted hw-note">Правка сохраняется новой версией, старые остаются.</p>}
          </>
        )}
        {message?.at === "draft" && <MessageLine message={message} />}
        {refusal !== null && (
          <div className="rcp-refusal">
            <ResultLine kind="fail" role="alert">
              {refusal.text}
            </ResultLine>
            <div className="acts start">
              <RefusalButton
                action={refusalAction(refusal, "page")}
                nav={nav}
                onChooseSubject={() => {
                  setRefusal(null);
                  setEditing(true);
                }}
              />
            </div>
          </div>
        )}
      </Card>

      <Card className="box">
        <h2>{latest === null ? "Новый черновик" : "Доработать черновик"}</h2>
        <Field
          label="Дополнительные указания"
          hint="Необязательно. Например: «короче», «добавь пример решения», «по-английски»."
        >
          {(fid) => (
            <textarea
              id={fid}
              className="input hw-instr"
              rows={3}
              value={instruction}
              disabled={offer.instructionDisabled}
              onChange={(e) => setInstruction(e.target.value)}
            />
          )}
        </Field>
        <div className="acts start">
          <Button
            variant={latest === null ? "primary" : "secondary"}
            busy={generating || busy === "generate"}
            busyLabel={generatingLabel}
            disabled={!idle || offer.disabled}
            disabledReason={offer.reason}
            onClick={generate}
          >
            {offer.label}
          </Button>
        </div>
        {genView && (
          <div className="hw-gen" role="status">
            <Chip view={genView} />
            {genView.hint !== "" && <span className="small muted">{genView.hint}</span>}
          </div>
        )}
      </Card>

      <HomeworkTurnkey api={turnkey} assignmentId={id} />

      {versions.length > 0 && (
        <>
          <h2 className="group-title">Версии</h2>
          {dirty && <p className="small muted">Чтобы открыть другую версию, сохраните или отмените правку.</p>}
          <div className="card rows" role="list">
            {versions.map((v) => (
              <Row
                key={v.number}
                title={
                  <>
                    {versionTitle(v)}
                    {v.latest ? " · последняя" : ""}
                  </>
                }
                meta={
                  <>
                    <span className="num">{formatTime(v.createdAt)}</span>
                    {v.instruction !== "" && <span className="hw-quote">Указания: {v.instruction}</span>}
                  </>
                }
                actions={
                  shown !== null && shown.number === v.number ? (
                    <span className="small muted">Открыта</span>
                  ) : (
                    <Button disabled={!idle || dirty || generating} onClick={() => void showVersion(v.number)}>
                      Открыть
                    </Button>
                  )
                }
              />
            ))}
          </div>
        </>
      )}

      <h2 className="group-title">Письма</h2>
      {deliveriesError && (
        <Banner kind="fail" role="alert">
          {deliveriesError}
        </Banner>
      )}
      {message?.at === "mail" && <MessageLine message={message} />}
      {deliveries === null ? (
        !deliveriesError && <p className="muted">Загрузка…</p>
      ) : deliveries.length === 0 ? (
        <p className="muted">Писем по этому заданию ещё не было.</p>
      ) : (
        <div className="card rows" role="list">
          {deliveries.map((d) => {
            const v = deliveryView(d);
            return (
              <Row
                key={d.id}
                title={<bdi>{d.recipient}</bdi>}
                meta={<span className="num">Создано {formatTime(d.createdAt)}</span>}
                status={
                  <>
                    <Chip view={v} />
                    {v.hint !== "" && <span className="small muted">{v.hint}</span>}
                    {v.problem !== "" && <span className="small row-problem">{v.problem}</span>}
                  </>
                }
                actions={
                  <>
                    {d.canMarkSent && (
                      <Button
                        busy={busy === `mark:${d.id}`}
                        busyLabel="Сохранение…"
                        disabled={!idle}
                        onClick={() => void markSent(d)}
                      >
                        Письмо дошло
                      </Button>
                    )}
                    {d.canSendAgain && (
                      <Button variant="danger" disabled={!idle} onClick={() => setSendAgain(d)}>
                        Отправить ещё раз…
                      </Button>
                    )}
                  </>
                }
              />
            );
          })}
        </div>
      )}

      {sendOpen && (
        <SendDialog
          api={api}
          prep={sendOpen.prep}
          version={sendOpen.version}
          switching={sendOpen.switching}
          problem={sendOpen.problem}
          onFormat={(f) => void switchFormat(f)}
          onClose={sendClosed}
        />
      )}
      {sendAgain && <SendAgainDialog api={api} delivery={sendAgain} onClose={sendAgainClosed} />}
      <Toast {...doneToastProps(doneRun, focus)} />
    </section>
  );
}
