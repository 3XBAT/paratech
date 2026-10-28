// The recipient of «Отправить преподавателю» (ADR-0030, TASK-237) as the Go core gives it in the send dialog
// (homework.ts) and the agent's confirm card (agentConfirm.ts); see internal/input/wails/homeworkrecipient.go.
// Names and addresses are stored or imported text: show them as text nodes only. Parsing is defensive: an unknown
// role or reason is the empty one and a malformed option is dropped, so nothing unexpected reaches the page.

/** The teacher's role: "lecture", "practice" (семинарист) or "lab". */
export type TeacherRole = "lecture" | "practice" | "lab";

export const teacherRoles: readonly TeacherRole[] = ["lecture", "practice", "lab"];

/**
 * "choose_teacher": the page asks «Кому отправить?» and keeps «Да» disabled until a click; "no_teacher_email" and
 * "no_subject": the send is refused, the message says why; "": nothing to ask.
 */
export type RecipientReason = "" | "choose_teacher" | "no_teacher_email" | "no_subject";

export const recipientReasons: readonly RecipientReason[] = ["", "choose_teacher", "no_teacher_email", "no_subject"];

/** One teacher of «Кому отправить?»: one per address, in role order. */
export interface TeacherOption {
  role: TeacherRole;
  /** "" when the name is unknown. */
  name: string;
  email: string;
}

/** The option the student clicked, as the confirmation sends it back. */
export interface TeacherChoice {
  role: TeacherRole;
  /** The address the option showed. */
  address: string;
}

/** The fields of the recipient both replies carry (recipient itself stays beside the other fields). */
export interface RecipientFields {
  /** The teacher's name for «Кому: <имя> <адрес>»; "" when unknown. */
  recipientName: string;
  /** "" when no role applies. */
  role: TeacherRole | "";
  /** The library subject the teachers come from; "" none. */
  subjectId: string;
  /** The teachers to choose from (reason "choose_teacher"); empty otherwise. */
  options: TeacherOption[];
}

function known<T extends string>(v: unknown, allowed: readonly T[], fallback: T): T {
  return typeof v === "string" && allowed.includes(v as T) ? (v as T) : fallback;
}

/** The role when it is a known one, else "". */
export function teacherRole(v: unknown): TeacherRole | "" {
  return known<TeacherRole | "">(v, teacherRoles, "");
}

/** The recipient reason when it is a known one, else "". */
export function recipientReason(v: unknown): RecipientReason {
  return known(v, recipientReasons, "");
}

/** The well-formed options of a reply (null or missing: none); a malformed entry is dropped. */
export function parseTeacherOptions(raw: unknown): TeacherOption[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const out: TeacherOption[] = [];
  for (const item of raw) {
    if (typeof item !== "object" || item === null) {
      continue;
    }
    const o = item as Record<string, unknown>;
    const role = teacherRole(o.role);
    if (role === "" || typeof o.name !== "string" || typeof o.email !== "string" || o.email === "") {
      continue;
    }
    out.push({ role, name: o.name, email: o.email });
  }
  return out;
}

/** The recipient fields of a reply; a field that is missing or of another type is the empty one. */
export function parseRecipientFields(r: Record<string, unknown>): RecipientFields {
  return {
    recipientName: typeof r.recipientName === "string" ? r.recipientName : "",
    role: teacherRole(r.role),
    subjectId: typeof r.subjectId === "string" ? r.subjectId : "",
    options: parseTeacherOptions(r.options),
  };
}

/** The role and address the confirmation sends: "" and "" when nothing was asked. */
export function choiceArgs(chosen?: TeacherChoice | null): [string, string] {
  return chosen === undefined || chosen === null ? ["", ""] : [chosen.role, chosen.address];
}
