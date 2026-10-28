import { type FormEvent, useState } from "react";
import { texts } from "../libraryView.js";
import { Button } from "../ui/Button.js";
import { Field, Input } from "../ui/controls.js";

// Names of subjects and folders are typed text: only text nodes here, never markup.

/** One-line form: a name and «Создать». The core checks the name; its refusal comes back as `error`. */
export function NameForm(props: {
  label: string;
  busy: boolean;
  error: string;
  onSubmit(name: string): void;
  onCancel(): void;
  /** False inside another panel, where the page already has its one accent button. */
  primary?: boolean;
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
        <Button type="submit" variant={props.primary === false ? "secondary" : "primary"} size="sm" disabled={props.busy || name.trim() === ""}>
          {texts.create}
        </Button>
        <Button size="sm" variant="ghost" disabled={props.busy} onClick={props.onCancel}>
          {texts.cancel}
        </Button>
      </div>
    </form>
  );
}
