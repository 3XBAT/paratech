import { type KeyboardEvent, type ReactNode, useEffect, useId, useMemo, useReducer, useRef } from "react";
import {
  createFromSchedule,
  type ScheduleGroup,
  type ScheduleMember,
  scheduleSubjects,
  type ScheduleSubjectsReply,
  undoFromSchedule,
} from "../bindings/library.js";
import type { CallByName } from "../bindings/settings.js";
import {
  type Action,
  type Batch,
  closedState,
  createFlow,
  createGate,
  doneText,
  escCloses,
  groupItem,
  initialState,
  lineSteps,
  mainButton,
  type MemberMode,
  memberItem,
  metaText,
  type NameProblem,
  nameProblemTexts,
  type Notice,
  offerGroups,
  type OfferState,
  offerStatus,
  omittedNote,
  planOf,
  reduce,
  refreshFlow,
  type ReviewApi,
  type ReviewState,
  reviewTexts,
  type RowSel,
  spellingsText,
  type Step,
  type SubjectRef,
  subjectNamesOf,
  takenOf,
  undoFlow,
} from "../scheduleSubjectsView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

// The review of subjects proposed from the schedule (TASK-312; the agent card of TASK-324 reuses it). The names,
// spellings and subject names are schedule text: text nodes and input values only, never markup, never logged. Fixed
// aria-label templates may hold a name as plain string interpolation.

/** The calls of the review over the Wails bindings; the strict parsers reject an unreadable reply. */
export function scheduleReviewApi(call: CallByName): ReviewApi {
  return {
    create: (token, choices) => createFromSchedule(call, token, choices),
    undo: (token) => undoFromSchedule(call, token),
    refresh: () => scheduleSubjects(call),
  };
}

export interface ReviewHandlers {
  check(group: string, checked: boolean): void;
  rename(group: string, name: string): void;
  member(group: string, member: string, mode: MemberMode): void;
  renameMember(group: string, member: string, name: string): void;
  create(): void;
  undo(): void;
  step(step: Step): void;
  close(): void;
}

/**
 * The controller: the state machine of scheduleSubjectsView in a reducer, the calls, and a guard against a second call
 * before the re-render that disables the buttons. `reply` is the proposal the review starts from; after that the
 * review holds its own (its «Обновить список» asks again), so a reload of the page does not drop an outcome on screen.
 * `onChanged` tells the page that subjects were created or removed; `onClose` hands back what is left to undo, so the
 * page can keep «Отменить всё» after the panel is gone and give it back when the panel opens again.
 */
export function ScheduleSubjectsReview(props: {
  reply: ScheduleSubjectsReply;
  /** The existing subjects: their names and other names are taken, and the outcomes call them by name. */
  subjects: readonly SubjectRef[];
  /** What earlier panels created and did not undo (the undo goes newest first, so they come first). */
  initialBatches?: readonly Batch[];
  api: ReviewApi;
  onChanged(): void;
  onClose(batches: readonly Batch[]): void;
  /** Moves the focus into the panel on arrival (the page does; a card in the chat must not). */
  autoFocus?: boolean;
}) {
  const [state, dispatch] = useReducer(reduce, props, (p) => initialState(p.reply, p.initialBatches));
  const current = useRef(state);
  current.current = state;
  const running = useRef(false);
  const latest = useRef(props);
  latest.current = props;

  async function run(start: Action, flow: () => Promise<Action>) {
    if (running.current) {
      return;
    }
    running.current = true;
    dispatch(start);
    try {
      const done = await flow();
      dispatch(done);
      const changed =
        (done.type === "created" && done.reply.created.length + done.reply.aliased.length > 0) ||
        done.type === "createThrown" ||
        done.type === "undone";
      if (changed) {
        latest.current.onChanged();
      }
    } finally {
      running.current = false;
    }
  }

  const create = () => {
    const s = current.current;
    if (createGate(s, latest.current.subjects).ok) {
      void run({ type: "saving" }, () => createFlow(latest.current.api, s));
    }
  };

  const undo = () => {
    const s = current.current;
    if (s.phase === "idle" && s.batches.length > 0) {
      void run({ type: "undoing" }, () => undoFlow(latest.current.api, s.batches, subjectNamesOf(latest.current.subjects)));
    }
  };

  const on: ReviewHandlers = {
    check: (group, checked) => dispatch({ type: "check", group, checked }),
    rename: (group, name) => dispatch({ type: "rename", group, name }),
    member: (group, member, mode) => dispatch({ type: "member", group, member, mode }),
    renameMember: (group, member, name) => dispatch({ type: "renameMember", group, member, name }),
    create,
    undo,
    step: (step) => {
      switch (step) {
        case "retryCreate":
          create();
          return;
        case "retryUndo":
          undo();
          return;
        case "refresh":
        case "rest":
          if (current.current.phase === "idle") {
            void run({ type: "refreshing" }, () => refreshFlow(latest.current.api));
          }
          return;
        case "close":
          on.close();
          return;
      }
    },
    close: () => {
      if (current.current.phase === "idle") {
        latest.current.onClose(current.current.batches);
      }
    },
  };

  return <ReviewView state={state} subjects={props.subjects} on={on} autoFocus={props.autoFocus} />;
}

