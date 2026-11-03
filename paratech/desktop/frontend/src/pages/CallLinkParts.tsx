import { type FormEvent, type KeyboardEvent, useEffect } from "react";
import type { LibrarySubject } from "../bindings/library.js";
import type { TodayLesson } from "../bindings/today.js";
import {
  type CallLinkFlow,
  type CardLink,
  callLinkTexts,
  cardLink,
  explainText,
  hasNoField,
  joinTitle,
  type LinkForm,
  type LinkLine,
  type LinkState,
  type LinkTarget,
  linkId,
  linkKey,
  pairLabel,
  saveLabel,
} from "../callLinkView.js";
import { Button } from "../ui/Button.js";
import { Card, Input, ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";

// The link to the online class (TASK-364): the card of a pair on «Сегодня» and the block on the page of a subject. The
// pages own the flow; here only what it reports is drawn. The link and the host are typed text: text nodes only, never a
// link element (the app opens a link through the core, by the subject ID).

/** What the controls ask of the flow; the page maps it. */
export interface LinkActions {
  openForm(key: string, mode: "add" | "change", text?: string): void;
  type(text: string): void;
  cancel(): void;
  save(target: LinkTarget): void;
  join(target: LinkTarget): void;
  clear(target: LinkTarget, link: { link: string; host: string }): void;
  restore(): void;
}

export interface LinkControls {
  state: LinkState;
  on: LinkActions;
  /** «Открыть «Предметы»»: the way out of a refusal that only the subject's page can fix. */
  onOpenSubjects: () => void;
}

export function linkActions(flow: CallLinkFlow): LinkActions {
  return {
    openForm: (key, mode, text) => flow.openForm(key, mode, text),
    type: (text) => flow.type(text),
    cancel: () => flow.cancel(),
    save: (target) => void flow.save(target),
    join: (target) => void flow.join(target),
    clear: (target, link) => void flow.clear(target, link),
    restore: () => void flow.restore(),
  };
}

/** The element the flow asked to focus (after an open, a save, a refusal); `n` changes on every request. */
export function useLinkFocus(focus: LinkState["focus"] | undefined): void {
  const id = focus?.id ?? "";
  const n = focus?.n ?? 0;
  useEffect(() => {
    if (n > 0 && id !== "") {
      document.getElementById(id)?.focus();
    }
  }, [id, n]);
}

function Line(props: { line: LinkLine; links: LinkControls; onRetry?: () => void; undo?: boolean }) {
  const { line, links } = props;
  const busy = links.state.busy !== "";
  return (
    <div className="calllink-line">
      {line.kind === "ok" ? (
        <p className="small muted" role="status">
          {line.text}
        </p>
      ) : (
        <ResultLine kind="fail" role="alert">
          {line.text}
        </ResultLine>
      )}
      {line.action === "retry" && props.onRetry !== undefined && (
        <Button size="sm" disabled={busy} onClick={props.onRetry}>
          {callLinkTexts.retry}
        </Button>
      )}
      {line.action === "subjects" && (
        <Button size="sm" onClick={links.onOpenSubjects}>
          {callLinkTexts.openSubjects}
        </Button>
      )}
      {props.undo === true && (
        <Button
          id={linkId(line.key, "undo")}
          size="sm"
          variant="ghost"
          disabled={busy}
          busy={links.state.busy === "restore"}
          busyLabel={callLinkTexts.restoring}
          onClick={links.on.restore}
        >
          {callLinkTexts.undo}
        </Button>
      )}
    </div>
  );
}

/**
 * The small form: the field (Enter saves, Escape cancels), «Сохранить», «Отмена»; above it the explanation when the pair has
 * no subject to keep the link. A pair that names no subject at all has no field: the way to «Предметы» instead.
 */
function Form(props: { form: LinkForm; card: CardLink; target: LinkTarget; links: LinkControls; labelled: boolean }) {
  const { form, card, target, links } = props;
  const { state, on } = links;
  const busy = state.busy !== "";
  const explain = explainText(card);
  const fieldId = linkId(form.key, "field");
  const errorId = `${fieldId}-error`;
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && !busy) {
      e.preventDefault();
      e.stopPropagation();
      on.cancel();
    }
  };
  if (hasNoField(card)) {
    return (
      <div className="calllink-form" role="group" aria-label={callLinkTexts.field} onKeyDown={onKey}>
        <p className="small muted">{explain}</p>
        <div className="acts start">
          <Button id={fieldId} size="sm" onClick={links.onOpenSubjects}>
            {callLinkTexts.openSubjects}
          </Button>
          <Button size="sm" variant="ghost" onClick={on.cancel}>
            {callLinkTexts.close}
          </Button>
        </div>
      </div>
    );
  }
  const submit = (e: FormEvent) => {
    e.preventDefault();
    on.save(target);
  };
  return (
    <form className="calllink-form" aria-label={callLinkTexts.field} noValidate onSubmit={submit} onKeyDown={onKey}>
      {explain !== "" && <p className="small muted">{explain}</p>}
      {props.labelled && <label htmlFor={fieldId}>{callLinkTexts.field}</label>}
      <div className="calllink-fieldrow">
        <Input
          id={fieldId}
          value={form.text}
          maxLength={2048}
          placeholder={callLinkTexts.placeholder}
          aria-label={props.labelled ? undefined : callLinkTexts.field}
          aria-invalid={state.fieldError !== ""}
          aria-describedby={state.fieldError !== "" ? errorId : undefined}
          inputMode="url"
          autoComplete="off"
          spellCheck={false}
          disabled={busy}
          onChange={(e) => on.type(e.target.value)}
        />
        <Button type="submit" size="sm" disabled={busy} busy={state.busy === "save"} busyLabel={callLinkTexts.saving}>
          {saveLabel(card)}
        </Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={on.cancel}>
          {callLinkTexts.cancel}
        </Button>
      </div>
      {state.fieldError !== "" && (
        <span className="field-error small" id={errorId} role="alert">
          <Icon name="warn" />
          {state.fieldError}
        </span>
      )}
    </form>
  );
}

