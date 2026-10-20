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
  correctionDraft,
  errorText,
  groupHits,
  hitCountText,
  hitPlace,
  kindOptions,
  lessonLabel,
  maxSubjectLength,
  readyMarks,
  subjectMeta,
  subjectName,
  subjectPlaceholder,
} from "../knowledgeView.js";
import { Button } from "../ui/Button.js";
import { Card, Field, Input, ResultLine, Select } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

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
        <li key={l.recordingId} className="kb-lesson">
          <button type="button" className="kb-item" onClick={() => props.onOpen(l)}>
            <span className="kb-name">{lessonLabel(l)}</span>
            <ReadyMarks lesson={l} />
          </button>
          {onEdit && (
            <Button variant="ghost" onClick={() => onEdit(l)}>
              Указать предмет
            </Button>
          )}
        </li>
      ))}
    </ul>
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
        <div key={g.lesson.recordingId} className="card kb-group">
          <button type="button" className="kb-item kb-group-head" onClick={() => props.onOpen(g.lesson, null)}>
            <span className="kb-name">{lessonLabel(g.lesson)}</span>
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
