import { type KeyboardEvent, type ReactNode, useEffect, useId, useMemo, useReducer, useRef } from "react";
import {
  createFromSchedule,
  type ScheduleGroup,
  type ScheduleMember,
  type ScheduleSubjectsReply,
  scheduleSubjects,
  undoFromSchedule,
} from "../bindings/library.js";
import type { CallByName } from "../bindings/settings.js";
import {
  type Action,
  createFlow,
  doneText,
  groupItem,
  initialState,
  mainButton,
  memberItem,
  metaText,
  type NameProblem,
  nameProblemTexts,
  type Notice,
  omittedNote,
  type OfferState,
  offerGroups,
  planOf,
  reduce,
  refreshFlow,
  type ReviewApi,
  type ReviewState,
  type RowSel,
  reviewTexts,
  spellingsText,
  type Step,
  subjectNameKeys,
  undoFlow,
} from "../scheduleSubjectsView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

// The review of subjects proposed from the schedule (TASK-312; the agent card of TASK-324 reuses it). The names,
// spellings and subject names are schedule text: text nodes and input values only, never markup, never logged.

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
  member(group: string, member: string, mode: "separate" | "skip"): void;
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
 * `onChanged` tells the page that subjects were created or removed.
 */
export function ScheduleSubjectsReview(props: {
  reply: ScheduleSubjectsReply;
  /** Names of the existing subjects by id. */
  subjectNames: ReadonlyMap<string, string>;
  api: ReviewApi;
  onChanged(): void;
  onClose(): void;
  /** Moves the focus into the panel on arrival (the page does; a card in the chat must not). */
  autoFocus?: boolean;
}) {
  const [state, dispatch] = useReducer(reduce, props.reply, initialState);
  const current = useRef(state);
  current.current = state;
  const running = useRef(false);
  const latest = useRef(props);
  latest.current = props;
  const taken = useMemo(() => subjectNameKeys(props.subjectNames), [props.subjectNames]);

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
    if (s.phase !== "idle" || s.view !== "list") {
      return;
    }
    const plan = planOf(s.groups, s.selection, taken);
    if (plan.problems.size > 0 || plan.creates + plan.additions === 0) {
      return;
    }
    void run({ type: "saving" }, () => createFlow(latest.current.api, s));
  };

  const undo = () => {
    const s = current.current;
    if (s.phase === "idle" && s.batches.length > 0) {
      void run({ type: "undoing" }, () => undoFlow(latest.current.api, s.batches, latest.current.subjectNames));
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
          if (current.current.phase === "idle") {
            void run({ type: "refreshing" }, () => refreshFlow(latest.current.api));
          }
          return;
        case "toSubjects":
          latest.current.onClose();
          return;
      }
    },
    close: () => {
      if (current.current.phase === "idle") {
        latest.current.onClose();
      }
    },
  };

  return <ReviewView state={state} names={props.subjectNames} on={on} autoFocus={props.autoFocus} />;
}

const stepLabels: Readonly<Record<Step, string>> = {
  retryCreate: reviewTexts.retry,
  retryUndo: reviewTexts.retry,
  refresh: reviewTexts.refresh,
  toSubjects: reviewTexts.toSubjects,
};

/** The panel for any state: the controller above and the tests render it. */
export function ReviewView(props: {
  state: ReviewState;
  names: ReadonlyMap<string, string>;
  on: ReviewHandlers;
  autoFocus?: boolean;
}) {
  const { state, on } = props;
  const panel = useRef<HTMLElement>(null);
  const taken = useMemo(() => subjectNameKeys(props.names), [props.names]);
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

  // Esc closes the panel when nothing is being saved; the choices are cheap to redo, so there is no confirmation.
  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (e.key === "Escape" && !busy) {
      e.stopPropagation();
      on.close();
    }
  };

  const stepButtons = (steps: readonly Step[]): ReactNode =>
    steps.map((s) => (
      <Button
        key={s}
        size="sm"
        variant="ghost"
        disabled={busy}
        busy={s === "refresh" && state.phase === "refreshing"}
        busyLabel={s === "refresh" ? reviewTexts.refreshing : undefined}
        onClick={() => on.step(s)}
      >
        {stepLabels[s]}
      </Button>
    ));

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
        {done !== "" && (
          <div className="sched-done">
            <ResultLine kind="ok">{done}</ResultLine>
            <Button size="sm" variant="ghost" disabled={busy} busy={state.phase === "undoing"} busyLabel={reviewTexts.undoing} onClick={on.undo}>
              {reviewTexts.undoAll}
            </Button>
          </div>
        )}
        {notice !== null && notice.tone === "ok" && <NoticeBody notice={notice} steps={stepButtons(notice.steps)} />}
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
          {state.omitted > 0 && (
            <p className="small muted">
              {omittedNote(state.omitted)} — после создания нажмите „{reviewTexts.refresh}“
            </p>
          )}
        </>
      )}

      <div className="sched-alert" role="alert">
        {notice !== null && notice.tone === "fail" && <NoticeBody notice={notice} steps={stepButtons(notice.steps)} />}
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

