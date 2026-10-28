// View logic of the recipient of «Отправить преподавателю» (ADR-0030, TASK-238): «Кому: <имя> <адрес>», the question
// «Кому отправить?» and the two refusals (no address, no subject). Pure, no React. Names, addresses and the core's
// message are stored or typed text: they are passed through untouched and rendered as text nodes only. The choice is
// the student's click; the role and the address of the option go back with the confirmation and nowhere else.

import {
  recipientReason,
  type TeacherChoice,
  type TeacherOption,
  type TeacherRole,
  teacherRole,
} from "./bindings/recipient.js";

/** The role in «Нет почты преподавателя (<роль>)». */
export const roleWords: Readonly<Record<TeacherRole, string>> = { lecture: "лектор", practice: "семинарист", lab: "лабораторные" };

/** The role at the start of an option of «Кому отправить?». */
export const roleTitles: Readonly<Record<TeacherRole, string>> = { lecture: "Лектор", practice: "Семинарист", lab: "Лабораторные" };

export const recipientTexts = {
  to: "Кому",
  chooseTitle: "Кому отправить?",
  chooseNeeded: "Выберите, кому отправить.",
  noTeachersLeft: "У предмета не осталось почты преподавателей: укажите её на странице предмета.",
  chooseHintDialog: "Кнопка «Отправить» станет доступна после выбора.",
  specifyEmail: "Указать почту",
  noSubject: "У задания не указан предмет: выберите предмет, и письмо уйдёт его преподавателю.",
  chooseSubject: "Выбрать предмет",
  openSubjects: "Открыть предметы",
  noEmailBlocked: "Отправить нельзя: нет почты преподавателя.",
  noSubjectBlocked: "Отправить нельзя: у задания не указан предмет.",
} as const;

/** One teacher of «Кому отправить?» as the page shows it. */
export interface ChoiceOption {
  role: TeacherRole;
  /** «Лектор», «Семинарист», «Лабораторные». */
  title: string;
  /** "" when the name is unknown. */
  name: string;
  address: string;
}

/** The options of a reply, in the order the core sent them. */
export function choiceOptions(options: readonly TeacherOption[] | undefined): ChoiceOption[] {
  return (options ?? []).map((o) => ({ role: o.role, title: roleTitles[o.role], name: o.name, address: o.email }));
}

/** «Лектор — Иванов И. И. ivanov@…»; just the address after the dash when the name is unknown. */
export function choiceText(o: ChoiceOption): string {
  return `${o.title} — ${o.name === "" ? o.address : `${o.name} ${o.address}`}`;
}

/** «<имя> <адрес>», or the address alone when the name is unknown. */
export function recipientText(name: string, address: string): string {
  return name === "" ? address : `${name} ${address}`;
}

/** What goes back to the core with «Да»: the role of the clicked option and the address that option shows. */
export function choiceOf(options: readonly TeacherOption[] | undefined, role: TeacherRole | ""): TeacherChoice | null {
  const o = role === "" ? undefined : (options ?? []).find((x) => x.role === role);
  return o === undefined ? null : { role: o.role, address: o.email };
}

/** A click on an option belongs to the preparation it was made on: another preparation (proposalId) starts unchosen. */
export function pickedRole(picked: { proposalId: string; role: TeacherRole } | null, proposalId: string): TeacherRole | "" {
  return picked !== null && picked.proposalId === proposalId ? picked.role : "";
}

/** The fields of a send preparation or a send card that name its recipient (all optional: older replies lack some). */
export interface RecipientSource {
  reason?: string;
  recipient?: string;
  recipientName?: string;
  role?: string;
  subjectId?: string;
  options?: readonly TeacherOption[];
  message?: string;
  assignmentId?: string;
}

export type RecipientView =
  /** «Кому: <имя> <адрес>». */
  | { kind: "to"; name: string; address: string }
  /** «Кому отправить?»: one to three options (the teachers may have changed since the question was asked), one is clicked. */
  | { kind: "choose"; options: ChoiceOption[] }
  /** No address for the role (or none left for the question): «Указать почту» opens that role's form on the subject page. */
  | { kind: "no_email"; text: string; subjectId: string; role: TeacherRole | "" }
  /** The assignment has no subject: the subject is chosen on the assignment. */
  | { kind: "no_subject"; text: string; assignmentId: string }
  | { kind: "none" };

