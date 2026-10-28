import { useEffect, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled dialogs (test/homework.test.mjs renders them).
import {
  type AssignmentItem,
  type DeliveryItem,
  type HomeworkApi,
  type SendFormat,
  sendFormats,
  type SendPreparation,
} from "../bindings/homework.js";
import type { LibraryApi, LibrarySubject } from "../bindings/library.js";
import type { TeacherRole } from "../bindings/recipient.js";
import {
  type AddFieldErrors,
  type AddForm,
  addSession,
  addTexts,
  changeTexts,
  editForm,
  editOutcome,
  editRequest,
  editSubjectId,
  emptyAddForm,
  errorText,
  missingSubjectName,
  subjectMissingText,
  noSubjectLabel,
  noteMax,
  sendFormatLabels,
  sendRetryable,
  sizeText,
  titleMax,
  validateAddForm,
  validateEditForm,
} from "../homeworkView.js";
import { choiceOf, recipientTexts, recipientView, sendBlocked } from "../recipientView.js";
import { Button } from "../ui/Button.js";
import { Dialog, Field, Input, ResultLine, Segmented, Select } from "../ui/controls.js";
import { RecipientName, TeacherChoiceGroup } from "./RecipientParts.js";

// The two dialogs that lead to an email. Each one has a single call site of its binding, in its confirm button handler
// (test/homework.test.mjs checks it): an email goes out only after the student pressed that button (AC-14, AC-15).
// Recipient, subject and file name come from the core's trusted state and are shown as plain text.

/** How the send dialog ended: the email was created, the student cancelled, or the confirmation failed. */
export type SendResult = "sent" | "cancelled" | "failed";

/**
 * Asks to send the prepared homework: the draft version, recipient, subject and attachment, «Отправить» and «Отмена».
 * Any way of closing it other than «Отправить» cancels the send in the core. version is the one the export wrote to
 * the attached file. The format choice asks the page for a new preparation (onFormat): the dialog never prepares a
 * send itself. While switching, nothing can be confirmed; problem is why the last switch did not happen ("" none), the
 * shown preparation then stays as it was.
 */
export function SendDialog(props: {
  api: HomeworkApi;
  prep: SendPreparation;
  version: number;
  switching?: boolean;
  problem?: string;
  onFormat?: (format: SendFormat) => void;
  /** Asks the page for a new preparation of the same send (the teachers changed while the question was open). */
  onRefresh?: () => void;
  onClose: (result: SendResult) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  const [retry, setRetry] = useState(false);
  const [error, setError] = useState("");
  // «Кому отправить?»: the role clicked; the address that goes back is the one that option shows now.
  const [role, setRole] = useState<TeacherRole | "">("");
  const view = recipientView(props.prep);
  const choice = view.kind === "choose" ? choiceOf(props.prep.options, role) : null;
  const blocked = sendBlocked(view, choice);
  // Synchronous guards: a double click comes before the re-render that disables the button.
  const running = useRef(false);
  const confirmed = useRef(false);
  const retryable = useRef(false);

  // The only call site of confirmSend (AC-14). One try per dialog, except after sendIncomplete: the core says
  // repeating the same proposal sends no second email, so «Повторить» calls it again with the same proposalId.
  async function onConfirmSend() {
    if (running.current || (confirmed.current && !retryable.current) || blocked !== "") {
      return;
    }
    running.current = true;
    confirmed.current = true;
    retryable.current = false;
    setTried(true);
    setRetry(false);
    setBusy(true);
    setError("");
    try {
      await props.api.confirmSend(props.prep.proposalId, props.prep.subject, props.prep.attachmentFormat, choice);
      props.onClose("sent");
    } catch (err) {
      retryable.current = sendRetryable(err);
      setRetry(retryable.current);
      setError(errorText(err));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  async function onCancelSend() {
    if (running.current || props.switching === true) {
      return; // a confirmation on its way is not cancelled halfway
    }
    running.current = true;
    try {
      await props.api.cancelSend(props.prep.proposalId);
    } catch {
      // Nothing to show: a send that was not cancelled still cannot go out without onConfirmSend.
    } finally {
      running.current = false;
    }
    props.onClose(confirmed.current ? "failed" : "cancelled");
  }

  const switching = props.switching === true;
  const formatLocked = busy || tried || switching || props.onFormat === undefined;
  return (
    <Dialog
      open
      title="Отправить домашнее задание?"
      confirmLabel={busy ? "Отправка…" : retry ? "Повторить" : "Отправить"}
      cancelLabel={tried ? "Закрыть" : "Отмена"}
      confirmDisabled={busy || switching || (tried && !retry) || blocked !== ""}
      onConfirm={() => void onConfirmSend()}
      onCancel={() => void onCancelSend()}
    >
      {view.kind === "choose" && (
        <>
          <TeacherChoiceGroup options={view.options} value={role} disabled={busy || tried || switching} onChange={setRole} />
          {choice === null && <p className="small muted">{recipientTexts.chooseHintDialog}</p>}
        </>
      )}
      {view.kind === "choice_changed" && (
        <>
          <ResultLine kind="fail" role="alert">
            {recipientTexts.choiceChangedDialog}
          </ResultLine>
          {props.onRefresh !== undefined && (
            <Button size="sm" disabled={busy || tried || switching} onClick={props.onRefresh}>
              {recipientTexts.refreshDialog}
            </Button>
          )}
        </>
      )}
      {(view.kind === "no_email" || view.kind === "no_subject") && (
        <ResultLine kind="fail" role="alert">
          {view.text}
        </ResultLine>
      )}
      <dl className="hw-send">
        <dt>Черновик</dt>
        <dd className="num">Версия {props.version}</dd>
        {view.kind === "to" && (
          <>
            <dt>{recipientTexts.to}</dt>
            <dd>
              <RecipientName name={view.name} address={view.address} />
            </dd>
          </>
        )}
        <dt>Тема</dt>
        <dd>
          <bdi>{props.prep.subject}</bdi>
        </dd>
        <dt>Вложение</dt>
        <dd>
          <bdi>{props.prep.attachmentName}</bdi> <span className="num">({sizeText(props.prep.attachmentSize)})</span>
        </dd>
      </dl>
      <fieldset className="hw-format" disabled={formatLocked}>
        <legend>Формат вложения</legend>
        {/* The disabled fieldset also disables the segment buttons. */}
        <Segmented
          label="Формат вложения"
          options={sendFormats.map((f) => ({ value: f, label: sendFormatLabels[f] }))}
          value={props.prep.attachmentFormat}
          onChange={(f) => {
            if (f !== props.prep.attachmentFormat) {
              props.onFormat?.(f);
            }
          }}
        />
        {switching && (
          <span className="small muted" role="status">
            Подготовка…
          </span>
        )}
      </fieldset>
      {props.problem && (
        <ResultLine kind="fail" role="alert">
          {props.problem}
        </ResultLine>
      )}
      <p>Письмо уйдёт сразу после подтверждения.</p>
      {error && (
        <ResultLine kind="fail" role="alert">
          {error}
        </ResultLine>
      )}
    </Dialog>
  );
}

/**
 * «Добавить задание» (ADR-0029): a small form. The requestId token is made when the form opens (the dialog is mounted
 * only while open); a second call cannot start while one runs. onCreated gets the stored assignment, the caller
 * refreshes its list and closes. Everything the student typed is shown as input values and text nodes only.
 */
export function AddAssignmentDialog(props: {
  api: Pick<HomeworkApi, "createAssignment">;
  library: Pick<LibraryApi, "subjects">;
  /** Subjects known up front (tests); the dialog loads them itself otherwise. */
  initialSubjects?: readonly LibrarySubject[];
  onCreated: (a: AssignmentItem) => void;
  onClose: () => void;
}) {
  const [session] = useState(() => addSession(() => crypto.randomUUID()));
  const [form, setForm] = useState<AddForm>(emptyAddForm);
  const [subjects, setSubjects] = useState<readonly LibrarySubject[]>(props.initialSubjects ?? []);
  const [subjectsFailed, setSubjectsFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<AddFieldErrors>({});
  const [general, setGeneral] = useState("");
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    if (props.initialSubjects === undefined) {
      props.library.subjects().then(
        (s) => mounted.current && setSubjects(s),
        () => mounted.current && setSubjectsFailed(true),
      );
    }
    return () => {
      mounted.current = false;
    };
    // The subjects are read once, when the form opens.
  }, []);

  function change(patch: Partial<AddForm>, field?: keyof AddFieldErrors) {
    session.edited();
    setForm((f) => ({ ...f, ...patch }));
    setGeneral("");
    if (field !== undefined) {
      setErrors((e) => ({ ...e, [field]: undefined }));
    }
  }

  async function submit() {
    if (busy) {
      return;
    }
    const local = validateAddForm(form);
    setErrors(local);
    setGeneral("");
    if (Object.keys(local).length > 0) {
      return;
    }
    setBusy(true);
    try {
      const out = await session.submit(props.api, form);
      if (out === null || !mounted.current) {
        return;
      }
      if (out.created !== null) {
        props.onCreated(out.created);
        return;
      }
      setErrors(out.fieldErrors);
      setGeneral(out.general);
    } catch (err) {
      if (mounted.current) {
        setGeneral(errorText(err));
      }
    } finally {
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  return (
    <Dialog
      open
      title={addTexts.title}
      confirmLabel={busy ? addTexts.adding : addTexts.add}
      cancelLabel={addTexts.cancel}
      confirmDisabled={busy}
      cancelDisabled={busy}
      onConfirm={() => void submit()}
      onCancel={props.onClose}
    >
      <AssignmentFields
        form={form}
        errors={errors}
        general={general}
        busy={busy}
        subjects={subjects}
        subjectHint={subjectsFailed ? addTexts.subjectsFailed : undefined}
        noSubject={addTexts.noSubject}
        onChange={change}
        onSubmit={() => void submit()}
      />
    </Dialog>
  );
}

/**
 * «Задание удалено» with «Вернуть» in place of a deleted assignment; the line stays until the student leaves the page.
 * The button takes the focus: the «Удалить» that had it is gone.
 */
export function RemovedLine(props: { busy: boolean; onRestore: () => void }) {
  return (
    <span className="hw-removed">
      <span role="status">{changeTexts.removed}</span>
      <Button autoFocus busy={props.busy} busyLabel={changeTexts.restoring} disabled={props.busy} onClick={props.onRestore}>
        {changeTexts.restore}
      </Button>
    </span>
  );
}

const unchosenValue = "?";

/** The fields of the add and the edit form: the same form, so the two cannot drift apart. */
function AssignmentFields(props: {
  form: AddForm;
  errors: AddFieldErrors;
  general: string;
  busy: boolean;
  subjects: readonly LibrarySubject[];
  subjectHint?: string;
  noSubject: string;
  /** Set: the select shows this extra option until the student chooses (the stored subject is not in the library). */
  unchosen?: string;
  onChange: (patch: Partial<AddForm>, field?: keyof AddFieldErrors) => void;
  onSubmit: () => void;
}) {
  const { form, errors, busy, onChange } = props;
  return (
    <div className="hw-add">
      <Field label={addTexts.what} error={errors.title}>
        {(id) => (
          <Input
            id={id}
            value={form.title}
            maxLength={titleMax}
            disabled={busy}
            aria-invalid={errors.title !== undefined}
            onChange={(e) => onChange({ title: e.target.value }, "title")}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                e.preventDefault();
                props.onSubmit();
              }
            }}
          />
        )}
      </Field>
      <Field label={addTexts.subject} hint={props.subjectHint} error={errors.subject}>
        {(id) => (
          <Select
            id={id}
            value={props.unchosen !== undefined ? unchosenValue : form.subjectId}
            disabled={busy}
            aria-invalid={errors.subject !== undefined}
            onChange={(e) => onChange({ subjectId: e.target.value }, "subject")}
          >
            {props.unchosen !== undefined && <option value={unchosenValue}>{props.unchosen}</option>}
            <option value="">{props.noSubject}</option>
            {props.subjects.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <div className="hw-add-due">
        <Field label={addTexts.due} error={errors.due}>
          {(id) => (
            <Input
              id={id}
              type="date"
              value={form.date}
              disabled={busy}
              aria-invalid={errors.due !== undefined}
              onChange={(e) => onChange({ date: e.target.value }, "due")}
            />
          )}
        </Field>
        <Field label={addTexts.time} hint={addTexts.timeHint} error={errors.time}>
          {(id) => (
            <Input
              id={id}
              type="time"
              value={form.time}
              disabled={busy}
              aria-invalid={errors.time !== undefined}
              onChange={(e) => onChange({ time: e.target.value }, "time")}
            />
          )}
        </Field>
      </div>
      <Field label={addTexts.note} error={errors.note}>
        {(id) => (
          <textarea
            id={id}
            className="input hw-add-note"
            rows={3}
            value={form.note}
            maxLength={noteMax}
            disabled={busy}
            aria-invalid={errors.note !== undefined}
            onChange={(e) => onChange({ note: e.target.value }, "note")}
          />
        )}
      </Field>
      {props.general && (
        <ResultLine kind="fail" role="alert">
          {props.general}
        </ResultLine>
      )}
    </div>
  );
}

/**
 * «Изменить» (TASK-232): the add form prefilled with the stored assignment. The version sent is the one of the
 * current assignment prop, so after a «changed» refusal the page reloads it (onStale) and the student repeats the
 * save over the new version with what was typed kept. Subjects are matched by name once they are loaded; until then
 * and after a failed load nothing can be saved (an empty choice would clear the subject).
 */
export function EditAssignmentDialog(props: {
  api: Pick<HomeworkApi, "editAssignment">;
  library: Pick<LibraryApi, "subjects">;
  assignment: AssignmentItem;
  /** Subjects known up front (tests); the dialog loads them itself otherwise. */
  initialSubjects?: readonly LibrarySubject[];
  onSaved: (a: AssignmentItem) => void;
  /** The shown data is out of date (changed, deleted, not found): reload it; resolves when the new data is in. */
  onStale: () => Promise<void>;
  onClose: () => void;
}) {
  const [subjects, setSubjects] = useState<readonly LibrarySubject[] | null>(props.initialSubjects ?? null);
  const [subjectsFailed, setSubjectsFailed] = useState(false);
  const [form, setForm] = useState<AddForm>(() => editForm(props.assignment, props.initialSubjects ?? []));
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<AddFieldErrors>({});
  const [general, setGeneral] = useState("");
  const mounted = useRef(true);
  const touchedSubject = useRef(false);
  const [touched, setTouched] = useState(false);
  const running = useRef(false);
  const latest = useRef(props.assignment);
  latest.current = props.assignment;

  useEffect(() => {
    mounted.current = true;
    if (props.initialSubjects === undefined) {
      props.library.subjects().then(
        (s) => {
          if (!mounted.current) {
            return;
          }
          setSubjects(s);
          if (!touchedSubject.current) {
            setForm((f) => ({ ...f, subjectId: editSubjectId(latest.current, s) }));
          }
        },
        () => mounted.current && setSubjectsFailed(true),
      );
    }
    return () => {
      mounted.current = false;
    };
    // The subjects are read once, when the form opens.
  }, []);

  function change(patch: Partial<AddForm>, field?: keyof AddFieldErrors) {
    if (patch.subjectId !== undefined) {
      touchedSubject.current = true;
      setTouched(true);
    }
    setForm((f) => ({ ...f, ...patch }));
    setGeneral("");
    if (field !== undefined) {
      setErrors((e) => ({ ...e, [field]: undefined }));
    }
  }

  const ready = subjects !== null;
  // The stored subject has no library match: saving the empty choice would silently clear it, so the student chooses.
  const missing = ready && !touched ? missingSubjectName(props.assignment, subjects) : "";

  async function submit() {
    if (running.current || !ready || missing !== "") {
      return;
    }
    const local = validateEditForm(form);
    setErrors(local);
    setGeneral("");
    if (Object.keys(local).length > 0) {
      return;
    }
    running.current = true;
    setBusy(true);
    try {
      const out = editOutcome(await props.api.editAssignment(editRequest(latest.current, form)));
      if (!mounted.current) {
        return;
      }
      if (out.saved !== null) {
        props.onSaved(out.saved);
        return;
      }
      setErrors(out.fieldErrors);
      setGeneral(out.general);
      if (out.reload) {
        await props.onStale();
      }
    } finally {
      running.current = false;
      if (mounted.current) {
        setBusy(false);
      }
    }
  }

  const hint = subjectsFailed ? changeTexts.subjectsFailed : ready ? undefined : changeTexts.subjectsLoading;
  return (
    <Dialog
      open
      title={changeTexts.editTitle}
      confirmLabel={busy ? changeTexts.saving : changeTexts.save}
      cancelLabel={addTexts.cancel}
      confirmDisabled={busy || !ready || missing !== ""}
      cancelDisabled={busy}
      onConfirm={() => void submit()}
      onCancel={props.onClose}
    >
      <AssignmentFields
        form={form}
        errors={missing !== "" ? { ...errors, subject: subjectMissingText(missing) } : errors}
        unchosen={missing !== "" ? "Выберите предмет" : undefined}
        general={general}
        busy={busy}
        subjects={subjects ?? []}
        subjectHint={hint}
        noSubject={noSubjectLabel(props.assignment)}
        onChange={change}
        onSubmit={() => void submit()}
      />
    </Dialog>
  );
}

/**
 * Asks to send again an email whose outcome is unknown: the teacher may get it twice. Only the core's canSendAgain
 * opens it. onClose(true): the email was queued again.
 */
export function SendAgainDialog(props: { api: HomeworkApi; delivery: DeliveryItem; onClose: (sent: boolean) => void }) {
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  const [error, setError] = useState("");
  const running = useRef(false);
  const confirmed = useRef(false);

  // The only call site of sendAgain. One try per dialog, like the send itself.
  async function onConfirmSendAgain() {
    if (running.current || confirmed.current) {
      return;
    }
    running.current = true;
    confirmed.current = true;
    setTried(true);
    setBusy(true);
    setError("");
    try {
      await props.api.sendAgain(props.delivery.id);
      props.onClose(true);
    } catch (err) {
      setError(errorText(err));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  function onCancelSendAgain() {
    if (!running.current) {
      props.onClose(false);
    }
  }

  return (
    <Dialog
      open
      danger
      title="Отправить письмо ещё раз?"
      confirmLabel={busy ? "Отправка…" : "Отправить ещё раз"}
      cancelLabel={tried ? "Закрыть" : "Отмена"}
      confirmDisabled={busy || tried}
      onConfirm={() => void onConfirmSendAgain()}
      onCancel={onCancelSendAgain}
    >
      <p>
        Подтверждения о первом письме нет: возможно, оно уже дошло. Тогда преподаватель получит задание дважды. Сначала
        проверьте папку «Отправленные» в почте.
      </p>
      <dl className="hw-send">
        <dt>Кому</dt>
        <dd>
          <bdi>{props.delivery.recipient}</bdi>
        </dd>
        <dt>Тема</dt>
        <dd>
          <bdi>{props.delivery.subject}</bdi>
        </dd>
      </dl>
      {error && (
        <ResultLine kind="fail" role="alert">
          {error}
        </ResultLine>
      )}
    </Dialog>
  );
}
