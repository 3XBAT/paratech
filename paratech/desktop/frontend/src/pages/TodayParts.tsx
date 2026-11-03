// The sections of «Сегодня». They hold no data or decisions: the page passes what the core reports. Every title,
// subject, teacher, headline and recipient is text from a file, a schedule or a model and is rendered as a text node.
import type { ReactNode } from "react";
import type { RecordingItem } from "../bindings/activity.js";
import type { Problem } from "../todayView.js";
import type { TodayChip, TodayDeadline, TodayDone, TodayLesson, TodayPost, TodayWaiting } from "../bindings/today.js";
import {
  deadlineLine,
  doneLine,
  lessonLine,
  looseProblems,
  postLine,
  recordButton,
  recordTexts,
  statusParts,
  summaryRows,
  texts,
  waitingLine,
} from "../todayView.js";
import { type ActiveIssue, doneTexts } from "../assignmentDoneView.js";
import { Button } from "../ui/Button.js";
import { ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { IconAction } from "../ui/IconAction.js";
import { DoneCheck, DoneIssueLine } from "./AssignmentDoneParts.js";
import type { AgentConfirmApi } from "../bindings/agentConfirm.js";
import type { RecordingDeleteApi } from "../bindings/recordingDelete.js";
import { isDeadlineAction } from "../agentConfirmView.js";
import { addTexts } from "../homeworkView.js";
import { PostCalendar } from "./TodayCalendar.js";
import { StatusChip } from "../ui/StatusChip.js";

function Section(props: { title: string; count?: number; className?: string; action?: ReactNode; children: ReactNode }) {
  const heading = (
    <h2 className="today-h">
      {props.title}
      {props.count !== undefined && props.count > 0 && <span className="num today-cnt"> · {props.count}</span>}
    </h2>
  );
  return (
    <section className={props.className ? `today-sec ${props.className}` : "today-sec"}>
      {props.action === undefined ? (
        heading
      ) : (
        <div className="today-hrow">
          {heading}
          {props.action}
        </div>
      )}
      {props.children}
    </section>
  );
}

function Empty(props: { children: string }) {
  return <p className="muted today-empty">{props.children}</p>;
}

/** Not shown while there are no chips: an empty block would only be noise. */
export function AskSection(props: { chips: readonly TodayChip[]; onChip: (text: string) => void }) {
  if (props.chips.length === 0) {
    return null;
  }
  return (
    <Section title={texts.askTitle}>
      <p className="small muted today-ask-hint">{texts.askHint}</p>
      <div className="today-chips">
        {props.chips.map((c, i) => (
          <Button key={`${c.kind}:${i}`} size="sm" onClick={() => props.onChip(c.text)}>
            {c.text}
          </Button>
        ))}
      </div>
    </Section>
  );
}

/**
 * A failed read of the day: the fixed text and the way out in place (the reason is always the same for the student).
 * `quiet`: a day is already shown (a failed re-read), so it is a muted line, no alert and no red.
 */
export function LoadError(props: { onRetry: () => void; quiet?: boolean }) {
  return (
    <div className="today-err">
      {props.quiet === true ? (
        <p className="small muted today-quiet">{texts.refreshFailed}</p>
      ) : (
        <ResultLine kind="fail" role="alert">
          {texts.loadFailed}
        </ResultLine>
      )}
      <IconAction icon="retry" label={texts.retry} onClick={props.onRetry} />
    </div>
  );
}

/**
 * The queue and the nearest deadline; nothing at all when both are empty. With `onOpenQueue` the queue is a quiet text
 * button to «Записи», where the queue is dealt with.
 */
export function StatusBar(props: { queue: string; nearest: string; onOpenQueue?: () => void }) {
  const parts = statusParts(props.queue, props.nearest);
  if (parts.length === 0) {
    return null;
  }
  return (
    <div className="statusbar">
      {parts.map((p) => (
        <span key={p} className="num">
          {p === props.queue && props.onOpenQueue !== undefined ? (
            <Button variant="ghost" size="sm" title={texts.queueOpen} aria-label={`${p}. ${texts.queueOpen}`} onClick={props.onOpenQueue}>
              {p}
            </Button>
          ) : (
            p
          )}
        </span>
      ))}
    </div>
  );
}

/**
 * The main button of the head (TASK-348): «Записать пару» opens «Записи» with the start panel; while a recording runs it
 * reads «● Идёт запись 00:41 · Остановить» and stops it. `now` is the clock in ms; the page ticks it every second while a
 * recording captures (useNow in TodayPage, like the recordings page).
 */
export function RecordAction(props: {
  recordings: readonly RecordingItem[] | null;
  now: number;
  stopping: boolean;
  onStart: () => void;
  onStop: (recordingId: string) => void;
}) {
  const b = recordButton(props.recordings, props.now);
  const waiting = b.kind === "wait";
  return (
    <Button
      variant="primary"
      icon={b.kind === "start" ? "rec" : undefined}
      busy={props.stopping}
      busyLabel={recordTexts.stopping}
      disabled={props.stopping || waiting}
      // The visible text carries the timer; the name stays the action.
      aria-label={b.kind === "stop" && !props.stopping ? recordTexts.stopAction : undefined}
      aria-busy={props.stopping || waiting || undefined}
      title={waiting ? b.title : undefined}
      onClick={() => (b.kind === "start" ? props.onStart() : props.onStop(b.id))}
    >
      {b.kind === "stop" ? (
        <>
          {recordTexts.running}
          {b.time !== "" && (
            <>
              {" "}
              <span className="num">{b.time}</span>
            </>
          )}
          {` · ${recordTexts.stop}`}
        </>
      ) : (
        b.label
      )}
    </Button>
  );
}

/**
 * A failed stop: the recording goes on. «Повторить» stops again (the busy state of the main button is shared), the quiet
 * button leads to «Записи».
 */
export function StopProblem(props: {
  text: string;
  retrying: boolean;
  /** The recording can be stopped right now (the main button is «Остановить»); otherwise «Повторить» would do nothing. */
  canRetry: boolean;
  onRetry: () => void;
  onOpenRecordings: () => void;
}) {
  return (
    <div className="today-err">
      <ResultLine kind="fail" role="alert">
        {props.text}
      </ResultLine>
      <IconAction icon="retry" label={props.retrying ? recordTexts.stopping : recordTexts.retry} busy={props.retrying} disabled={!props.canRetry} onClick={props.onRetry} />
      <IconAction icon="open" label={recordTexts.openRecordings} onClick={props.onOpenRecordings} />
    </div>
  );
}

function ProblemChip(props: { problem: Problem; onClick?: () => void }) {
  const chip = (
    <StatusChip kind={props.problem.kind} icon={props.problem.icon}>
      {props.problem.label}
    </StatusChip>
  );
  return props.onClick === undefined ? (
    chip
  ) : (
    <button type="button" className="today-problem" onClick={props.onClick}>
      {chip}
    </button>
  );
}

/**
 * The pairs of the day (TASK-352): the whole row opens the lesson of its recording (the title is the button, stretched
 * over the row by .row-open; the problem chip and «Записать» stay separate controls above it). A pair without a recording
 * offers «Записать» from ten minutes before its start until its end (it opens the start panel of «Записи»: the core links a
 * recording to the pair in progress, the panel takes no pair); an earlier or a finished one is a plain row.
 */
export function LessonsSection(props: {
  lessons: readonly TodayLesson[];
  recordings: readonly RecordingItem[];
  /** The list of recordings has been read: no «Записать» on a guess before that. */
  recordingsKnown?: boolean;
  /** The start of the day: only failures of this day are shown. */
  day?: string;
  /** The moment the rows are read against; the clock when omitted (tests pass a fixed one). */
  now?: Date;
  onOpenLesson: (recordingId: string) => void;
  /** «Записать»: opens «Записи» with the start panel; without it the button is not offered. */
  onRecord?: () => void;
  /** Opens «Записи», where a failed recording is dealt with. */
  onOpenRecordings?: () => void;
}) {
  const day = props.day ?? "";
  const loose = looseProblems(
    props.lessons.map((l) => l.id),
    props.recordings,
    day,
  );
  return (
    <Section title={texts.lessons} count={props.lessons.length}>
      {loose.length > 0 && (
        <div className="today-loose">
          <ProblemChip problem={loose[0]} onClick={props.onOpenRecordings} />
          {loose.length > 1 && <span className="small muted num"> · {loose.length}</span>}
        </div>
      )}
      {props.lessons.length === 0 ? (
        <Empty>{texts.lessonsEmpty}</Empty>
      ) : (
        <div className="card" role="list">
          {props.lessons.map((l) => {
            const m = lessonLine(l, props.recordings, day, props.now ?? new Date(), props.recordingsKnown ?? true);
            const open = m.open;
            const cls = ["today-row", "today-lesson", m.live?.kind === "rec" ? "live" : "", open.kind === "open" ? "row-openable" : ""];
            return (
              <div key={m.key} className={cls.filter((c) => c !== "").join(" ")} role="listitem">
                <div className="today-tm num">
                  {m.start}
                  {m.end !== "" && <span>{m.end}</span>}
                </div>
                <div className="today-main">
                  <p className="today-t">
                    {open.kind === "open" ? (
                      <button
                        type="button"
                        className="row-open"
                        title={texts.openLesson}
                        aria-label={`${texts.openLesson}: ${m.title}`}
                        onClick={() => props.onOpenLesson(open.recordingId)}
                      >
                        {m.title}
                      </button>
                    ) : (
                      m.title
                    )}
                  </p>
                  {m.meta !== "" && <div className="small muted today-meta">{m.meta}</div>}
                </div>
                <div className="today-st">
                  {m.live !== null && (
                    <StatusChip kind={m.live.kind} icon={m.live.kind === "run" ? "clock" : null}>
                      <span className="num">{m.live.text}</span>
                    </StatusChip>
                  )}
                  {m.problem !== null && <ProblemChip problem={m.problem} onClick={props.onOpenRecordings} />}
                  {m.post !== null && (
                    <>
                      <StatusChip kind="neutral" icon={m.post.canOpen ? "ok" : "clock"}>
                        {m.post.text}
                      </StatusChip>
                      {m.post.homework !== "" && <span className="small muted">{m.post.homework}</span>}
                    </>
                  )}
                </div>
                <div className="today-ac">
                  {open.kind === "record" && props.onRecord !== undefined && (
                    <Button size="sm" icon="rec" aria-label={`${texts.record}: ${m.title}`} onClick={props.onRecord}>
                      {texts.record}
                    </Button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </Section>
  );
}

export function WaitingSection(props: { waiting: readonly TodayWaiting[]; onOpenAssignment?: (id: string) => void }) {
  return (
    <Section title={texts.waiting} count={props.waiting.length}>
      {props.waiting.length === 0 ? (
        <Empty>{texts.waitingEmpty}</Empty>
      ) : (
        <>
          <div className="card" role="list">
            {props.waiting.map((w) => {
              const m = waitingLine(w, new Date());
              const body = (
                <>
                  <Icon name={m.icon} />
                  <div className="today-main">
                    <div className="today-t">{m.label}</div>
                    <div className="small muted today-meta">{m.meta === "" ? m.title : `${m.title} · ${m.meta}`}</div>
                    {m.subject !== "" && <div className="small muted today-subject">{m.subject}</div>}
                  </div>
                </>
              );
              // Navigation only: the confirmation stays on the existing path of the assignment.
              const open = props.onOpenAssignment;
              return w.assignmentId !== "" && open !== undefined ? (
                <div key={m.key} role="listitem">
                  <button type="button" className="today-row today-wait today-dl" onClick={() => open(w.assignmentId)}>
                    {body}
                  </button>
                </div>
              ) : (
                <div key={m.key} className="today-row today-wait" role="listitem">
                  {body}
                </div>
              );
            })}
          </div>
          <p className="small muted today-foot">{texts.waitingFoot}</p>
        </>
      )}
    </Section>
  );
}

/** «Выполнено» in the rows of «Сроки на неделе» (TASK-316): the page owns the calls, the toast and the refusal. */
export interface BurningDone {
  /** The assignment whose call runs, "" none. */
  busyId: string;
  /** The refusal of the last call, with its repeat. */
  issue: ActiveIssue | null;
  /** title is the one of the row, for the toast. */
  onMark: (assignmentId: string, title: string) => void;
  /** «Закрыть» of a refusal that stays above the rows. */
  onDismissIssue?: () => void;
}

export function BurningSection(props: {
  burning: readonly TodayDeadline[];
  onOpenAssignment: (id: string) => void;
  /** «Добавить задание» next to the title; without it the button is not offered. */
  onAdd?: () => void;
  /** The check in every row; without it the rows only open the assignment. */
  done?: BurningDone;
  /** The moment the chips are read against; the clock when omitted (tests pass a fixed one). */
  now?: Date;
}) {
  const now = props.now ?? new Date();
  const onAdd = props.onAdd;
  const done = props.done;
  const issue = done?.issue ?? null;
  // A refusal about a row that left the list stays above the rows.
  const strayIssue = issue !== null && !props.burning.some((b) => issue.ids.includes(b.assignmentId)) ? issue : null;
  return (
    <Section
      title={texts.burning}
      count={props.burning.length}
      action={
        onAdd === undefined ? undefined : (
          <Button size="sm" icon="plus" onClick={onAdd}>
            {addTexts.button}
          </Button>
        )
      }
    >
      {strayIssue !== null && (
        <DoneIssueLine
          issue={strayIssue}
          where="list"
          onRetry={strayIssue.retry}
          onOpen={props.onOpenAssignment}
          onClose={done?.onDismissIssue}
        />
      )}
      {props.burning.length === 0 ? (
        <Empty>{texts.burningEmpty}</Empty>
      ) : (
        <div className="card" role="list">
          {props.burning.map((b) => {
            const m = deadlineLine(b, now);
            const rowIssue = issue !== null && issue.ids.includes(b.assignmentId) ? issue : null;
            // The check is a sibling of the open button: no interactive element inside another.
            return (
              <div key={m.key} className={done === undefined ? undefined : "today-dlitem"} role="listitem">
                {done !== undefined && (
                  <DoneCheck
                    title={m.title}
                    done={false}
                    busy={done.busyId === b.assignmentId}
                    rowId={b.assignmentId}
                    onToggle={() => {
                      if (done.busyId === "") {
                        done.onMark(b.assignmentId, m.title);
                      }
                    }}
                  />
                )}
                <button type="button" className="today-row today-dl" onClick={() => props.onOpenAssignment(b.assignmentId)}>
                  <div className="today-main">
                    <div className="today-t">{m.title}</div>
                    <div className="today-dlmeta">
                      {m.subject !== "" && <div className="small muted today-subject">{m.subject}</div>}
                      <StatusChip kind={m.overdue ? "fail" : m.urgent ? "wait" : "neutral"} icon={m.overdue ? "warn" : m.urgent ? "clock" : null}>
                        <span className="num">{m.chip}</span>
                      </StatusChip>
                    </div>
                  </div>
                </button>
                {done?.busyId === b.assignmentId && (
                  <span className="small muted today-dlnote" role="status">
                    {doneTexts.saving}
                  </span>
                )}
                {rowIssue !== null && (
                  <div className="today-dlissue">
                    <DoneIssueLine issue={rowIssue} where="list" onRetry={rowIssue.retry} onOpen={props.onOpenAssignment} />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Section>
  );
}

/**
 * «Итоги дня»: the messages of paratech after the lessons, then what is done; one list, no sub-headings. Hidden when
 * there is nothing to show.
 */
export function SummarySection(props: {
  posts: readonly TodayPost[];
  done: readonly TodayDone[];
  /** The pairs of the day: a lesson that is a row there is not repeated here (its row opens it). */
  lessons?: readonly TodayLesson[];
  /** The recordings list the pair rows read: a row without a post opens the newest recording, which is not repeated either. */
  recordings?: readonly RecordingItem[];
  /** The moment the pair rows are read against; the clock when omitted (tests pass a fixed one). */
  now?: Date;
  onOpenLesson: (recordingId: string) => void;
  /** The core calls of «Да, в календарь»; without them the button is not offered. */
  calendar?: { api: AgentConfirmApi; deleteApi: RecordingDeleteApi };
}) {
  // A post with a question about the calendar has no other place on the screen, so it stays (without «Открыть»).
  const asks = (p: TodayPost) => props.calendar !== undefined && p.calendar.some((c) => isDeadlineAction(c.kind, c.assignmentId));
  const rows = summaryRows(props.posts, props.done, props.lessons, asks, props.recordings, props.now);
  if (rows.count === 0) {
    return null;
  }
  return (
    <Section title={texts.summary} count={rows.count} className="today-summary">
      <div className="card" role="list">
        {rows.posts.map((p) => {
          const m = postLine(p, new Date(), rows.onDay);
          return (
            <div key={`post:${m.key}`} className="today-row today-wait" role="listitem">
              <Icon name="wave" />
              <div className="today-main">
                <div className="today-t today-post">{m.headline}</div>
                <div className="small muted today-meta num">{m.homework === "" ? m.meta : `${m.meta} · ${m.homework}`}</div>
                {props.calendar !== undefined &&
                  p.calendar
                    .filter((c) => isDeadlineAction(c.kind, c.assignmentId))
                    .filter((c, i, all) => all.findIndex((x) => x.assignmentId === c.assignmentId) === i)
                    .map((c) => (
                      <PostCalendar
                        key={c.assignmentId}
                        api={props.calendar!.api}
                        deleteApi={props.calendar!.deleteApi}
                        assignmentId={c.assignmentId}
                        due={c.due}
                      />
                    ))}
              </div>
              {m.canOpen && <IconAction icon="open" place="left" label={texts.open} onClick={() => props.onOpenLesson(m.recordingId)} />}
            </div>
          );
        })}
        {rows.done.map((d) => {
          const m = doneLine(d);
          return (
            <div key={`done:${m.key}`} className="today-row today-wait" role="listitem">
              <Icon name={m.icon} />
              <div className="today-main">
                <div className="today-t">{m.title}</div>
                {m.meta !== "" && <div className="small muted today-meta num">{m.meta}</div>}
              </div>
              {m.canOpen && <IconAction icon="open" place="left" label={texts.open} onClick={() => props.onOpenLesson(m.recordingId)} />}
            </div>
          );
        })}
      </div>
    </Section>
  );
}
