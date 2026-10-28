import { useEffect, useMemo, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled form (test/recordingimport.test.mjs renders it).
import type { RecordingImportApi } from "../bindings/recordingImport.js";
import {
  checkLecture,
  dateInput,
  type ImportForm,
  importErrorText,
  importOutcome,
  type ImportOutcome,
  importTexts,
  initialForm,
  type LectureMode,
  lectureKinds,
  type LessonChoice,
  maxNameLength,
  pastLessonOptions,
} from "../recordingImportView.js";
import { Button } from "../ui/Button.js";
import { Card, Field, Input, ResultLine, Segmented, Select } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

// «Импортировать запись» (TASK-162): first the lecture, then the native file dialog, which the core opens (the path
// never comes from the page). The core copies the file and queues its transcription; this panel shows the answer.
export function RecordingImportPanel(props: {
  api: RecordingImportApi;
  /** Today's lessons, for «Пара из расписания»; a failure leaves only «Дата и время». */
  loadLessons: () => Promise<LessonChoice[]>;
  /** The subject of the page the panel opens on ("" for none): prefilled, and it narrows the lessons. */
  subject?: string;
  now?: () => number;
  /** A recording was imported (or was already there): the page refreshes; the row is marked by the recording's origin. */
  onImported: (recordingId: string, existing: boolean) => void;
  onClose: () => void;
}) {
  const clock = props.now ?? Date.now;
  const subject = props.subject ?? "";
  const [lessons, setLessons] = useState<LessonChoice[] | null>(null);
  const [form, setForm] = useState<ImportForm>(() => initialForm(subject, new Date(clock()), false));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [outcome, setOutcome] = useState<ImportOutcome | null>(null);
  const running = useRef(false);
  const mounted = useRef(true);
  const touched = useRef(false);

  useEffect(() => {
    mounted.current = true;
    props
      .loadLessons()
      .then(
        (list) => list,
        () => [] as LessonChoice[],
      )
      .then((list) => {
        if (mounted.current) {
          setLessons(list);
        }
      });
    return () => {
      mounted.current = false;
    };
    // The lessons are read once when the panel opens.
  }, []);

  const options = useMemo(() => pastLessonOptions(lessons ?? [], clock(), subject), [lessons, clock, subject]);
  // The first answer picks «Пара из расписания» when there is something to choose, unless the user already chose.
  useEffect(() => {
    if (lessons !== null && !touched.current && options.length > 0) {
      setForm((f) => ({ ...f, mode: "lesson" }));
    }
  }, [lessons, options.length]);

  const change = (patch: Partial<ImportForm>) => {
    touched.current = true;
    setProblem("");
    setForm((f) => ({ ...f, ...patch }));
  };

  async function onChoose() {
    if (running.current) {
      return;
    }
    const check = checkLecture(form, clock());
    if (!check.ok) {
      setProblem(check.problem);
      return;
    }
    running.current = true;
    setBusy(true);
    setProblem("");
    setOutcome(null);
    try {
      const out = importOutcome(await props.api.importRecording(check.lecture));
      if (mounted.current) {
        setOutcome(out);
        if (out.kind !== "cancelled") {
          props.onImported(out.recordingId, out.kind === "existing");
        }
      }
    } catch (err) {
      if (mounted.current) {
        setProblem(importErrorText(err));
      }
    } finally {
      running.current = false;
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  const modes = [
    { value: "lesson" as LectureMode, label: importTexts.modeLesson },
    { value: "manual" as LectureMode, label: importTexts.modeManual },
  ];

  return (
    <Card className="box rec-import-panel" label={importTexts.title}>
      <h2>{importTexts.title}</h2>
      <span className="small muted box-desc">{importTexts.explain}</span>

      <h3 className="rec-import-step">{importTexts.lectureStep}</h3>
      <Segmented label={importTexts.modeLabel} options={modes} value={form.mode} onChange={(mode) => change({ mode })} />
      {form.mode === "lesson" ? (
        options.length === 0 ? (
          <p className="small muted">{lessons === null ? "Загрузка…" : importTexts.lessonsEmpty}</p>
        ) : (
          <Field label={importTexts.lessonLabel}>
            {(id) => (
              <Select id={id} value={form.lessonId} disabled={busy} onChange={(e) => change({ lessonId: e.target.value })}>
                <option value="">{importTexts.lessonNone}</option>
                {options.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )
      ) : (
        <>
          <div className="rec-import-when">
            <Field label={importTexts.dateLabel}>
              {(id) => (
                <Input
                  id={id}
                  type="date"
                  value={form.date}
                  max={dateInput(new Date(clock()))}
                  readOnly={busy}
                  onChange={(e) => change({ date: e.target.value })}
                />
              )}
            </Field>
            <Field label={importTexts.timeLabel}>
              {(id) => <Input id={id} type="time" value={form.time} readOnly={busy} onChange={(e) => change({ time: e.target.value })} />}
            </Field>
          </div>
          <Field label={importTexts.subjectLabel}>
            {(id) => (
              <Input
                id={id}
                value={form.subject}
                maxLength={maxNameLength}
                readOnly={busy}
                onChange={(e) => change({ subject: e.target.value })}
              />
            )}
          </Field>
          <Field label={importTexts.kindLabel}>
            {(id) => (
              <Select id={id} value={form.kind} disabled={busy} onChange={(e) => change({ kind: e.target.value })}>
                {lectureKinds.map((k) => (
                  <option key={k.value} value={k.value}>
                    {k.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label={importTexts.titleLabel}>
            {(id) => (
              <Input id={id} value={form.title} maxLength={maxNameLength} readOnly={busy} onChange={(e) => change({ title: e.target.value })} />
            )}
          </Field>
        </>
      )}

      <h3 className="rec-import-step">{importTexts.fileStep}</h3>
      <p className="small muted rec-import-note">
        <Icon name="lock" /> {importTexts.local}
      </p>
      <p className="small muted">{importTexts.formats}</p>

      {problem !== "" && (
        <ResultLine kind="fail" role="alert">
          {problem}
        </ResultLine>
      )}
      {outcome !== null && (
        <ResultLine kind={outcome.kind === "ok" ? "ok" : "wait"} role="status">
          {outcome.text}
        </ResultLine>
      )}
      <div className="acts start">
        <Button variant="primary" icon="up" busy={busy} busyLabel={importTexts.importing} disabled={busy} onClick={() => void onChoose()}>
          {importTexts.choose}
        </Button>
        <Button variant="ghost" disabled={busy} onClick={props.onClose}>
          {importTexts.close}
        </Button>
      </div>
    </Card>
  );
}