const stepLabels: Readonly<Record<Step, string>> = {
  retryCreate: reviewTexts.retry,
  retryUndo: reviewTexts.retry,
  refresh: reviewTexts.refresh,
  rest: reviewTexts.showRest,
  close: reviewTexts.close,
};

/** The buttons of an outcome; they are off while a call runs. */
function StepButtons(props: { steps: readonly Step[]; phase: ReviewState["phase"]; onStep(step: Step): void }) {
  const busy = props.phase !== "idle";
  return (
    <>
      {props.steps.map((s) => (
        <Button
          key={s}
          size="sm"
          variant="ghost"
          disabled={busy}
          busy={(s === "refresh" || s === "rest") && props.phase === "refreshing"}
          busyLabel={s === "refresh" || s === "rest" ? reviewTexts.refreshing : undefined}
          onClick={() => props.onStep(s)}
        >
          {stepLabels[s]}
        </Button>
      ))}
    </>
  );
}

/** «Создано предметов: 3 · [Отменить всё]»; the same line in the panel and on the page after the panel is closed. */
function DoneRow(props: { text: string; phase: ReviewState["phase"]; onUndo(): void }) {
  return (
    <div className="sched-done">
      <ResultLine kind="ok">{props.text}</ResultLine>
      <Button size="sm" variant="ghost" disabled={props.phase !== "idle"} busy={props.phase === "undoing"} busyLabel={reviewTexts.undoing} onClick={props.onUndo}>
        {reviewTexts.undoAll}
      </Button>
    </div>
  );
}

/** The panel for any state: the controller above and the tests render it. */
export function ReviewView(props: {
  state: ReviewState;
  subjects: readonly SubjectRef[];
  on: ReviewHandlers;
  autoFocus?: boolean;
}) {
  const { state, on } = props;
  const panel = useRef<HTMLElement>(null);
  const taken = useMemo(
    () => takenOf(props.subjects, { token: state.token, batches: state.batches, attempt: state.attempt }),
    [props.subjects, state.token, state.batches, state.attempt],
  );
  const plan = useMemo(() => planOf(state.groups, state.selection, taken), [state.groups, state.selection, taken]);
  const main = mainButton(plan);
  const saving = state.phase === "saving";
  const busy = state.phase !== "idle";
  const done = doneText(state.batches);
  const notice = state.notice;

  useEffect(() => {
    if (props.autoFocus) {
      panel.current?.focus();
    }
  }, [props.autoFocus]);

  // Esc closes the panel when nothing is being saved; the choices are cheap to redo, so there is no confirmation. Inside
  // a text field it does nothing, so a stray Esc cannot throw the edits away.
  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (e.key === "Escape" && escCloses(e.target as HTMLInputElement, busy)) {
      e.stopPropagation();
      on.close();
    }
  };

  const steps = (n: Notice) => <StepButtons steps={n.steps} phase={state.phase} onStep={on.step} />;

  return (
    <section
      ref={panel}
      className="sched-review"
      aria-label={reviewTexts.label}
      aria-busy={busy || undefined}
      tabIndex={-1}
      onKeyDown={onKeyDown}
    >
      <div className="sched-status" role="status" aria-live="polite">
        {done !== "" && <DoneRow text={done} phase={state.phase} onUndo={on.undo} />}
        {notice !== null && notice.tone === "ok" && <NoticeBody notice={notice} steps={steps(notice)} />}
      </div>

      {state.view === "list" && (
        <>
          <p className="small muted sched-intro">{reviewTexts.intro}</p>
          <ul className="sched-rows">
            {state.groups.map((g) => {
              const row = state.selection.get(g.key);
              return row === undefined ? null : (
                <GroupRow
                  key={g.key}
                  group={g}
                  row={row}
                  problems={plan.problems}
                  disabled={busy}
                  on={on}
                />
              );
            })}
          </ul>
          {state.omitted > 0 && <p className="small muted">{omittedNote(state.omitted)}</p>}
        </>
      )}

      <div className="sched-alert" role="alert">
        {notice !== null && notice.tone === "fail" && <NoticeBody notice={notice} steps={steps(notice)} />}
      </div>

      <div className="sched-foot acts start">
        {state.view === "list" && (
          <>
            <Button
              variant="primary"
              disabled={busy || main.disabled}
              disabledReason={busy ? undefined : main.reason || undefined}
              busy={saving}
              busyLabel={reviewTexts.creating}
              onClick={on.create}
            >
              {main.label}
            </Button>
            {!busy && main.extra !== "" && <span className="small muted">{main.extra}</span>}
          </>
        )}
        <Button disabled={busy} onClick={on.close}>
          {reviewTexts.close}
        </Button>
      </div>
    </section>
  );
}

