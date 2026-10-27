// Parts of «База знаний» (TASK-091) that only show what the core sent: subjects, lessons, search hits and the
// subject/kind form. Titles, subjects and snippets are imported, typed or model text: React text nodes only, no HTML;
// the match of a snippet is marked with <mark> around a text node.
import { useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled modules (test/knowledge.test.mjs renders these parts).
import type {
  KnowledgeHit,
  KnowledgeLesson,
  KnowledgeSearchResult,
  KnowledgeSubject,
  LessonKind,
} from "../bindings/knowledgeBase.js";
import {
  checkPastDate,
  correctionDraft,
  dateCheckText,
  errorText,
  groupHits,
  hitCountText,
  hitPlace,
  isNoteOnly,
  kindOptions,
  lectureLabel,
  lessonsWithoutNotes,
  localDay,
  maxSubjectLength,
  noteLectureTexts,
  type PastChoice,
  readyMarks,
  subjectMeta,
  subjectName,
  subjectPlaceholder,
} from "../knowledgeView.js";
import { noSubjectText } from "../ownNotesView.js";
import { Button } from "../ui/Button.js";
import { Card, Field, Input, ResultLine, Select } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { StatusChip } from "../ui/StatusChip.js";

/** «Кратко ✓ · Текст лекции · Расшифровка ✓»: a ready one has the tick and the word «готово» for screen readers. */
export function ReadyMarks(props: { lesson: KnowledgeLesson }) {
  return (
    <span className="kb-marks">
      {readyMarks(props.lesson).map((m) => (
        <span key={m.key} className={m.ready ? "kb-mark on" : "kb-mark"}>
          {m.ready && <Icon name="ok" />}
          {m.label}
          <span className="vh">{m.ready ? ": готово" : ": нет"}</span>
        </span>
      ))}
    </span>
  );
}

/** The subjects, each a button that opens its lessons. */
export function SubjectList(props: { subjects: readonly KnowledgeSubject[]; onOpen: (s: KnowledgeSubject) => void }) {
  return (
    <ul className="card kb-list" aria-label="Предметы">
      {props.subjects.map((s) => (
        <li key={s.key}>
          <button type="button" className={s.noSubject ? "kb-item kb-none" : "kb-item"} onClick={() => props.onOpen(s)}>
            <span className="kb-name">{s.name}</span>
            <span className="small muted num">{subjectMeta(s)}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}

/** The lessons of a subject by date; in «Без предмета» each has «Указать предмет». */
export function LessonList(props: {
  lessons: readonly KnowledgeLesson[];
  onOpen: (l: KnowledgeLesson) => void;
  /** Shown in «Без предмета»: opens the lesson with the subject form. */
  onEdit?: (l: KnowledgeLesson) => void;
}) {
  const onEdit = props.onEdit;
  return (
    <ul className="card kb-list" aria-label="Занятия">
      {props.lessons.map((l) => (
        <li key={l.key} className="kb-lesson">
          <button type="button" className="kb-item" onClick={() => props.onOpen(l)}>
            <span className="kb-name">{lectureLabel(l)}</span>
            {isNoteOnly(l) ? (
              <span>
                <StatusChip kind="neutral" icon="doc">
                  {noteLectureTexts.ownChip}
                </StatusChip>
              </span>
            ) : (
              <ReadyMarks lesson={l} />
            )}
          </button>
          {onEdit && !isNoteOnly(l) && (
            <Button variant="ghost" onClick={() => onEdit(l)}>
              Указать предмет
            </Button>
          )}
        </li>
      ))}
    </ul>
  );
}

/**
 * «Добавить конспект прошедшей пары»: a recorded lesson without notes, a date or «без даты»; the choice opens the
 * notes block for it. Needs the subject in the library (the notes are filed under it): off with the block's own hint
 * otherwise. Past schedule slots are not offered: the core has no list of them yet (follow-up, ADR-0027).
 */
export function AddPastNote(props: {
  subject: Pick<KnowledgeSubject, "subjectId" | "noSubject">;
  lectures: readonly KnowledgeLesson[];
  onChoose: (c: PastChoice) => void;
  /** Today as "YYYY-MM-DD"; the page's clock in the app, a fixed day in tests. */
  today?: string;
}) {
  const known = props.subject.subjectId !== "";
  const [open, setOpen] = useState(false);
  const [lessonKey, setLessonKey] = useState("");
  const [date, setDate] = useState("");
  const [dateError, setDateError] = useState("");
  const today = props.today ?? localDay(new Date());
  const choices = lessonsWithoutNotes(props.lectures);
  const t = noteLectureTexts;
  const hint = known ? "" : noSubjectText(props.subject.noSubject ? "no-subject" : "none");

  function chooseDate() {
    const c = checkPastDate(date, today);
    if (c.kind !== "ok") {
      setDateError(dateCheckText(c));
      return;
    }
    setDateError("");
    props.onChoose({ kind: "day", date: c.date });
  }

  return (
    <div className="kb-past">
      <Button
        icon="plus"
        aria-expanded={open}
        disabled={!known}
        disabledReason={known ? undefined : hint}
        onClick={() => setOpen((o) => !o)}
      >
        {t.addPast}
      </Button>
      {!known && <p className="small muted">{hint}</p>}
      {known && open && (
        <Card className="box kb-form">
          <h2>{t.addPastTitle}</h2>
          <Field label={t.pickRecording}>
            {(fid) => (
              <Select id={fid} value={lessonKey} disabled={choices.length === 0} onChange={(e) => setLessonKey(e.target.value)}>
                <option value="">{choices.length === 0 ? t.pickRecordingNone : "—"}</option>
                {choices.map((l) => (
                  <option key={l.key} value={l.key}>
                    {lectureLabel(l)}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <div className="acts start">
            <Button
              disabled={lessonKey === ""}
              onClick={() => {
                const lesson = choices.find((l) => l.key === lessonKey);
                if (lesson) {
                  props.onChoose({ kind: "recording", lesson });
                }
              }}
            >
              {t.choose}
            </Button>
          </div>
          <Field label={t.pickDate}>
            {(fid) => (
              <Input
                id={fid}
                type="date"
                max={today}
                value={date}
                aria-invalid={dateError !== ""}
                onChange={(e) => {
                  setDate(e.target.value);
                  setDateError("");
                }}
              />
            )}
          </Field>
          <div className="acts start">
            <Button onClick={chooseDate}>{t.choose}</Button>
            <Button onClick={() => props.onChoose({ kind: "undated" })}>{t.pickUndated}</Button>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              {t.close}
            </Button>
          </div>
          {dateError !== "" && (
            <ResultLine kind="fail" role="alert">
              {dateError}
            </ResultLine>
          )}
        </Card>
      )}
    </div>
  );
}

/** «…до <mark>совпадения</mark> после…» as text nodes. */
export function Snippet(props: { hit: Pick<KnowledgeHit, "before" | "match" | "after"> }) {
  const h = props.hit;
  return (
    <span className="kb-snippet">
      {h.before}
      <mark>{h.match}</mark>
      {h.after}
    </span>
  );
}

/** The hits by lesson; a click on a hit opens the lesson at that place, on the lesson at its top. */
export function HitList(props: {
  result: KnowledgeSearchResult;
  onOpen: (lesson: KnowledgeLesson, hit: KnowledgeHit | null) => void;
}) {
  const groups = groupHits(props.result.hits);
  return (
    <>
      <p className="small muted kb-count" role="status">
        {hitCountText(props.result.hits.length, groups.length, props.result.truncated)}
      </p>
      {groups.map((g) => (
        <div key={g.lesson.key} className="card kb-group">
          <button type="button" className="kb-item kb-group-head" onClick={() => props.onOpen(g.lesson, null)}>
            <span className="kb-name">{lectureLabel(g.lesson)}</span>
            <span className="small muted">{subjectName(g.lesson)}</span>
          </button>
          <ul className="kb-hits">
            {g.hits.map((h, i) => (
              <li key={`${h.section}:${h.index}:${i}`}>
                <button type="button" className="kb-hit" onClick={() => props.onOpen(g.lesson, h)}>
                  <span className="small muted num kb-place">{hitPlace(h)}</span>
                  <Snippet hit={h} />
                </button>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </>
  );
}

/**
 * «Предмет и тип занятия» of a recording. The fields hold the user's own correction; the placeholder and the first
 * option show what is shown without it. An empty field removes that correction. The core checks and stores the values.
 */
export function LessonInfoForm(props: {
  lesson: KnowledgeLesson;
  save: (id: string, subject: string, kind: LessonKind) => Promise<KnowledgeLesson>;
  onSaved: (l: KnowledgeLesson) => void;
  onClose: () => void;
}) {
  const l = props.lesson;
  const draft = correctionDraft(l);
  const [subject, setSubject] = useState(draft.subject);
  const [kind, setKind] = useState<LessonKind>(draft.kind);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const running = useRef(false); // a double click comes before the re-render that disables the button

  async function onSave() {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
    try {
      const next = await props.save(l.recordingId, subject, kind);
      const d = correctionDraft(next);
      setSubject(d.subject);
      setKind(d.kind);
      props.onSaved(next);
    } catch (err) {
      setError(errorText(err));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  return (
    <Card className="box kb-form">
      <h2>Предмет и тип занятия</h2>
      <p className="small muted box-desc">Пустое поле — снова как в записи или в расписании.</p>
      <Field label="Предмет">
        {(fid) => (
          <Input
            id={fid}
            value={subject}
            maxLength={maxSubjectLength}
            placeholder={subjectPlaceholder(l)}
            readOnly={busy}
            onChange={(e) => setSubject(e.target.value)}
          />
        )}
      </Field>
      <Field label="Тип занятия">
        {(fid) => (
          <Select id={fid} value={kind} disabled={busy} onChange={(e) => setKind(e.target.value as LessonKind)}>
            {kindOptions(l).map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <div className="acts start">
        <Button variant="primary" busy={busy} busyLabel="Сохранение…" disabled={busy} onClick={() => void onSave()}>
          Сохранить
        </Button>
        <Button variant="ghost" disabled={busy} onClick={props.onClose}>
          Закрыть
        </Button>
      </div>
      {error !== "" && (
        <ResultLine kind="fail" role="alert">
          {error}
        </ResultLine>
      )}
    </Card>
  );
}
