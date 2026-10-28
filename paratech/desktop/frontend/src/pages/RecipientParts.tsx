import { useId } from "react";
import type { TeacherRole } from "../bindings/recipient.js";
import type { RecipientNav } from "../recipientNav.js";
import { type ChoiceOption, type RefusalAction, recipientTexts } from "../recipientView.js";
import { Button } from "../ui/Button.js";

// Names, addresses and the core's messages are stored or typed text: only text nodes (<bdi>) here, never markup and
// never a mailto link. Nothing here sends: the confirmation stays with the dialog's and the card's own button.

/** «<имя> <адрес>» as two text nodes; the address alone when the name is unknown. */
export function RecipientName(props: { name: string; address: string }) {
  return (
    <>
      {props.name !== "" && (
        <>
          <bdi>{props.name}</bdi>{" "}
        </>
      )}
      <bdi>{props.address}</bdi>
    </>
  );
}

/**
 * «Кому отправить?»: one option per teacher, a native radio group (arrow keys move and choose, Tab enters and leaves).
 * Nothing is chosen at first, so the confirmation stays off until a click.
 */
export function TeacherChoiceGroup(props: {
  options: readonly ChoiceOption[];
  value: TeacherRole | "";
  disabled: boolean;
  onChange: (role: TeacherRole) => void;
}) {
  const legendId = useId();
  const name = useId();
  return (
    <fieldset className="rcp-choose" role="radiogroup" aria-labelledby={legendId} disabled={props.disabled}>
      <legend id={legendId}>{recipientTexts.chooseTitle}</legend>
      {props.options.map((o) => (
        <label key={o.role} className="ag-opt">
          <input type="radio" name={name} value={o.role} checked={props.value === o.role} onChange={() => props.onChange(o.role)} />
          <span>
            {o.title} — <RecipientName name={o.name} address={o.address} />
          </span>
        </label>
      ))}
    </fieldset>
  );
}

/** The one button that leads out of a refusal; what it does is up to the caller for choosing the subject in place. */
export function RefusalButton(props: {
  action: RefusalAction;
  nav: RecipientNav;
  onChooseSubject?: () => void;
  disabled?: boolean;
}) {
  const { action, nav } = props;
  const click = () => {
    switch (action.kind) {
      case "teacher":
        nav.openTeacher(action.subjectId, action.role);
        break;
      case "assignment":
        nav.openAssignment(action.assignmentId);
        break;
      case "subjects":
        nav.openSubjects();
        break;
      case "choose_subject":
        props.onChooseSubject?.();
        break;
    }
  };
  return (
    <Button size="sm" variant="primary" disabled={props.disabled} onClick={click}>
      {action.label}
    </Button>
  );
}
