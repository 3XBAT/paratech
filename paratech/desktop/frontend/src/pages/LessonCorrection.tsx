import { useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled form (test/lesson.test.mjs renders it).
import type { AssignmentItem } from "../bindings/homework.js";
import { errorText } from "../homeworkView.js";
import { Button } from "../ui/Button.js";
import { Card, Field, Input, ResultLine } from "../ui/controls.js";

// «Поправить предмет и преподавателя» of an assignment (TASK-092). The fields hold the student's own correction; the
// placeholders show what is shown now. An empty field removes that correction, so the schedule or the recording shows
// again. The core cleans and stores the texts.

/** The longest subject or teacher the core keeps (values.MaxNameRunes). */
export const maxNameLength = 200;

export function LessonCorrection(props: {
  assignment: AssignmentItem;
  correct: (id: string, subject: string, teacher: string) => Promise<AssignmentItem>;
  onCorrected: (a: AssignmentItem) => void;
  onClose: () => void;
  disabled?: boolean;
}) {
  const a = props.assignment;
  const [subject, setSubject] = useState(a.correctedSubject);
  const [teacher, setTeacher] = useState(a.correctedTeacher);
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
      const next = await props.correct(a.id, subject, teacher);
      setSubject(next.correctedSubject);
      setTeacher(next.correctedTeacher);
      props.onCorrected(next);
    } catch (err) {
      setError(errorText(err));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  const off = busy || props.disabled === true;
  return (
    <Card className="box hw-lesson-form">
      <h2>Поправить предмет и преподавателя</h2>
      <p className="small muted box-desc">Пустое поле — снова как в расписании или по записи.</p>
      <Field label="Предмет">
        {(fid) => (
          <Input
            id={fid}
            value={subject}
            maxLength={maxNameLength}
            placeholder={a.subject}
            readOnly={busy}
            onChange={(e) => setSubject(e.target.value)}
          />
        )}
      </Field>
      <Field label="Преподаватель">
        {(fid) => (
          <Input
            id={fid}
            value={teacher}
            maxLength={maxNameLength}
            placeholder={a.teacher}
            readOnly={busy}
            onChange={(e) => setTeacher(e.target.value)}
          />
        )}
      </Field>
      <div className="acts start">
        <Button variant="primary" busy={busy} busyLabel="Сохранение…" disabled={off} onClick={() => void onSave()}>
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