/** An editable name in a row, with its error beside it. */
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
        maxLength={200}
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
  const metaId = useId();
  const aliasOf = g.action === "alias_of";
  const spellings = spellingsText(g.aliases);
  const doubtful = g.members.filter((m) => m.doubtful);
  return (
    <li className={row.checked ? "sched-row" : "sched-row off"}>
      <input
        type="checkbox"
        className="sched-chk"
        checked={row.checked}
        disabled={props.disabled}
        aria-label={aliasOf ? reviewTexts.checkboxAdd : reviewTexts.checkbox}
        aria-describedby={metaId}
        onChange={(e) => on.check(g.key, e.target.checked)}
      />
      <div className="sched-body">
        {aliasOf ? (
          <p className="sched-to">Добавить к „{g.subjectName}“</p>
        ) : (
          <NameInput
            value={row.name}
            label={reviewTexts.nameLabel}
            problem={row.checked ? props.problems.get(groupItem(g.key)) : undefined}
            disabled={props.disabled || !row.checked}
            onChange={(name) => on.rename(g.key, name)}
          />
        )}
        <p id={metaId} className="small muted">
          {metaText(g)}
        </p>
        {spellings !== "" && <p className="small muted sched-spell">{spellings}</p>}
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
            label={reviewTexts.separateLabel}
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
      <div className="sched-m-acts">
        <Button size="sm" disabled={props.disabled} aria-pressed={separate} onClick={() => on.member(g.key, m.key, "separate")}>
          {reviewTexts.separate}
        </Button>
        <Button size="sm" disabled={props.disabled} aria-pressed={sel.mode === "skip"} onClick={() => on.member(g.key, m.key, "skip")}>
          {reviewTexts.skip}
        </Button>
      </div>
    </li>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// The page's entry points: the button, and the actions of the empty list.

/** What the list of subjects needs to offer the review; undefined: no offer (the list as before). */
export interface ScheduleOffer {
  state: OfferState;
  onReview(): void;
  /** «Импортировать расписание»: opens the page of the schedule. */
  onImport(): void;
}

/** The id the focus returns to when the panel closes. */
export const offerButtonId = "subjects-schedule-offer";

/** «Завести предметы из расписания» with the number of groups; nothing when the schedule has none. */
export function ScheduleOfferButton(props: { groups: number; variant: "primary" | "secondary"; disabled?: boolean; onClick(): void }) {
  return (
    <Button id={offerButtonId} variant={props.variant} icon="cal" disabled={props.disabled} onClick={props.onClick}>
      {reviewTexts.open}
      <span className="sched-cnt num">{props.groups}</span>
    </Button>
  );
}

/**
 * The actions of the empty «Предметов пока нет»: with groups the schedule button is the primary one, without a schedule
 * «Импортировать расписание» is; «Добавить предмет» is always the secondary one. Never two primaries.
 */
export function EmptySubjectsActions(props: { offer?: ScheduleOffer; addLabel: string; onAdd(): void }) {
  const offer = props.offer;
  const groups = offer === undefined ? 0 : offerGroups(offer.state);
  const add = (primary: boolean) => (
    <Button variant={primary ? "primary" : "secondary"} icon="plus" onClick={props.onAdd}>
      {props.addLabel}
    </Button>
  );
  if (offer === undefined) {
    return add(true);
  }
  if (groups === null) {
    return (
      <>
        {add(false)}
        <span className="small muted" role="status">
          {reviewTexts.checking}
        </span>
      </>
    );
  }
  if (groups > 0) {
    return (
      <>
        <ScheduleOfferButton groups={groups} variant="primary" onClick={offer.onReview} />
        {add(false)}
      </>
    );
  }
  return (
    <>
      <Button variant="primary" icon="up" onClick={offer.onImport}>
        {reviewTexts.importSchedule}
      </Button>
      {add(false)}
    </>
  );
}
