// «Профиль студента» in the settings (TASK-094): labels, the change check and error texts only. The core trims and
// checks the fields (ARCH-44); the page sends what the student typed.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { errorMessages, profileFields, type StudentProfile } from "./bindings/settings.js";

/** The longest field the core keeps (settings.MaxProfileRunes). */
export const maxProfileLength = 200;

export const profileLabels: Readonly<Record<keyof StudentProfile, string>> = {
  fullName: "ФИО",
  group: "Группа",
  university: "Вуз",
  faculty: "Факультет или кафедра",
  city: "Город",
};

export const profilePlaceholders: Readonly<Record<keyof StudentProfile, string>> = {
  fullName: "Иванов Иван Иванович",
  group: "ИВТ-21",
  university: "Название вуза",
  faculty: "Факультет информатики",
  city: "Москва",
};

export const emptyProfile: StudentProfile = { fullName: "", group: "", university: "", faculty: "", city: "" };

/** Whether the form differs from what the core stored: «Сохранить» is offered only then. */
export function profileChanged(form: StudentProfile, stored: StudentProfile): boolean {
  return profileFields.some((f) => form[f] !== stored[f]);
}

const saveFailed = "Не удалось сохранить профиль.";

/** The core's text of a refused field as it is; anything else gets a generic text. */
export function profileErrorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return msg === errorMessages.profileInvalid ? msg : saveFailed;
}
