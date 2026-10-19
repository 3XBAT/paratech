import { useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled dialogs (test/homework.test.mjs renders them).
import { type DeliveryItem, type HomeworkApi, type SendFormat, sendFormats, type SendPreparation } from "../bindings/homework.js";
import { errorText, sendFormatLabels, sendRetryable, sizeText } from "../homeworkView.js";
import { Dialog, ResultLine } from "../ui/controls.js";

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
        {sendFormats.map((f) => (
          <label key={f}>
            <input
              type="radio"
              name="hw-send-format"
              value={f}
              checked={props.prep.attachmentFormat === f}
              onChange={() => props.onFormat?.(f)}
            />
            {sendFormatLabels[f]}
          </label>
        ))}
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