/** «Нет почты преподавателя (лектор) предмета «Физика»: укажите её на странице предмета.» */
export function noEmailText(role: TeacherRole | "", subjectName = ""): string {
  const who = role === "" ? "" : ` (${roleWords[role]})`;
  const of = subjectName === "" ? "" : ` предмета «${subjectName}»`;
  return `Нет почты преподавателя${who}${of}: укажите её на странице предмета.`;
}

/**
 * What the recipient part of a card shows. The core's message is the text of a refusal (it names the subject); an empty
 * one gets a text built here. asking is false once the card no longer waits for «Да»: a question is not shown then.
 */
export function recipientView(c: RecipientSource, asking = true): RecipientView {
  const reason = recipientReason(c.reason);
  const message = (c.message ?? "").trim();
  if (reason === "choose_teacher" && asking) {
    const options = choiceOptions(c.options);
    // The core accepts any teacher it still has an address for: even one option is a click, never a UI-made dead end.
    // Nothing left is a refusal with the way out of «Указать почту».
    return options.length >= 1
      ? { kind: "choose", options }
      : { kind: "no_email", text: recipientTexts.noTeachersLeft, subjectId: c.subjectId ?? "", role: "" };
  }
  if (reason === "no_teacher_email") {
    const role = teacherRole(c.role);
    return { kind: "no_email", text: message !== "" ? message : noEmailText(role), subjectId: c.subjectId ?? "", role };
  }
  if (reason === "no_subject") {
    return { kind: "no_subject", text: message !== "" ? message : recipientTexts.noSubject, assignmentId: c.assignmentId ?? "" };
  }
  const address = c.recipient ?? "";
  return address === "" ? { kind: "none" } : { kind: "to", name: c.recipientName ?? "", address };
}

/** Whether the view is a refusal that cannot be confirmed. */
export function isRefusal(v: RecipientView): v is Extract<RecipientView, { kind: "no_email" | "no_subject" }> {
  return v.kind === "no_email" || v.kind === "no_subject";
}

/**
 * Why «Да» / «Отправить» is off, "" when it may be pressed. A question needs a click on one of its options; a
 * refusal and a question without choices cannot be confirmed at all.
 */
export function sendBlocked(v: RecipientView, chosen: TeacherChoice | null): string {
  switch (v.kind) {
    case "choose":
      return chosen === null ? recipientTexts.chooseNeeded : "";
    case "no_email":
      return recipientTexts.noEmailBlocked;
    case "no_subject":
      return recipientTexts.noSubjectBlocked;
    default:
      return "";
  }
}

/** The way out of a refusal: one button. */
export type RefusalAction =
  | { kind: "teacher"; label: string; subjectId: string; role: TeacherRole }
  | { kind: "choose_subject"; label: string }
  | { kind: "assignment"; label: string; assignmentId: string; edit: true }
  | { kind: "subjects"; label: string };

/**
 * The button of a refusal. "page" is the assignment page, where the subject is chosen in place («Изменить»); "card" is
 * the agent's card, which leads to the assignment with «Изменить» already open (or to the subjects when it names none).
 */
export function refusalAction(v: Extract<RecipientView, { kind: "no_email" | "no_subject" }>, where: "page" | "card"): RefusalAction {
  if (v.kind === "no_email") {
    const req = teacherRequest(v);
    return req === null
      ? { kind: "subjects", label: recipientTexts.openSubjects }
      : { kind: "teacher", label: recipientTexts.specifyEmail, ...req };
  }
  if (where === "page") {
    return { kind: "choose_subject", label: recipientTexts.chooseSubject };
  }
  return v.assignmentId === ""
    ? { kind: "subjects", label: recipientTexts.openSubjects }
    : { kind: "assignment", label: recipientTexts.chooseSubject, assignmentId: v.assignmentId, edit: true };
}

/** What «Указать почту» asks of the window: the role to open, «Лектор» when the core named none. */
export function teacherRequest(v: Extract<RecipientView, { kind: "no_email" }>): { subjectId: string; role: TeacherRole } | null {
  return v.subjectId === "" ? null : { subjectId: v.subjectId, role: v.role === "" ? "lecture" : v.role };
}
