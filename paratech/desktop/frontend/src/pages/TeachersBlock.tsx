import { type FormEvent, type KeyboardEvent, useEffect, useId } from "react";
import type { LibraryTeachers } from "../bindings/library.js";
import {
  type CopySource,
  canCopy,
  copyLabels,
  copySources,
  noTeachers,
  rowLabels,
  type TeacherForm,
  type TeacherRow,
  type TeacherState,
  teacherRows,
  teacherTexts,
  triggerId,
} from "../teachersView.js";
import { Button } from "../ui/Button.js";
import { Card, Field, Input, ResultLine } from "../ui/controls.js";

// Names and addresses of teachers are typed text: only text nodes (<bdi>) here, never markup, never a mailto link.

/** What the block asks of the page; the page maps it to the TeacherFlow. */
export interface TeacherActions {
  open(role: TeacherRow["role"]): void;
  type(patch: { name?: string; email?: string }): void;
  copy(source: CopySource): void;
  save(): void;
  cancel(): void;
  clear(): void;
}

function RowView(props: { row: TeacherRow; disabled: boolean; onOpen: () => void }) {
  const { row } = props;
  return (
    <div className="lrow" role="listitem">
      <div className="tx">
        <p className="t">{row.label}</p>
        <div className="meta">
          {row.filled ? (
            <>
              <bdi>{row.name}</bdi> · <bdi>{row.email}</bdi>
            </>
          ) : (
            teacherTexts.notSet
          )}
        </div>
      </div>
      <Button id={triggerId(row.role)} size="sm" variant="ghost" aria-label={row.actionLabel} disabled={props.disabled} onClick={props.onOpen}>
        {row.action}
      </Button>
    </div>
  );
}

function FormView(props: {
  form: TeacherForm;
  teachers: LibraryTeachers;
  filled: boolean;
  state: TeacherState;
  on: TeacherActions;
}) {
  const { form, state, on } = props;
  const busy = state.busy !== "";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    on.save();
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && !busy) {
      e.preventDefault();
      e.stopPropagation();
      on.cancel();
    }
  };
  return (
    <div className="lrow teacher-edit" role="listitem">
      <form className="teacher-form" aria-label={rowLabels[form.role]} noValidate onSubmit={submit} onKeyDown={onKey}>
        <p className="t">{rowLabels[form.role]}</p>
        <Field label={teacherTexts.name} error={state.errors.name || undefined}>
          {(id) => (
            <Input
              id={id}
              value={form.name}
              maxLength={200}
              autoFocus
              autoComplete="off"
              disabled={busy}
              aria-invalid={state.errors.name !== ""}
              onChange={(e) => on.type({ name: e.target.value })}
            />
          )}
        </Field>
        <Field label={teacherTexts.email} error={state.errors.email || undefined}>
          {(id) => (
            <Input
              id={id}
              value={form.email}
              maxLength={254}
              inputMode="email"
              autoComplete="off"
              spellCheck={false}
              disabled={busy}
              aria-invalid={state.errors.email !== ""}
              onChange={(e) => on.type({ email: e.target.value })}
            />
          )}
        </Field>
        {copySources(form.role).length > 0 && (
          <div className="acts start" role="group" aria-label={teacherTexts.copyGroup}>
            {copySources(form.role).map((source) => {
              const can = canCopy(source, props.teachers);
              return (
                <Button
                  key={source}
                  size="sm"
                  variant="secondary"
                  disabled={busy || !can}
                  disabledReason={!can ? copyLabels[source].missing : undefined}
                  onClick={() => on.copy(source)}
                >
                  {copyLabels[source].button}
                </Button>
              );
            })}
          </div>
        )}
        <div className="acts start">
          <Button type="submit" size="sm" variant="primary" disabled={busy} busy={state.busy === "save"} busyLabel={teacherTexts.saving}>
            {teacherTexts.save}
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={on.cancel}>
            {teacherTexts.cancel}
          </Button>
          {props.filled && (
            <Button size="sm" variant="ghost" disabled={busy} busy={state.busy === "clear"} busyLabel={teacherTexts.clearing} onClick={on.clear}>
              {teacherTexts.clear}
            </Button>
          )}
        </div>
      </form>
    </div>
  );
}

/**
 * «Преподаватели» of a subject: three rows, «Не указан» for an empty one, a small form in place of the row being
 * edited. Quiet when empty: gray rows and one line of hint. The focus goes into the opened form and back to its button.
 */
export function TeachersBlock(props: { teachers: LibraryTeachers; state: TeacherState; on: TeacherActions }) {
  const { state, on } = props;
  const headId = useId();
  const rows = teacherRows(props.teachers);
  const busy = state.busy !== "";

  const back = state.focusRole;
  useEffect(() => {
    if (back !== "") {
      document.getElementById(triggerId(back))?.focus();
    }
  }, [back]);

  return (
    <section className="section teachers" aria-labelledby={headId}>
      <h2 className="sec-h" id={headId}>
        {teacherTexts.title}
      </h2>
      <Card className="lrows" label={teacherTexts.title}>
        <div role="list">
          {rows.map((row) =>
            state.form !== null && state.form.role === row.role ? (
              <FormView key={row.role} form={state.form} teachers={props.teachers} filled={row.filled} state={state} on={on} />
            ) : (
              <RowView key={row.role} row={row} disabled={busy} onOpen={() => on.open(row.role)} />
            ),
          )}
        </div>
      </Card>
      {state.form === null && noTeachers(props.teachers) && <p className="small muted group-foot">{teacherTexts.hint}</p>}
      {state.errors.general !== "" && (
        <ResultLine kind="fail" role="alert">
          {state.errors.general}
        </ResultLine>
      )}
      <p className="small muted group-foot teachers-note" role="status">
        {state.note}
      </p>
    </section>
  );
}
