import { type DragEvent, type ReactNode, useState } from "react";
import type { LibraryMaterial, LibrarySubject, SubjectOverview } from "../bindings/library.js";
import {
  addToFolderLabel,
  dropCue,
  folderAnchor,
  inboxAnchor,
  isMaterialDrag,
  materialDragType,
  readMaterialDrop,
  scrollToFolder as scrollToFolderIn,
} from "../libraryInboxView.js";
import {
  dayMonth,
  daysChip,
  type FreshMarks,
  filesOfFolder,
  folderCountText,
  folderTitle,
  groupBySemester,
  lessonChip,
  lessonMeta,
  lessonTitle,
  noFresh,
  orderedFolders,
  texts,
  treeCount,
  weekdayOf,
} from "../libraryView.js";
import { offerGroups, reviewTexts } from "../scheduleSubjectsView.js";
import { Button, IconButton } from "../ui/Button.js";
import { Card, EmptyState, ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { StatusChip } from "../ui/StatusChip.js";
import { type FileActions, FileRows } from "./FileRows.js";
import { NameForm } from "./NameForm.js";
import { EmptySubjectsActions, type ScheduleOffer } from "./ScheduleSubjectsReview.js";

export { NameForm };

// Names of subjects, folders and files are typed or imported text: only text nodes here, never markup.

/** A row of «Входящие» dragged onto the cards of the list (TASK-339). */
export interface CardDrop {
  /** A row is being dragged: the cards show the cue. */
  dragging: boolean;
  /** The files of «Входящие» a drop may carry. */
  inboxIds: ReadonlySet<string>;
  onDrop(subject: LibrarySubject, materialId: string): void;
  onEnd(): void;
}

/** Semesters, each with its subjects. With `drop` the cards take a dragged file of «Входящие». */
export function SubjectList(props: {
  subjects: readonly LibrarySubject[];
  onOpen(id: string): void;
  onAdd(): void;
  drop?: CardDrop;
  /** TASK-312: the subjects of the schedule; the primary action of the empty list (see EmptySubjectsActions). */
  offer?: ScheduleOffer;
}) {
  const [over, setOver] = useState("");
  if (props.subjects.length === 0) {
    const groups = props.offer === undefined ? 0 : (offerGroups(props.offer.state) ?? 0);
    return (
      <EmptyState
        label="Библиотека"
        title={texts.emptyTitle}
        actions={<EmptySubjectsActions offer={props.offer} addLabel={props.offer ? reviewTexts.addSubject : texts.addSubject} onAdd={props.onAdd} />}
      >
        {props.offer === undefined ? texts.emptyText : groups > 0 ? reviewTexts.emptyWithGroups(groups) : reviewTexts.emptyPlain}
      </EmptyState>
    );
  }
  const drop = props.drop;
  const onDragOver = (e: DragEvent<HTMLButtonElement>, id: string) => {
    if (drop && isMaterialDrag(e.dataTransfer)) {
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      setOver(id);
    }
  };
  const onDrop = (e: DragEvent<HTMLButtonElement>, s: LibrarySubject) => {
    if (!drop || !isMaterialDrag(e.dataTransfer)) {
      return;
    }
    e.preventDefault();
    setOver("");
    drop.onEnd();
    const id = readMaterialDrop(e.dataTransfer.getData(materialDragType), drop.inboxIds);
    if (id !== null) {
      drop.onDrop(s, id);
    }
  };
  return (
    <>
      {groupBySemester(props.subjects).map((g) => (
        <section className="section" key={g.code}>
          <h2 className="sec-h">
            {g.label} <span className="cnt num">· {g.subjects.length}</span>
          </h2>
          <ul className="subj-cards">
            {g.subjects.map((s) => (
              <li key={s.id}>
                <button
                  type="button"
                  className={over === s.id ? "subj-card drop" : "subj-card"}
                  onClick={() => props.onOpen(s.id)}
                  onDragOver={(e) => onDragOver(e, s.id)}
                  onDragLeave={() => setOver((o) => (o === s.id ? "" : o))}
                  onDrop={(e) => onDrop(e, s)}
                >
                  <span className="subj-card-head">
                    <Icon name="folder" />
                    <span className="t">{s.name}</span>
                  </span>
                  <span className="small muted">{orderedFolders(s.folders).map(folderTitle).join(" · ")}</span>
                  {drop?.dragging === true ? (
                    <span className="subj-card-open small">
                      <span>{dropCue(s.name)}</span>
                    </span>
                  ) : (
                    <span className="subj-card-open small">
                      <span>Открыть</span>
                      <Icon name="chevr" />
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </>
  );
}

function scrollToFolder(folderId: string) {
  scrollToFolderIn(typeof document === "undefined" ? undefined : document, folderId);
}

/**
 * The tree of the subject page: semesters (collapsible headings), their subjects, and the folders of the open subject with
 * the number of files in each; a folder scrolls to its section. `counts` maps a folder id to its files; null while the
 * files are being read.
 */
export function SubjectTree(props: {
  subjects: readonly LibrarySubject[];
  currentId: string;
  counts: ReadonlyMap<string, number> | null;
  /** Folders with fresh files get «новое»; optional so the tree works without the overview. */
  freshFolders?: ReadonlySet<string>;
  busy: boolean;
  onOpen(id: string): void;
  onAddSubject(): void;
  onAddFolder(): void;
}) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const toggle = (code: string) =>
    setCollapsed((c) => {
      const next = new Set(c);
      if (!next.delete(code)) {
        next.add(code);
      }
      return next;
    });
  return (
    <nav className="tree" aria-label={texts.title}>
      <ul className="lv0">
        {groupBySemester(props.subjects).map((g) => {
          const shut = collapsed.has(g.code);
          return (
            <li key={g.code}>
              {/* A heading of the group, not a row: it has no folder icon and the subjects below are indented under it. */}
              <button type="button" className="ti sem" aria-expanded={!shut} onClick={() => toggle(g.code)}>
                <Icon name={shut ? "chevr" : "chev"} />
                <span className="nm">{g.label}</span>
                {shut && <span className="cnt num">{g.subjects.length}</span>}
              </button>
              {!shut && (
                <ul className="lv1">
                  {g.subjects.map((s) => (
                    <li key={s.id}>
                      <button
                        type="button"
                        className={s.id === props.currentId ? "ti cur" : "ti"}
                        aria-current={s.id === props.currentId ? "page" : undefined}
                        onClick={() => props.onOpen(s.id)}
                      >
                        <Icon name={s.id === props.currentId ? "chev" : "chevr"} />
                        <span className="nm">{s.name}</span>
                      </button>
                      {s.id === props.currentId && (
                        <ul className="lv2">
                          {orderedFolders(s.folders).map((f) => (
                            <li key={f.id}>
                              <button type="button" className="ti fold" onClick={() => scrollToFolder(f.id)}>
                                <Icon name="folder" />
                                <span className="nm">{folderTitle(f)}</span>
                                {props.freshFolders?.has(f.id) === true && <span className="fresh">{texts.fresh}</span>}
                                <span className="cnt num">{treeCount(props.counts?.get(f.id))}</span>
                              </button>
                            </li>
                          ))}
                        </ul>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </li>
          );
        })}
      </ul>
      <div className="tree-acts">
        <Button size="sm" icon="plus" disabled={props.busy} onClick={props.onAddSubject}>
          {texts.addSubject}
        </Button>
        {props.currentId !== "" && (
          <Button size="sm" icon="plus" disabled={props.busy} onClick={props.onAddFolder}>
            {texts.addFolder}
          </Button>
        )}
      </div>
    </nav>
  );
}

/** «Ближайшее» and the lessons of a subject. Titles come from the model or the student: text nodes only. */
export function OverviewSections(props: { overview: SubjectOverview }) {
  const { deadlines, lessons } = props.overview;
  return (
    <>
      {deadlines.length > 0 && (
        <section className="section">
          <h2 className="sec-h">{texts.upcoming}</h2>
          <Card className="lrows" label={texts.upcoming}>
            <div role="list">
              {deadlines.map((d) => {
                const chip = daysChip(d.daysLeft);
                return (
                  <div className="lrow drow" role="listitem" key={d.assignmentId}>
                    <div className="d num">
                      {weekdayOf(d.due)}
                      <span>{dayMonth(d.due)}</span>
                    </div>
                    <div className="tx">
                      <p className="t">{d.title}</p>
                    </div>
                    <StatusChip kind={chip.kind}>{chip.label}</StatusChip>
                  </div>
                );
              })}
            </div>
          </Card>
        </section>
      )}
      {lessons.length > 0 && (
        <section className="section">
          <h2 className="sec-h">
            {texts.lessons} <span className="cnt num">· {lessons.length}</span>
          </h2>
          <Card className="lrows" label={texts.lessons}>
            <div role="list">
              {lessons.map((l) => {
                const chip = lessonChip(l.status);
                const meta = lessonMeta(l);
                return (
                  <div className="lrow" role="listitem" key={l.recordingId}>
                    <Icon name="doc" />
                    <div className="tx">
                      <p className="t">{lessonTitle(l)}</p>
                      {meta !== "" && <div className="meta num">{meta}</div>}
                    </div>
                    {l.summaryNew && <span className="fresh">{texts.fresh}</span>}
                    <StatusChip kind={chip.kind} icon={chip.icon}>
                      {chip.label}
                    </StatusChip>
                  </div>
                );
              })}
            </div>
          </Card>
        </section>
      )}
    </>
  );
}

/** The page of a subject: its folders (system ones first), the files of each, «＋ Папка». */
export function SubjectDetail(props: {
  subject: LibrarySubject;
  materials: readonly LibraryMaterial[];
  /** The «новое» marks of this visit (TASK-227); none without the overview. */
  fresh?: FreshMarks;
  folderForm: boolean;
  busy: boolean;
  error: string;
  onCreateFolder(name: string): void;
  onCancelFolder(): void;
  /** What the rows can do (open, move, delete); without it the rows only show the files. */
  actions?: FileActions;
  /** «+» in the header of a folder: the file dialog, the file lands in this folder. */
  onAddFile?(folderId: string): void;
  /** What to show under the header of a folder (the result of its «+»); null for nothing. */
  folderExtra?(folderId: string): ReactNode;
}) {
  const { subject } = props;
  const fresh = props.fresh ?? noFresh;
  return (
    <>
      {orderedFolders(subject.folders).map((f) => {
        const files = filesOfFolder(props.materials, f.id);
        const onAddFile = props.onAddFile;
        return (
          <section className="section" key={f.id} id={folderAnchor(f.id)}>
            <div className="sec-head">
              <h2 className="sec-h">
                {folderTitle(f)} <span className="cnt num">· {folderCountText(files.length)}</span>
                {fresh.folders.has(f.id) && <span className="fresh">{texts.fresh}</span>}
              </h2>
              {onAddFile && <IconButton icon="plus" label={addToFolderLabel(folderTitle(f))} disabled={props.busy} onClick={() => onAddFile(f.id)} />}
            </div>
            {props.folderExtra?.(f.id)}
            <Card className="lrows" label={folderTitle(f)}>
              {files.length === 0 ? (
                <p className="small muted empty-folder">{texts.emptyFolder}</p>
              ) : (
                <FileRows files={files} where="folder" fresh={fresh.materials} actions={props.actions} />
              )}
            </Card>
          </section>
        );
      })}
      {props.folderForm && (
        <NameForm label={texts.folderName} busy={props.busy} error={props.error} onSubmit={props.onCreateFolder} onCancel={props.onCancelFolder} />
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
export function InboxList(props: { materials: readonly LibraryMaterial[]; actions?: FileActions }) {
  const files = props.materials.filter((m) => !m.removed);
  if (files.length === 0) {
    return null;
  }
  return (
    <section className="section" id={inboxAnchor}>
      <h2 className="sec-h">
        {texts.inbox} <span className="cnt num">· {folderCountText(files.length)}</span>
      </h2>
      <Card className="lrows" label={texts.inbox}>
        <FileRows files={files} where="inbox" actions={props.actions} />
      </Card>
      <p className="small muted group-foot">{texts.inboxHint}</p>
    </section>
  );
}

/**
 * The body of the list page: «Входящие» first (files waiting for a decision are what needs attention, and a long list of
 * semesters would push them out of sight), then the subjects by semester. «Входящие» is not drawn while empty.
 */
export function SubjectsListBody(props: {
  subjects: readonly LibrarySubject[];
  inbox: readonly LibraryMaterial[];
  inboxActions?: FileActions;
  onOpen(id: string): void;
  onAdd(): void;
  drop?: CardDrop;
  offer?: ScheduleOffer;
  /** The review is open over an empty list: its own empty state is not repeated (TASK-312). */
  hideList?: boolean;
}) {
  return (
    <>
      <InboxList materials={props.inbox} actions={props.inboxActions} />
      {props.hideList !== true && (
        <SubjectList subjects={props.subjects} onOpen={props.onOpen} onAdd={props.onAdd} drop={props.drop} offer={props.offer} />
      )}
    </>
  );
}