function NoticeBody(props: { notice: Notice; steps: ReactNode }) {
  const n = props.notice;
  const hasSteps = n.steps.length > 0;
  return (
    <div className="sched-notice">
      {n.text !== "" && <ResultLine kind={n.tone === "ok" ? "ok" : "fail"}>{n.text}</ResultLine>}
      {n.lines.length > 0 && (
        <ul className="sched-lines small muted">
          {n.lines.map((l, i) => (
            <li key={i}>{l}</li>
          ))}
        </ul>
      )}
      {hasSteps && <div className="acts start">{props.steps}</div>}
    </div>
  );
}

function FieldError(props: { id: string; problem: NameProblem | undefined }) {
  if (props.problem === undefined) {
    return null;
  }
  return (
    <span id={props.id} className="field-error small">
      <Icon name="warn" />
      {nameProblemTexts[props.problem]}
    </span>
  );
}

/** An editable name in a row, with its error beside it. No length cap in the field: the plan counts the characters. */
function NameInput(props: {
  value: string;
  label: string;
  problem: NameProblem | undefined;
  disabled: boolean;
  onChange(name: string): void;
}) {
  const errId = useId();
  return (
    <span className="sched-name">
      <input
        className="input"
        value={props.value}
        disabled={props.disabled}
        aria-label={props.label}
        aria-invalid={props.problem !== undefined || undefined}
        aria-describedby={props.problem !== undefined ? errId : undefined}
        onChange={(e) => props.onChange(e.target.value)}
      />
      <FieldError id={errId} problem={props.problem} />
    </span>
  );
}

