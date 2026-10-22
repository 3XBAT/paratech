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
  texts,
  waitingLine,
} from "../todayView.js";
import { Button } from "../ui/Button.js";
import { Icon } from "../ui/Icon.js";
import { StatusChip } from "../ui/StatusChip.js";

function Section(props: { title: string; count?: number; className?: string; children: ReactNode }) {
  return (
    <section className={props.className ? `today-sec ${props.className}` : "today-sec"}>
      <h2 className="today-h">
        {props.title}
        {props.count !== undefined && props.count > 0 && <span className="num today-cnt"> · {props.count}</span>}
      </h2>
      {props.children}
    </section>
  );
}

function Empty(props: { children: string }) {
  return <p className="muted today-empty">{props.children}</p>;
}

export function AskSection(props: { chips: readonly TodayChip[]; onChip: (text: string) => void }) {
  return (
    <Section title={texts.askTitle}>
      <p className="small muted today-ask-hint">{texts.askHint}</p>
      {props.chips.length === 0 ? (
        <Empty>{texts.askNoChips}</Empty>
      ) : (
        <div className="today-chips">
          {props.chips.map((c, i) => (
            <Button key={`${c.kind}:${i}`} size="sm" onClick={() => props.onChip(c.text)}>
              {c.text}
            </Button>
          ))}
        </div>
      )}
    </Section>
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

export function LessonsSection(props: {
  lessons: readonly TodayLesson[];
  recordings: readonly RecordingItem[];
  /** The start of the day: only failures of this day are shown. */
  day?: string;
  onOpenSummary: (recordingId: string) => void;
  /** Opens «Записи и задания», where a failed recording is dealt with. */
  onOpenAll?: () => void;
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
          <ProblemChip problem={loose[0]} onClick={props.onOpenAll} />
          {loose.length > 1 && <span className="small muted num"> · {loose.length}</span>}
        </div>
      )}
      {props.lessons.length === 0 ? (
        <Empty>{texts.lessonsEmpty}</Empty>
      ) : (
        <div className="card" role="list">
          {props.lessons.map((l) => {
            const m = lessonLine(l, props.recordings, day);
            return (
              <div key={m.key} className={m.live?.kind === "rec" ? "today-row today-lesson live" : "today-row today-lesson"} role="listitem">
                <div className="today-tm num">
                  {m.start}
                  {m.end !== "" && <span>{m.end}</span>}
                </div>
                <div className="today-main">
                  <p className="today-t">{m.title}</p>
                  {m.meta !== "" && <div className="small muted today-meta">{m.meta}</div>}
                </div>
                <div className="today-st">
                  {m.live !== null && (
                    <StatusChip kind={m.live.kind} icon={m.live.kind === "run" ? "clock" : null}>
                      <span className="num">{m.live.text}</span>
                    </StatusChip>
                  )}
                  {m.problem !== null && <ProblemChip problem={m.problem} onClick={props.onOpenAll} />}
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
                  {m.post?.canOpen === true && (
                    <Button size="sm" onClick={() => props.onOpenSummary(m.post?.recordingId ?? "")}>
                      {texts.open}
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
              const m = waitingLine(w);
              const body = (
                <>
                  <Icon name={m.icon} />
                  <div className="today-main">
                    <div className="today-t">{m.label}</div>
                    <div className="small muted today-meta">{m.meta === "" ? m.title : `${m.title} · ${m.meta}`}</div>
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

export function BurningSection(props: { burning: readonly TodayDeadline[]; onOpenAssignment: (id: string) => void }) {
  return (
    <Section title={texts.burning} count={props.burning.length}>
      {props.burning.length === 0 ? (
        <Empty>{texts.burningEmpty}</Empty>
      ) : (
        <div className="card" role="list">
          {props.burning.map((b) => {
            const m = deadlineLine(b);
            return (
              <div key={m.key} role="listitem">
                <button type="button" className="today-row today-dl" onClick={() => props.onOpenAssignment(b.assignmentId)}>
                  <div className="today-day num">
                    {m.weekday}
                    <span>{m.date}</span>
                  </div>
                  <div className="today-main">
                    <div className="today-t">{m.title}</div>
                    {m.subject !== "" && <div className="small muted today-meta">{m.subject}</div>}
                  </div>
                  <StatusChip kind={m.urgent ? "run" : "neutral"}>
                    <span className="num">{m.chip}</span>
                  </StatusChip>
                </button>
              </div>
            );
          })}
        </div>
      )}
    </Section>
  );
}

export function DoneSection(props: { done: readonly TodayDone[]; onOpenSummary: (recordingId: string) => void }) {
  return (
    <Section title={texts.done} count={props.done.length}>
      {props.done.length === 0 ? (
        <Empty>{texts.doneEmpty}</Empty>
      ) : (
        <div className="card" role="list">
          {props.done.map((d) => {
            const m = doneLine(d);
            return (
              <div key={m.key} className="today-row today-wait" role="listitem">
                <Icon name={m.icon} />
                <div className="today-main">
                  <div className="today-t">{m.title}</div>
                  {m.meta !== "" && <div className="small muted today-meta num">{m.meta}</div>}
                </div>
                {m.canOpen && (
                  <Button size="sm" onClick={() => props.onOpenSummary(m.recordingId)}>
                    {texts.open}
                  </Button>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Section>
  );
}

export function PostsSection(props: { posts: readonly TodayPost[]; onOpenSummary: (recordingId: string) => void }) {
  return (
    <Section title={texts.posts} count={props.posts.length}>
      {props.posts.length === 0 ? (
        <Empty>{texts.postsEmpty}</Empty>
      ) : (
        <div className="card" role="list">
          {props.posts.map((p) => {
            const m = postLine(p);
            return (
              <div key={m.key} className="today-row today-wait" role="listitem">
                <Icon name="wave" />
                <div className="today-main">
                  <div className="today-t today-post">{m.headline}</div>
                  <div className="small muted today-meta num">{m.homework === "" ? m.meta : `${m.meta} · ${m.homework}`}</div>
                </div>
                {m.canOpen && (
                  <Button size="sm" onClick={() => props.onOpenSummary(m.recordingId)}>
                    {texts.open}
                  </Button>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Section>
  );
}