// ---- «Сегодня» ----

function targetOf(l: TodayLesson, card: CardLink): LinkTarget {
  return { key: linkKey(l), subjectId: l.subjectId, name: card.kind === "create" ? card.name : "" };
}

/**
 * Next to «Записать»: «Подключиться» opens the link (it does not record and does not leave the page); without one a quiet
 * «Добавить ссылку» opens the field. Available for every pair of the day, whatever the time.
 */
export function LessonLinkButtons(props: { lesson: TodayLesson; title: string; links: LinkControls }) {
  const { lesson, title, links } = props;
  const card = cardLink(lesson);
  const key = linkKey(lesson);
  const { state, on } = links;
  const busy = state.busy !== "";
  if (card.kind === "join") {
    return (
      <Button
        id={linkId(key, "join")}
        size="sm"
        busy={state.busy === "open" && state.busyKey === key}
        busyLabel={callLinkTexts.joining}
        disabled={busy}
        title={joinTitle(card.host) || undefined}
        aria-label={pairLabel(callLinkTexts.join, title)}
        onClick={() => on.join(targetOf(lesson, card))}
      >
        {callLinkTexts.join}
      </Button>
    );
  }
  // While the field of this pair is open it has its own buttons.
  if (state.form?.key === key) {
    return null;
  }
  return (
    <Button
      id={linkId(key, "add")}
      size="sm"
      variant="ghost"
      disabled={busy}
      aria-label={pairLabel(callLinkTexts.add, title)}
      onClick={() => on.openForm(key, "add")}
    >
      {callLinkTexts.add}
    </Button>
  );
}

