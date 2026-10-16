import { useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled dialogs (test/homework.test.mjs renders them).
import type { DeliveryItem, HomeworkApi, SendPreparation } from "../bindings/homework.js";
import { errorText, sizeText } from "../homeworkView.js";
import { Dialog, ResultLine } from "../ui/controls.js";

// The two dialogs that lead to an email. Each one has a single call site of its binding, in its confirm button handler
// (test/homework.test.mjs checks it): an email goes out only after the student pressed that button (AC-14, AC-15).
// Recipient, subject and file name come from the core's trusted state and are shown as plain text.

/** How the send dialog ended: the email was created, the student cancelled, or the confirmation failed. */
export type SendResult = "sent" | "cancelled" | "failed";

/**
 * Asks to send the prepared homework: recipient, subject and attachment, «Отправить» and «Отмена». Any way of closing
 * it other than «Отправить» cancels the send in the core.
 */
export function SendDialog(props: { api: HomeworkApi; prep: SendPreparation; onClose: (result: SendResult) => void }) {
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  const [error, setError] = useState("");
  // Synchronous guards: a double click comes before the re-render that disables the button.
  const running = useRef(false);
  const confirmed = useRef(false);

  // The only call site of confirmSend (AC-14). One try per dialog: after a failure the send is prepared again.
  async function onConfirmSend() {
    if (running.current || confirmed.current) {
      return;
    }
    running.current = true;
    confirmed.current = true;
    setTried(true);
    setBusy(true);
    setError("");
    try {
      await props.api.confirmSend(props.prep.proposalId);
      props.onClose("sent");
    } catch (err) {
      setError(errorText(err));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  async function onCancelSend() {
    if (running.current) {
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

  return (
    <Dialog
      open
      title="Отправить домашнее задание?"
      confirmLabel={busy ? "Отправка…" : "Отправить"}
      cancelLabel={tried ? "Закрыть" : "Отмена"}
      confirmDisabled={busy || tried}
      onConfirm={() => void onConfirmSend()}
      onCancel={() => void onCancelSend()}
    >
      <dl className="hw-send">
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
