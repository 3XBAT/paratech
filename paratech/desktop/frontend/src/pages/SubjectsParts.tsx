import { type FormEvent, useState } from "react";
import type { LibraryMaterial, LibrarySubject } from "../bindings/library.js";
import { filesOfFolder, folderCountText, folderTitle, groupBySemester, orderedFolders, sizeText, texts, typeLabel } from "../libraryView.js";
import { Button } from "../ui/Button.js";
import { Card, EmptyState, Field, Input, ResultLine, Row } from "../ui/controls.js";

// Names of subjects, folders and files are typed or imported text: only text nodes here, never markup.

/** One-line form: a name and «Создать». The core checks the name; its refusal comes back as `error`. */
export function NameForm(props: {
  label: string;
  busy: boolean;
  error: string;
  onSubmit(name: string): void;
  onCancel(): void;
}) {
  const [name, setName] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim() !== "" && !props.busy) {
      props.onSubmit(name.trim());
    }
  };
  return (
    <form className="name-form" onSubmit={submit}>
      <Field label={props.label} error={props.error || undefined}>
        {(id) => <Input id={id} value={name} maxLength={200} autoFocus onChange={(e) => setName(e.target.value)} />}
      </Field>
      <div className="acts start">
        <Button type="submit" variant="primary" size="sm" disabled={props.busy || name.trim() === ""}>
          {texts.create}
        </Button>
        <Button size="sm" variant="ghost" disabled={props.busy} onClick={props.onCancel}>
          {texts.cancel}
        </Button>
      </div>
    </form>
  );
}

/** Semesters, each with its subjects. */
export function SubjectList(props: { subjects: readonly LibrarySubject[]; onOpen(id: string): void; onAdd(): void }) {
  if (props.subjects.length === 0) {
    return (
      <EmptyState
        label="Библиотека"
        title={texts.emptyTitle}
        actions={
          <Button variant="primary" icon="plus" onClick={props.onAdd}>
            {texts.addSubject}
          </Button>
        }
      >
        {texts.emptyText}
      </EmptyState>
    );
  }
  return (
    <>
      {groupBySemester(props.subjects).map((g) => (
        <div className="gsec" key={g.code}>
          <h2 className="group-cap">{g.label}</h2>
          <Card className="rows" label={g.label}>
            {g.subjects.map((s) => (
              <Row
                key={s.id}
                title={s.name}
                meta={orderedFolders(s.folders).map(folderTitle).join(" · ")}
                actions={
                  <Button size="sm" onClick={() => props.onOpen(s.id)}>
                    Открыть
                  </Button>
                }
              />
            ))}
          </Card>
        </div>
      ))}
    </>
  );
}

function FileRow(props: { m: LibraryMaterial }) {
  const { m } = props;
  const meta = [typeLabel(m.type), sizeText(m.sizeBytes), m.textNote].filter((x) => x !== "").join(" · ");
  return <Row title={m.name} meta={meta} />;
}

/** The page of a subject: its folders (system ones first), the files of each, «＋ Папка». */
export function SubjectDetail(props: {
  subject: LibrarySubject;
  materials: readonly LibraryMaterial[];
  folderForm: boolean;
  busy: boolean;
  error: string;
  onAddFolder(): void;
  onCreateFolder(name: string): void;
  onCancelFolder(): void;
}) {
  const { subject } = props;
  return (
    <>
      {subject.aliases.length > 0 && (
        <p className="small muted">
          {texts.aliases}: {subject.aliases.join(", ")}
        </p>
      )}
      {orderedFolders(subject.folders).map((f) => {
        const files = filesOfFolder(props.materials, f.id);
        return (
          <div className="gsec" key={f.id}>
            <h2 className="group-cap">
              {folderTitle(f)} <span className="muted">· {folderCountText(files.length)}</span>
            </h2>
            <Card className="rows" label={folderTitle(f)}>
              {files.length === 0 ? <p className="small muted empty-folder">{texts.emptyFolder}</p> : files.map((m) => <FileRow key={m.id} m={m} />)}
            </Card>
          </div>
        );
      })}
      {props.folderForm ? (
        <NameForm label={texts.folderName} busy={props.busy} error={props.error} onSubmit={props.onCreateFolder} onCancel={props.onCancelFolder} />
      ) : (
        <div className="acts start group-acts">
          <Button icon="plus" onClick={props.onAddFolder}>
            {texts.addFolder}
          </Button>
        </div>
      )}
      {!props.folderForm && props.error !== "" && (
        <ResultLine kind="fail" role="alert">
          {props.error}
        </ResultLine>
      )}
    </>
  );
}

/** «Входящие»: files that no subject holds yet (dropped on the window or added by hand). */
export function InboxList(props: { materials: readonly LibraryMaterial[] }) {
  const files = props.materials.filter((m) => !m.removed);
  if (files.length === 0) {
    return null;
  }
  return (
    <div className="gsec">
      <h2 className="group-cap">
        {texts.inbox} <span className="muted">· {folderCountText(files.length)}</span>
      </h2>
      <Card className="rows" label={texts.inbox}>
        {files.map((m) => (
          <FileRow key={m.id} m={m} />
        ))}
      </Card>
      <p className="small muted group-foot">{texts.inboxHint}</p>
    </div>
  );
}