function GroupRow(props: {
  group: ScheduleGroup;
  row: RowSel;
  problems: ReadonlyMap<string, NameProblem>;
  disabled: boolean;
  on: ReviewHandlers;
}) {
  const { group: g, row, on } = props;
  const chkId = useId();
  const metaId = useId();
  const aliasOf = g.action === "alias_of";
  const spellings = spellingsText(g.aliases);
  const doubtful = g.members.filter((m) => m.doubtful);
  return (
    <li className={row.checked ? "sched-row" : "sched-row off"}>
      <label className="sched-chk-hit">
        <input
          id={chkId}
          type="checkbox"
          className="sched-chk"
          checked={row.checked}
          disabled={props.disabled}
          aria-label={aliasOf ? reviewTexts.checkboxAddAria(g.subjectName) : reviewTexts.checkboxAria(g.name)}
          aria-describedby={metaId}
          onChange={(e) => on.check(g.key, e.target.checked)}
        />
      </label>
      <div className="sched-body">
        {aliasOf ? (
          <label htmlFor={chkId} className="sched-to">{`${reviewTexts.addTo} «${g.subjectName}»`}</label>
        ) : (
          <NameInput
            value={row.name}
            label={`${reviewTexts.nameLabel} «${g.name}»`}
            problem={row.checked ? props.problems.get(groupItem(g.key)) : undefined}
            disabled={props.disabled || !row.checked}
            onChange={(name) => on.rename(g.key, name)}
          />
        )}
        <label htmlFor={chkId} id={metaId} className="small muted">
          {metaText(g)}
        </label>
        {spellings !== "" && (
          <label htmlFor={chkId} className="small muted sched-spell">
            {spellings}
          </label>
        )}
        {g.doubtful && (
          <p className="small sched-mark">
            <Icon name="info" />
            {reviewTexts.doubtful}
          </p>
        )}
        {doubtful.length > 0 && (
          <ul className="sched-members">
            {doubtful.map((m) => (
              <MemberRow key={m.key} group={g} member={m} row={row} problems={props.problems} disabled={props.disabled || !row.checked} on={on} />
            ))}
          </ul>
        )}
      </div>
    </li>
  );
}

const memberModes: readonly (readonly [MemberMode, string])[] = [
  ["keep", reviewTexts.together],
  ["separate", reviewTexts.separate],
  ["skip", reviewTexts.skip],
];

