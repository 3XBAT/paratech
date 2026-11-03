// Which subjects of the «Предметы» tree show their folders. Pure, no React.
//
// Each subject has its own toggle: opening a subject on the right (selecting it) neither expands nor collapses anything,
// and the selected subject can be collapsed like any other, so all of them can be closed at once. Before this (TASK-341)
// only the selected subject was ever expanded, which made "collapse all" impossible.

export interface SubjectsOpen {
  /** Ids of the subjects whose folders are shown. */
  readonly open: ReadonlySet<string>;
  /** The first selected subject has been expanded already; later selections never expand anything. */
  readonly seeded: boolean;
}

/** On arrival the subject that is on screen shows its folders, once. */
export function initialOpen(currentId: string): SubjectsOpen {
  return currentId === "" ? { open: new Set(), seeded: false } : { open: new Set([currentId]), seeded: true };
}

/** The subject on screen is not known at the first render (it is read after the list): it is expanded when it first appears. */
export function seedOpen(state: SubjectsOpen, currentId: string): SubjectsOpen {
  if (state.seeded || currentId === "") {
    return state;
  }
  return { open: new Set(state.open).add(currentId), seeded: true };
}

/** The toggle of one subject; the others stay as they are. */
export function toggleSubject(state: SubjectsOpen, id: string): SubjectsOpen {
  const open = new Set(state.open);
  if (!open.delete(id)) {
    open.add(id);
  }
  return { open, seeded: true };
}

/** The name of a subject's toggle (the chevron has no text). */
export function toggleLabel(name: string, open: boolean): string {
  return `${open ? "Свернуть" : "Развернуть"}: ${name}`;
}

export function isSubjectOpen(state: SubjectsOpen, id: string): boolean {
  return state.open.has(id);
}
