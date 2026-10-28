// Where a refused send leads (TASK-238): `const nav = useRecipientNav(); nav.openTeacher(subjectId, "lecture")`. App
// provides it, like the transcript and the summary; it is a context so the send dialog, the agent's confirm card and
// the work card need no props passed through the panel. Nothing is stored in the route: the request is one-shot.
import { createContext, useContext } from "react";
import type { TeacherRole } from "./bindings/recipient.js";

export interface RecipientNav {
  /** «Указать почту»: the page of the subject with the form of that teacher's role open and focused. */
  openTeacher(subjectId: string, role: TeacherRole): void;
  /** «Задания» on that assignment; edit also opens «Изменить» there (where the subject is chosen), once. */
  openAssignment(assignmentId: string, opts?: { edit?: boolean }): void;
  /** The list of subjects, when a refusal names no subject. */
  openSubjects(): void;
}

/** The window keeps a request to open «Изменить» only while the route still shows that assignment ("" none). */
export function keepAssignmentEdit(editId: string, route: { page: string; assignment: string }): string {
  return editId !== "" && route.page === "homework" && route.assignment === editId ? editId : "";
}

const none: RecipientNav = { openTeacher: () => undefined, openAssignment: () => undefined, openSubjects: () => undefined };

export const RecipientNavContext = createContext<RecipientNav>(none);

export function useRecipientNav(): RecipientNav {
  return useContext(RecipientNavContext);
}
