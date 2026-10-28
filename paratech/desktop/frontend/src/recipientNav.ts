// Where a refused send leads (TASK-238): `const nav = useRecipientNav(); nav.openTeacher(subjectId, "lecture")`. App
// provides it, like the transcript and the summary; it is a context so the send dialog, the agent's confirm card and
// the work card need no props passed through the panel. Nothing is stored in the route: the request is one-shot.
import { createContext, useContext } from "react";
import type { TeacherRole } from "./bindings/recipient.js";

export interface RecipientNav {
  /** «Указать почту»: the page of the subject with the form of that teacher's role open and focused. */
  openTeacher(subjectId: string, role: TeacherRole): void;
  /** «Открыть задание»: «Задания» on that assignment (where «Изменить» chooses the subject). */
  openAssignment(assignmentId: string): void;
  /** The list of subjects, when a refusal names no subject. */
  openSubjects(): void;
}

const none: RecipientNav = { openTeacher: () => undefined, openAssignment: () => undefined, openSubjects: () => undefined };

export const RecipientNavContext = createContext<RecipientNav>(none);

export function useRecipientNav(): RecipientNav {
  return useContext(RecipientNavContext);
}