function MemberRow(props: {
  group: ScheduleGroup;
  member: ScheduleMember;
  row: RowSel;
  problems: ReadonlyMap<string, NameProblem>;
  disabled: boolean;
  on: ReviewHandlers;
}) {
  const { group: g, member: m, row, on } = props;
  const sel = row.members.get(m.key) ?? { mode: "keep" as const, name: m.name };
  const separate = sel.mode === "separate";
  return (
    <li className="sched-member">
      <div className="sched-m-main">
        {separate ? (
          <NameInput
            value={sel.name}
            label={`${reviewTexts.separateLabel} «${m.name}»`}
            problem={row.checked ? props.problems.get(memberItem(g.key, m.key)) : undefined}
            disabled={props.disabled}
            onChange={(name) => on.renameMember(g.key, m.key, name)}
          />
        ) : (
          <span className={sel.mode === "skip" ? "sched-m-name off" : "sched-m-name"}>{m.name}</span>
        )}
        <span className="small muted">
          {metaText(m)}
          {sel.mode === "skip" ? `${metaText(m) === "" ? "" : " · "}${reviewTexts.skipped}` : ""}
        </span>
      </div>
      <div className="sched-seg" role="radiogroup" aria-label={`${reviewTexts.memberChoice} «${m.name}»`}>
        {memberModes.map(([mode, label]) => (
          <Button
            key={mode}
            size="sm"
            role="radio"
            aria-checked={sel.mode === mode}
            disabled={props.disabled}
            onClick={() => on.member(g.key, m.key, mode)}
          >
            {label}
          </Button>
        ))}
      </div>
    </li>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// After the panel closed: «Отменить всё» stays on the page until the student leaves or undoes.

/**
 * The batches of the closed panel and the undo flow over them, for the page. The same reducer and flow as the panel's, so
 * the outcomes (kept lines, problems with their buttons) read the same. `adopt` hands the batches over when the panel
 * closes, and takes them back (with `[]`) when it opens again.
 */
export function useKeptUndo(subjects: readonly SubjectRef[], api: ReviewApi, onChanged: () => void) {
  const [state, dispatch] = useReducer(reduce, [] as readonly Batch[], closedState);
  const current = useRef(state);
  current.current = state;
  const running = useRef(false);
  const latest = useRef({ subjects, api, onChanged });
  latest.current = { subjects, api, onChanged };

  const undo = () => {
    const s = current.current;
    if (running.current || s.phase !== "idle" || s.batches.length === 0) {
      return;
    }
    running.current = true;
    dispatch({ type: "undoing" });
    void undoFlow(latest.current.api, s.batches, subjectNamesOf(latest.current.subjects))
      .then((done) => {
        dispatch(done);
        latest.current.onChanged();
      })
      .finally(() => {
        running.current = false;
      });
  };

  return {
    state,
    undo,
    adopt: (batches: readonly Batch[]) => dispatch({ type: "adopt", batches }),
    dismiss: () => dispatch({ type: "dismiss" }),
  };
}

/** «Создано предметов: N · [Отменить всё]» and the outcome of the undo; nothing when there is nothing to show. */
export function KeptUndoLine(props: { state: ReviewState; onUndo(): void; onDismiss(): void }) {
  const { state } = props;
  const done = doneText(state.batches);
  const notice = state.notice;
  if (done === "" && notice === null) {
    return null;
  }
  const onStep = (s: Step) => {
    if (s === "retryUndo") {
      props.onUndo();
    } else {
      props.onDismiss();
    }
  };
  const shown = notice === null ? null : { ...notice, steps: lineSteps(notice) };
  return (
    <div className="sched-kept">
      <div role="status" aria-live="polite">
        {done !== "" && <DoneRow text={done} phase={state.phase} onUndo={props.onUndo} />}
        {shown !== null && shown.tone === "ok" && <NoticeBody notice={shown} steps={<StepButtons steps={shown.steps} phase={state.phase} onStep={onStep} />} />}
      </div>
      <div role="alert">
        {shown !== null && shown.tone === "fail" && <NoticeBody notice={shown} steps={<StepButtons steps={shown.steps} phase={state.phase} onStep={onStep} />} />}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// The page's entry points: the button, the line above the list, and the actions of the empty list.

/** What the list of subjects needs to offer the review; undefined: no offer (the list as before). */
export interface ScheduleOffer {
  state: OfferState;
  onReview(): void;
  /** «Импортировать расписание»: opens the page of the schedule. */
  onImport(): void;
  /** «Повторить» after a check that failed. */
  onRetry(): void;
}

/** The id the focus returns to when the panel closes. */
export const offerButtonId = "subjects-schedule-offer";

/** «Завести предметы из расписания», with the number of groups when `groups` is given. */
export function ScheduleOfferButton(props: { groups?: number; variant: "primary" | "secondary"; onClick(): void }) {
  return (
    <Button id={offerButtonId} variant={props.variant} icon="cal" onClick={props.onClick}>
      {reviewTexts.open}
      {props.groups !== undefined && <span className="sched-cnt num">{props.groups}</span>}
    </Button>
  );
}

/** One quiet line above the list of subjects: what the schedule can still make, and the button. */
export function ScheduleOfferLine(props: { groups: number; onClick(): void }) {
  return (
    <p className="sched-offer small muted">
      <span>{reviewTexts.offerLine(props.groups)}</span>
      <ScheduleOfferButton variant="secondary" onClick={props.onClick} />
    </p>
  );
}

/**
 * The actions of the empty «Предметов пока нет»: with groups the schedule button is the primary one; with a schedule that
 * has nothing to create, or none, «Импортировать расписание» is; when the check failed «Добавить предмет» is the only
 * primary and «Повторить» asks again. Never two primaries.
 */
export function EmptySubjectsActions(props: { offer?: ScheduleOffer; addLabel: string; onAdd(): void }) {
  const offer = props.offer;
  const add = (primary: boolean) => (
    <Button variant={primary ? "primary" : "secondary"} icon="plus" onClick={props.onAdd}>
      {props.addLabel}
    </Button>
  );
  if (offer === undefined) {
    return add(true);
  }
  switch (offerStatus(offer.state)) {
    case "checking":
      return (
        <>
          {add(false)}
          <span className="small muted" role="status">
            {reviewTexts.checking}
          </span>
        </>
      );
    case "failed":
      return (
        <>
          {add(true)}
          <span className="small muted" role="status">
            {reviewTexts.checkFailed}
          </span>
          <Button size="sm" variant="ghost" onClick={offer.onRetry}>
            {reviewTexts.retry}
          </Button>
        </>
      );
    case "groups":
      return (
        <>
          <ScheduleOfferButton groups={offerGroups(offer.state) ?? 0} variant="primary" onClick={offer.onReview} />
          {add(false)}
        </>
      );
    case "none":
      return (
        <>
          <Button variant="primary" icon="up" onClick={offer.onImport}>
            {reviewTexts.importSchedule}
          </Button>
          {add(false)}
        </>
      );
  }
}