/** Under the title: the host of the link (never the link) and «Изменить». Nothing for a pair without a link. */
export function LessonLinkHost(props: { lesson: TodayLesson; title: string; links: LinkControls }) {
  const { lesson, title, links } = props;
  const card = cardLink(lesson);
  const key = linkKey(lesson);
  if (card.kind !== "join") {
    return null;
  }
  return (
    <div className="small muted today-host">
      {card.host !== "" && <bdi>{card.host}</bdi>}
      {links.state.form?.key !== key && (
        <Button
          id={linkId(key, "change")}
          size="sm"
          variant="ghost"
          disabled={links.state.busy !== ""}
          aria-label={pairLabel(callLinkTexts.change, title)}
          onClick={() => links.on.openForm(key, "change")}
        >
          {callLinkTexts.change}
        </Button>
      )}
    </div>
  );
}

/** Under the row: the open field and/or the result of the last call for this pair. */
export function LessonLinkPanel(props: { lesson: TodayLesson; links: LinkControls }) {
  const { lesson, links } = props;
  const card = cardLink(lesson);
  const key = linkKey(lesson);
  const form = links.state.form?.key === key ? links.state.form : null;
  const line = links.state.line?.key === key ? links.state.line : null;
  if (form === null && line === null) {
    return null;
  }
  return (
    <div className="today-link">
      {form !== null && <Form form={form} card={card} target={targetOf(lesson, card)} links={links} labelled={false} />}
      {line !== null && <Line line={line} links={links} onRetry={() => links.on.join(targetOf(lesson, card))} />}
    </div>
  );
}

// ---- The page of a subject ----

/**
 * «Ссылка на пару» on the page of a subject, next to the teachers: the host (the full link as selectable text), «Подключиться»,
 * «Изменить», «Удалить» with «Вернуть»; without a link «Ссылки на пару нет» and «Добавить ссылку».
 */
export function CallLinkBlock(props: { subject: LibrarySubject; links: LinkControls }) {
  const { subject, links } = props;
  const { state, on } = links;
  const key = subject.id;
  const link = subject.callLink;
  const busy = state.busy !== "";
  const target: LinkTarget = { key, subjectId: subject.id, name: "" };
  const form = state.form?.key === key ? state.form : null;
  const line = state.line?.key === key ? state.line : null;
  const undo = line !== null && state.removed?.key === key && link === null;
  useLinkFocus(state.focus);
  return (
    <section className="section calllink" aria-label={callLinkTexts.field}>
      <Card className="lrows">
        {form !== null ? (
          <div className="lrow teacher-edit">
            <Form form={form} card={link === null ? { kind: "add" } : { kind: "join", host: link.host }} target={target} links={links} labelled />
          </div>
        ) : (
          <div className="lrow">
            <div className="tx">
              <p className="t">{callLinkTexts.field}</p>
              <div className="meta">{link === null ? callLinkTexts.none : <bdi>{link.host}</bdi>}</div>
              {link !== null && (
                <div className="meta calllink-full" title={link.link}>
                  <bdi>{link.link}</bdi>
                </div>
              )}
            </div>
            {link === null ? (
              <Button id={linkId(key, "add")} size="sm" variant="ghost" disabled={busy} onClick={() => on.openForm(key, "add")}>
                {callLinkTexts.add}
              </Button>
            ) : (
              <>
                <Button
                  id={linkId(key, "join")}
                  size="sm"
                  busy={state.busy === "open" && state.busyKey === key}
                  busyLabel={callLinkTexts.joining}
                  disabled={busy}
                  onClick={() => on.join(target)}
                >
                  {callLinkTexts.join}
                </Button>
                <Button id={linkId(key, "change")} size="sm" variant="ghost" disabled={busy} onClick={() => on.openForm(key, "change", link.link)}>
                  {callLinkTexts.change}
                </Button>
                <Button
                  id={linkId(key, "clear")}
                  size="sm"
                  variant="ghost"
                  disabled={busy}
                  busy={state.busy === "clear" && state.busyKey === key}
                  busyLabel={callLinkTexts.clearing}
                  onClick={() => on.clear(target, link)}
                >
                  {callLinkTexts.clear}
                </Button>
              </>
            )}
          </div>
        )}
      </Card>
      {line !== null && <Line line={line} links={links} undo={undo} onRetry={() => on.join(target)} />}
    </section>
  );
}
