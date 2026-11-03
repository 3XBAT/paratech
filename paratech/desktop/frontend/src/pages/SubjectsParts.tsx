import { type DragEvent, Fragment, type ReactNode, useState } from "react";
import type { LibraryMaterial, LibrarySubject, SubjectOverview } from "../bindings/library.js";
import {
  addToFolderLabel,
  dropCue,
  fileTexts,
  folderAnchor,
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
  foldEmptyFolders,
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
import type { PageId } from "../nav.js";
import { offerGroups, reviewTexts } from "../scheduleSubjectsView.js";
import { emptyInboxText, type InboxEntry } from "../subjectsHome.js";
import { Button, IconButton } from "../ui/Button.js";
import { Card, EmptyState, ResultLine } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { StatusChip } from "../ui/StatusChip.js";
import { type FileActions, FileRows } from "./FileRows.js";
import { NameForm } from "./NameForm.js";
import { EmptySubjectsActions, type ScheduleOffer } from "./ScheduleSubjectsReview.js";

export { NameForm };

// Names of subjects, folders and files are typed or imported text: only text nodes here, never markup.

/** A row of «Входящие» dragged onto the subjects of the tree (TASK-339, TASK-341). */
export interface TreeDrop {
  /** A row is being dragged: the tree shows the cue. */
  dragging: boolean;
  /** The files of «Входящие» a drop may carry. */
  inboxIds: ReadonlySet<string>;
  onDrop(subject: LibrarySubject, materialId: string): void;
  onEnd(): void;
}

/**
 * The screen of a library without subjects: why it is empty and how to make the first ones (TASK-312). With subjects the
 * section shows the tree instead (TASK-341), so there is nothing else here.
 */
export function EmptySubjects(props: {
  onAdd(): void;
  /** TASK-312: the subjects of the schedule; the primary action of the empty screen (see EmptySubjectsActions). */
  offer?: ScheduleOffer;
}) {
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

/**
 * The buttons in the head of every screen of the section: «База знаний» and «Билеты» (the pages that belong to the
 * library), «Добавить файл» (its tooltip says where the file goes), and what the screen adds (`children`). With
 * `links={false}` (the library without subjects, where both pages are empty) only «Добавить файл» is left.
 */
export function LibraryActions(props: {
  onOpen(id: PageId): void;
  onAddFile(): void;
  addTip: string;
  busy: boolean;
  links?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className="acts">
      {props.links !== false && (
        <>
          <Button icon="book" onClick={() => props.onOpen("knowledge")}>
            {texts.knowledge}
          </Button>
          <Button icon="doc" onClick={() => props.onOpen("tickets")}>
            Билеты
          </Button>
        </>
      )}
      <Button icon="up" disabled={props.busy} title={props.addTip} onClick={props.onAddFile}>
        {texts.addFile}
      </Button>
      {props.children}
    </div>
  );
}

function scrollToFolder(folderId: string) {
  scrollToFolderIn(typeof document === "undefined" ? undefined : document, folderId);
}

/**
 * The tree of the section: «Входящие» first (like the inbox of a mail client), then the semesters (collapsible headings),
 * their subjects, and the folders of the open subject with the number of files in each; a folder scrolls to its section.
 * `counts` maps a folder id to its files; null while the files are being read. With `drop` the subjects take a dragged
 * file of «Входящие».
 */
export function SubjectTree(props: {
  subjects: readonly LibrarySubject[];
  /** The subject on screen; "" when it is «Входящие». */
  currentId: string;
  counts: ReadonlyMap<string, number> | null;
  /** Folders with fresh files get «новое»; optional so the tree works without the overview. */
  freshFolders?: ReadonlySet<string>;
  busy: boolean;
  onOpen(id: string): void;
  onAddSubject(): void;
  onAddFolder(): void;
  /** The first item; without it the tree starts with the semesters. */
  inbox?: InboxEntry;
  onOpenInbox?(): void;
  drop?: TreeDrop;
  /** The id of the «Предмет» button (the focus returns to it when the review of the schedule closes). */
  addId?: string;
}) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [over, setOver] = useState("");
  const toggle = (code: string) =>
    setCollapsed((c) => {
      const next = new Set(c);
      if (!next.delete(code)) {
        next.add(code);
      }
      return next;
    });

  const drop = props.drop;
  const overNow = drop?.dragging === true ? over : "";
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
  const overName = props.subjects.find((s) => s.id === overNow)?.name;
  const entry = props.inbox;
  return (
    <nav className="tree" aria-label={texts.title}>
      {/* Above the items, in a line that is always there once a file can be dragged, so nothing shifts when the drag starts. */}
      {drop !== undefined && drop.inboxIds.size > 0 && (
        <p className="tree-cue small muted" aria-live="polite">
          {drop.dragging ? (overName === undefined ? fileTexts.dropOnSubject : dropCue(overName)) : ""}
        </p>
      )}
      <ul className="lv0">
        {entry && (
          <li>
            <button
              type="button"
              className={["ti", "inbox", entry.current ? "cur" : "", entry.quiet ? "quiet" : "", entry.count !== "" ? "has" : ""].filter((c) => c !== "").join(" ")}
              aria-current={entry.current ? "page" : undefined}
              aria-label={entry.title}
              title={entry.title}
              onClick={props.onOpenInbox}
            >
              <Icon name="down" />
              <span className="nm">{entry.label}</span>
              {entry.count !== "" && <span className="cnt num">{entry.count}</span>}
            </button>
          </li>
        )}
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
                        className={["ti", s.id === props.currentId ? "cur" : "", overNow === s.id ? "drop" : ""].filter((c) => c !== "").join(" ")}
                        aria-current={s.id === props.currentId ? "page" : undefined}
                        data-subject-id={s.id}
                        onClick={() => props.onOpen(s.id)}
                        onDragOver={(e) => onDragOver(e, s.id)}
                        onDragLeave={() => setOver((o) => (o === s.id ? "" : o))}
                        onDrop={(e) => onDrop(e, s)}
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
                                {/* Blank while the files are being read (or failed to load): «—» would claim an empty folder. */}
                                {props.counts !== null && <span className="cnt num">{treeCount(props.counts.get(f.id))}</span>}
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
        <Button id={props.addId} size="sm" icon="plus" disabled={props.busy} onClick={props.onAddSubject}>
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
export function OverviewSections(props: { overview: SubjectOverview; onOpenAssignment?: (assignmentId: string) => void }) {
  const { deadlines, lessons } = props.overview;
  const open = props.onOpenAssignment;
  return (
    <>
      {deadlines.length > 0 && (
        <section className="section">
          <h2 className="sec-h">{texts.upcoming}</h2>
          <Card className="lrows" label={texts.upcoming}>
            <div role="list">
              {deadlines.map((d) => {
                const chip = daysChip(d.daysLeft);
                const body = (
                  <>
                    {/* Spans only: the same body sits inside a <button> when the row opens its assignment. */}
                    <span className="d num">
                      {weekdayOf(d.due)}
                      <span>{dayMonth(d.due)}</span>
                    </span>
                    <span className="tx">
                      <span className="t">{d.title}</span>
                    </span>
                    <StatusChip kind={chip.kind}>{chip.label}</StatusChip>
                  </>
                );
                // The whole row opens its assignment (navigation only, as on «Сегодня»); the focus ring is the global one.
                return open === undefined ? (
                  <div className="lrow drow" role="listitem" key={d.assignmentId}>
                    {body}
                  </div>
                ) : (
                  <div className="drow-item" role="listitem" key={d.assignmentId}>
                    <button type="button" className="lrow drow drow-open" title={texts.openAssignment} onClick={() => open(d.assignmentId)}>
                      {body}
                    </button>
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
  /** The files of the subject; null while they are being read or when reading failed (see `failed`). */
  materials: readonly LibraryMaterial[] | null;
  /** The files could not be read: the page says so once, so the folders say nothing about their files (neither «пока нет файлов» nor «Загрузка…», which they do not show while loading either). */
  failed?: boolean;
  /** The «новое» marks of this visit (TASK-227); none without the overview. */
  fresh?: FreshMarks;
  /** Folders made during this visit: they stay sections («Пока пусто») instead of folding into the «Пусто» line. */
  created?: ReadonlySet<string>;
  folderForm: boolean;
  busy: boolean;
  error: string;
  onCreateFolder(name: string): void;
  onCancelFolder(): void;
  /** What the rows can do (open, move, delete); without it the rows only show the files. */
  actions?: FileActions;
  /** «+» in the header of a folder: the file dialog, the file lands in this folder. */
  onAddFile?(folderId: string): void;
  /**
   * What to show under the header of a folder (the result of its «+»); null for nothing. `named` is the folder's title
   * when it has no header of its own (it sits in the «Пусто» line), so the result can name it.
   */
  folderExtra?(folderId: string, named?: string): ReactNode;
}) {
  const { subject } = props;
  const fresh = props.fresh ?? noFresh;
  const onAddFile = props.onAddFile;
  // Folders without files are one line, not a section each (TASK-347); with unknown files nothing is called empty.
  // A folder just made and a folder marked «новое» keep their sections.
  const keepOpen = new Set([...(props.created ?? []), ...fresh.folders]);
  const { shown, empty } = foldEmptyFolders(subject.folders, props.materials, keepOpen);
  return (
    <>
      {shown.map((f) => {
        const files = props.materials === null ? null : filesOfFolder(props.materials, f.id);
        return (
          <section className="section" key={f.id} id={folderAnchor(f.id)}>
            <div className="sec-head">
              <h2 className="sec-h">
                {folderTitle(f)} {files !== null && files.length > 0 && <span className="cnt num">· {folderCountText(files.length)}</span>}
                {fresh.folders.has(f.id) && <span className="fresh">{texts.fresh}</span>}
              </h2>
              {onAddFile && <IconButton icon="plus" label={addToFolderLabel(folderTitle(f))} disabled={props.busy} onClick={() => onAddFile(f.id)} />}
            </div>
            {props.folderExtra?.(f.id)}
            {/* Unknown files (loading, failed): no «пусто» claim and no per-folder «Загрузка…»; a failure is one line under the head of the page. */}
            {files === null ? null : files.length === 0 ? (
              <p className="small muted folder-empty">{texts.folderEmpty}</p>
            ) : (
              <Card className="lrows" label={folderTitle(f)}>
                <FileRows files={files} where="folder" fresh={fresh.materials} actions={props.actions} />
              </Card>
            )}
          </section>
        );
      })}
      {empty.length > 0 && (
        <section className="section empty-folders">
          {/* The «+» after each name stays a visible button (keyboard reachable), the tree's click lands on the name. The
              names are set apart by the gap of the line, not by commas that would end up after a button. */}
          <div className="small muted empty-folders-line">
            <span className="empty-folders-label">{texts.emptyFolders}</span>
            {empty.map((f) => (
              <span className="empty-folder-name" id={folderAnchor(f.id)} key={f.id}>
                {folderTitle(f)}
                {onAddFile && <IconButton icon="plus" label={addToFolderLabel(folderTitle(f))} disabled={props.busy} onClick={() => onAddFile(f.id)} />}
              </span>
            ))}
          </div>
          {empty.map((f) => (
            <Fragment key={f.id}>{props.folderExtra?.(f.id, folderTitle(f))}</Fragment>
          ))}
        </section>
      )}
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

/**
 * «Входящие»: files that no subject holds yet (dropped on the window or added by hand). On the screen without subjects it
 * is a section above the empty state and is not drawn while empty. With `bare` it is the content of the view «Входящие»
 * (the head of the page already names it): no section heading, and while empty it says how files get here.
 */
export function InboxList(props: {
  materials: readonly LibraryMaterial[];
  actions?: FileActions;
  bare?: boolean;
  /** The first answer for «Входящие» has not come yet: an empty list is not announced as empty. */
  loading?: boolean;
  /** Reading «Входящие» failed: the page says so with «Повторить», an empty list must not read as «nothing here». */
  failed?: boolean;
}) {
  const files = props.materials.filter((m) => !m.removed);
  if (files.length === 0) {
    if (props.bare !== true || props.failed === true) {
      return null;
    }
    return <p className="muted inbox-empty">{props.loading === true ? texts.loading : emptyInboxText}</p>;
  }
  return (
    <section className="section">
      {props.bare !== true && (
        <h2 className="sec-h">
          {texts.inbox} <span className="cnt num">· {folderCountText(files.length)}</span>
        </h2>
      )}
      <Card className="lrows" label={texts.inbox}>
        <FileRows files={files} where="inbox" actions={props.actions} />
      </Card>
      <p className="small muted group-foot">{texts.inboxHint}</p>
    </section>
  );
}
