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
import {
  type AddFieldErrors,
  type AddForm,
  addSession,
  addTexts,
  emptyAddForm,
  errorText,
  noteMax,
  sendFormatLabels,
  sendRetryable,
  sizeText,
  titleMax,
  validateAddForm,
} from "../homeworkView.js";
import { Dialog, Field, Input, ResultLine, Segmented, Select } from "../ui/controls.js";

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
  onClose: (result: SendResult) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  const [retry, setRetry] = useState(false);
  const [error, setError] = useState("");
  // Synchronous guards: a double click comes before the re-render that disables the button.
  const running = useRef(false);
  const confirmed = useRef(false);
  const retryable = useRef(false);

  // The only call site of confirmSend (AC-14). One try per dialog, except after sendIncomplete: the core says
  // repeating the same proposal sends no second email, so «Повторить» calls it again with the same proposalId.
  async function onConfirmSend() {
    if (running.current || (confirmed.current && !retryable.current)) {
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
      await props.api.confirmSend(props.prep.proposalId, props.prep.subject, props.prep.attachmentFormat);
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
      confirmDisabled={busy || switching || (tried && !retry)}
      onConfirm={() => void onConfirmSend()}
      onCancel={() => void onCancelSend()}
    >
      <dl className="hw-send">
        <dt>Черновик</dt>
        <dd className="num">Версия {props.version}</dd>
        <dt>Кому</dt>
        <dd>
          <bdi>{props.prep.recipient}</bdi>
        </dd>
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
      <div className="hw-add">
        <Field label={addTexts.what} error={errors.title}>
          {(id) => (
            <Input
              id={id}
              value={form.title}
              maxLength={titleMax}
              disabled={busy}
              aria-invalid={errors.title !== undefined}
              onChange={(e) => change({ title: e.target.value }, "title")}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  void submit();
                }
              }}
            />
          )}
        </Field>
        <Field label={addTexts.subject} hint={subjectsFailed ? addTexts.subjectsFailed : undefined}>
          {(id) => (
            <Select id={id} value={form.subjectId} disabled={busy} onChange={(e) => change({ subjectId: e.target.value })}>
              <option value="">{addTexts.noSubject}</option>
              {subjects.map((s) => (
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
                onChange={(e) => change({ date: e.target.value }, "due")}
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
                onChange={(e) => change({ time: e.target.value }, "time")}
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
              onChange={(e) => change({ note: e.target.value }, "note")}
            />
          )}
        </Field>
        {general && (
          <ResultLine kind="fail" role="alert">
            {general}
          </ResultLine>
        )}
      </div>
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
