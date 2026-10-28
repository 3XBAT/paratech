import { type DragEvent, type KeyboardEvent, useState, useRef } from "react";
import type { LibraryMaterial, LibrarySubject } from "../bindings/library.js";
import type { PlacementHint } from "../bindings/libraryInbox.js";
import {
  addFileTexts,
  errorWay,
  fileMenu,
  fileTexts,
  folderChoices,
  hintLine,
  type ImportElsewhere,
  materialDragType,
  moreLabel,
  newSubjectValue,
  type PanelPick,
  panelChoice,
  panelCreated,
  panelPickSubject,
  panelStart,
  primaryAction,
  type UndoItem,
} from "../libraryInboxView.js";
import { sizeText, texts, typeLabel } from "../libraryView.js";
import { Button } from "../ui/Button.js";
import { Field, ResultLine, Select } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { RowMenu } from "../ui/RowMenu.js";
import { NameForm } from "./NameForm.js";

// Names of files, subjects and folders are typed or imported text: only text nodes here, never markup or attributes
// built from them (the aria-labels are plain strings with the name inside, set as attribute values by React).

/**
 * What a row can do. Every call resolves to "" when it worked or to the Russian text of the refusal to show in the row.
 * The page owns the calls (useMaterialActions); a row only shows its own state (busy, panel, refusal).
 */
export interface FileActions {
  subjects: readonly LibrarySubject[];
  /** The hints of «Входящие» by file, and the files whose hint is being read. */
  hints: ReadonlyMap<string, PlacementHint>;
  hintPending: ReadonlySet<string>;
  open(m: LibraryMaterial): Promise<string>;
  place(m: LibraryMaterial, subjectId: string, folderId: string): Promise<string>;
  placeHint(m: LibraryMaterial, hint: PlacementHint): Promise<string>;
  toInbox(m: LibraryMaterial): Promise<string>;
  remove(m: LibraryMaterial): Promise<string>;
  /** Creates a subject for the panel «Разложить» («Новый предмет…»); it neither opens it nor places anything into it. */
  createSubject(name: string): Promise<CreatedSubject>;
  /** Re-reads the lists: the way out of a refusal that says the list is out of date. */
  refresh(): void;
  dragStart?(materialId: string): void;
  dragEnd?(): void;
}

/** The answer of a subject creation: the subject, or the Russian text of the refusal. */
export interface CreatedSubject {
  error: string;
  subject: LibrarySubject | null;
}

/** A refusal in plain words and the way out as a button: «Обновить список» or «Повторить». */
export function ErrorLine(props: { text: string; onRefresh(): void; onRetry?: () => void }) {
  const refresh = errorWay(props.text) === "refresh";
  return (
    <div className="lrow-error">
      <ResultLine kind="fail" role="alert">
        {props.text}
      </ResultLine>
      {refresh ? (
        <Button size="sm" onClick={props.onRefresh}>
          {fileTexts.refresh}
        </Button>
      ) : (
        props.onRetry && (
          <Button size="sm" onClick={props.onRetry}>
            {fileTexts.retry}
          </Button>
        )
      )}
    </div>
  );
}

/**
 * The result of «Добавить файл» on the page of a subject: the line (nothing when the dialog was cancelled), the way on
 * when the file lies elsewhere («Переместить сюда», «Открыть предмет») or unplaced («Разложить» in the line,
 * «Перейти во «Входящие»»), and a refusal in plain words with «Повторить» / «Обновить список». Nothing here moves a
 * file by itself.
 */
export function AddFileLines(props: {
  note: string;
  elsewhere: ImportElsewhere | null;
  toInbox: boolean;
  /** The file that did not land and the place it was meant for; with `placing` the line offers «Разложить». */
  unplaced?: { material: LibraryMaterial; subjectId: string; folderId: string } | null;
  placing?: Placing;
  busy: boolean;
  error: string;
  onMoveHere(): void;
  onOpenSubject(subjectId: string): void;
  onOpenInbox(): void;
  onRetry(): void;
  onRefresh(): void;
}) {
  const e = props.elsewhere;
  return (
    <>
      {props.note !== "" && props.toInbox && (
        <NotPlacedLine
          note={props.note}
          unplaced={props.unplaced ?? null}
          placing={props.placing}
          busy={props.busy}
          onOpenInbox={props.onOpenInbox}
          onRefresh={props.onRefresh}
        />
      )}
      {props.note !== "" && !props.toInbox && (
        <div className="lrow-error">
          <ResultLine kind="ok" role="status">
            {props.note}
          </ResultLine>
          {e !== null && (
            <Button size="sm" disabled={props.busy} onClick={props.onMoveHere}>
              {addFileTexts.moveHere}
            </Button>
          )}
          {e !== null && e.openSubjectId !== "" && (
            <Button size="sm" variant="ghost" onClick={() => props.onOpenSubject(e.openSubjectId)}>
              {addFileTexts.openSubject}
            </Button>
          )}
        </div>
      )}
      {props.error !== "" && <ErrorLine text={props.error} onRefresh={props.onRefresh} onRetry={props.onRetry} />}
    </>
  );
}

/** What the line of an unplaced file needs to place it: the lists and the calls of the page. */
export interface Placing {
  subjects: readonly LibrarySubject[];
  /** Places the file; resolves to "" or the Russian text of the refusal. */
  place(subjectId: string, folderId: string): Promise<string>;
  createSubject(name: string): Promise<CreatedSubject>;
}

/**
 * The file was kept but did not land where it was meant to: a neutral (warning) line with «Разложить», which opens the
 * panel of the row right here with that subject and folder chosen, and «Перейти во «Входящие»».
 */
function NotPlacedLine(props: {
  note: string;
  unplaced: { subjectId: string; folderId: string } | null;
  placing?: Placing;
  busy: boolean;
  onOpenInbox(): void;
  onRefresh(): void;
}) {
  const [panel, setPanel] = useState(false);
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState("");
  const { placing, unplaced } = props;
  const submit = async (subjectId: string, folderId: string) => {
    if (!placing) {
      return;
    }
    setWorking(true);
    setProblem("");
    const text = await placing.place(subjectId, folderId);
    setWorking(false);
    if (text !== "") {
      setProblem(text);
    }
  };
  const canPlace = placing !== undefined && unplaced !== null;
  return (
    <>
      <div className="lrow-error">
        <ResultLine kind="wait" role="status">
          {props.note}
        </ResultLine>
        {canPlace && !panel && (
          <Button size="sm" disabled={props.busy} onClick={() => setPanel(true)}>
            {addFileTexts.place}
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={props.onOpenInbox}>
          {addFileTexts.openInbox}
        </Button>
      </div>
      {canPlace && panel && (
        <div className="place-inline">
          <PlacePanel
            subjects={placing.subjects}
            subjectId={unplaced.subjectId}
            folderId={unplaced.folderId}
            busy={props.busy || working}
            onSubmit={(s, f) => void submit(s, f)}
            onCancel={() => setPanel(false)}
            onCreateSubject={placing.createSubject}
          />
          {problem !== "" && <ErrorLine text={problem} onRefresh={props.onRefresh} />}
        </div>
      )}
    </>
  );
}

/**
 * The panel under a row: the subject and the folder, «Положить» and «Отмена». Esc cancels; the focus starts in the first
 * field. The subject starts on «Выберите предмет» unless one is given (a hint, the place of a placed file), and «Положить»
 * stays off until a subject is chosen. «Новый предмет…» turns the subject part into the name form; the subject made
 * there is selected, nothing is placed by it.
 */
export function PlacePanel(props: {
  subjects: readonly LibrarySubject[];
  subjectId: string;
  folderId: string;
  busy: boolean;
  onSubmit(subjectId: string, folderId: string): void;
  onCancel(): void;
  onCreateSubject(name: string): Promise<CreatedSubject>;
}) {
  const [pick, setPick] = useState<PanelPick>(() => panelStart(props.subjectId, props.folderId));
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState("");
  const choice = panelChoice(props.subjects, pick.subjectId, pick.folderId);
  const subject = props.subjects.find((s) => s.id === choice.subjectId);
  const creating = pick.creating;

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      props.onCancel();
    }
  };

  const create = async (name: string) => {
    setCreateBusy(true);
    setCreateError("");
    const r = await props.onCreateSubject(name);
    setCreateBusy(false);
    if (r.subject !== null) {
      setPick(panelCreated(r.subject.id));
    } else {
      setCreateError(r.error);
    }
  };

  if (creating || props.subjects.length === 0) {
    return (
      <div className="place-panel" role="group" aria-label={fileTexts.place} onKeyDown={onKeyDown}>
        {creating ? (
          <NameForm
            label={texts.subjectName}
            busy={createBusy}
            error={createError}
            primary={false}
            onSubmit={(n) => void create(n)}
            onCancel={() => {
              setCreateError("");
              setPick((p) => ({ ...p, creating: false }));
            }}
          />
        ) : (
          <>
            <span className="muted">{fileTexts.noSubjects}</span>
            <Button size="sm" autoFocus onClick={() => setPick((p) => ({ ...p, creating: true }))}>
              {fileTexts.createSubject}
            </Button>
            <Button size="sm" variant="ghost" onClick={props.onCancel}>
              {fileTexts.cancel}
            </Button>
          </>
        )}
      </div>
    );
  }

  return (
    <div className="place-panel" role="group" aria-label={fileTexts.place} onKeyDown={onKeyDown}>
      <Field label={fileTexts.subject}>
        {(id) => (
          <Select id={id} autoFocus value={choice.subjectId} onChange={(e) => setPick((p) => panelPickSubject(p, e.target.value))}>
            <option value="">{fileTexts.pickSubject}</option>
            {props.subjects.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
            <option value={newSubjectValue}>{fileTexts.newSubject}</option>
          </Select>
        )}
      </Field>
      <Field label={fileTexts.folder}>
        {(id) => (
          <Select
            id={id}
            value={choice.folderId}
            disabled={subject === undefined}
            onChange={(e) => setPick((p) => ({ ...p, subjectId: choice.subjectId, folderId: e.target.value }))}
          >
            {subject === undefined && <option value="">{fileTexts.noFolder}</option>}
            {folderChoices(subject).map((f) => (
              <option key={f.id} value={f.id}>
                {f.title}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <div className="acts start">
        <Button
          size="sm"
          disabled={props.busy || choice.subjectId === "" || choice.folderId === ""}
          onClick={() => props.onSubmit(choice.subjectId, choice.folderId)}
        >
          {fileTexts.putHere}
        </Button>
        <Button size="sm" variant="ghost" disabled={props.busy} onClick={props.onCancel}>
          {fileTexts.cancel}
        </Button>
      </div>
    </div>
  );
}

/**
 * One file: the name and what it is, one visible button by state («Разложить» in «Входящие», «Открыть» in a folder),
 * «⋯» with the rest (a right click opens the same menu), the hint where it goes, and the panel and the refusals below.
 * Without `actions` it only shows the file.
 */
export function FileRow(props: { m: LibraryMaterial; where: "inbox" | "folder"; fresh?: boolean; actions?: FileActions }) {
  const { m, actions: a } = props;
  const placed = props.where === "folder";
  const meta = [typeLabel(m.type), sizeText(m.sizeBytes), m.textNote].filter((x) => x !== "").join(" · ");
  const [panel, setPanel] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<{ text: string; again: () => void } | null>(null);
  const [menuAsk, setMenuAsk] = useState(0);
  const running = useRef(false); // a double click comes before the re-render that disables the button
  const row = useRef<HTMLDivElement>(null);

  async function act(work: () => Promise<string>) {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setProblem(null);
    try {
      const text = await work();
      if (text === "") {
        setPanel(false);
      } else {
        setProblem({ text, again: () => void act(work) });
      }
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  const closePanel = () => {
    setPanel(false);
    row.current?.querySelector<HTMLButtonElement>(".lrow-primary")?.focus();
  };

  const run = (key: "open" | "place" | "toInbox" | "remove") => {
    if (!a) {
      return;
    }
    switch (key) {
      case "open":
        void act(() => a.open(m));
        return;
      case "place":
        setProblem(null);
        setPanel(true);
        return;
      case "toInbox":
        void act(() => a.toInbox(m));
        return;
      case "remove":
        void act(() => a.remove(m));
    }
  };

  const dragStart = (e: DragEvent<HTMLSpanElement>) => {
    e.dataTransfer.setData(materialDragType, m.id);
    e.dataTransfer.effectAllowed = "move";
    if (row.current !== null && typeof e.dataTransfer.setDragImage === "function") {
      e.dataTransfer.setDragImage(row.current, 16, 16);
    }
    a?.dragStart?.(m.id);
  };

  const known = a && !placed ? a.hints.get(m.id) : undefined; // also while the panel is open: it starts on the hint
  const hint = panel ? undefined : known;
  const line = hintLine(hint);
  const hintPending = a !== undefined && !placed && !panel && line === null && a.hintPending.has(m.id);
  const primary = primaryAction(placed);
  const icon = <Icon name="doc" />;

  return (
    <div
      className="lrow"
      role="listitem"
      ref={row}
      onContextMenu={
        a
          ? (e) => {
              e.preventDefault();
              setMenuAsk((n) => n + 1);
            }
          : undefined
      }
    >
      {a && !placed ? (
        // Only the icon is the handle, so the name stays selectable and the buttons keep working.
        <span className="lrow-grip" draggable title={fileTexts.dragTitle} onDragStart={dragStart} onDragEnd={() => a.dragEnd?.()}>
          {icon}
        </span>
      ) : (
        icon
      )}
      <div className="tx">
        <p className="t">{m.name}</p>
        {meta !== "" && <div className="meta num">{meta}</div>}
        {line !== null && a && hint && (
          <div className="lrow-hint small">
            {line.text !== "" && <span className="muted">{line.text} ·</span>}
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              aria-label={`${line.action}: ${m.name}`}
              onClick={() => void act(() => a.placeHint(m, hint))}
            >
              {line.action}
            </Button>
          </div>
        )}
        {/* Nothing is written while the hint loads: the line only keeps its height, so the rows do not jump. */}
        {hintPending && <div className="lrow-hint small" aria-hidden="true" />}
      </div>
      {props.fresh === true && <span className="fresh">{texts.fresh}</span>}
      {a && (
        <>
          <Button
            size="sm"
            className="lrow-primary"
            disabled={busy}
            aria-label={`${primary.label}: ${m.name}`}
            onClick={() => run(primary.key)}
          >
            {primary.label}
          </Button>
          <RowMenu
            label={moreLabel(m.name)}
            title={fileTexts.menuTitle}
            items={fileMenu(placed).map((i) => ({ key: i.key, label: i.label, disabled: busy }))}
            onPick={run}
            openRequest={menuAsk}
          />
        </>
      )}
      {a && (panel || problem !== null) && (
        <div className="lrow-extra">
          {panel && (
            <PlacePanel
              subjects={a.subjects}
              subjectId={known?.kind === "existing" ? known.subjectId : placed ? m.subjectId : ""}
              folderId={known?.kind === "existing" ? known.folderId : placed ? m.folderId : ""}
              busy={busy}
              onSubmit={(s, f) => void act(() => a.place(m, s, f))}
              onCancel={closePanel}
              onCreateSubject={a.createSubject}
            />
          )}
          {problem !== null && <ErrorLine text={problem.text} onRefresh={a.refresh} onRetry={panel ? undefined : problem.again} />}
        </div>
      )}
    </div>
  );
}

export function FileRows(props: {
  files: readonly LibraryMaterial[];
  where: "inbox" | "folder";
  fresh?: ReadonlySet<string>;
  actions?: FileActions;
}) {
  return (
    <div role="list">
      {props.files.map((m) => (
        <FileRow key={m.id} m={m} where={props.where} fresh={props.fresh?.has(m.id)} actions={props.actions} />
      ))}
    </div>
  );
}

/**
 * «Положено в «Сети → Лабораторные». Вернуть» and «Удалено «имя». Вернуть»: a live region that is always there, so a
 * screen reader announces the line when it appears. A refused «Вернуть» says why and offers «Обновить список».
 */
export function UndoLines(props: { items: readonly UndoItem[]; busy: boolean; onUndo(item: UndoItem): void; onRefresh(): void }) {
  return (
    <div className="undo-lines" role="status" aria-live="polite">
      {props.items.map((it) => (
        <div className="undo-line" key={it.id}>
          <span>
            {it.text}
            {it.error !== "" && <span className="muted"> {it.error}</span>}
          </span>
          {it.error !== "" && errorWay(it.error) === "refresh" ? (
            <Button size="sm" variant="ghost" onClick={props.onRefresh}>
              {fileTexts.refresh}
            </Button>
          ) : (
            <Button size="sm" variant="ghost" disabled={props.busy} onClick={() => props.onUndo(it)}>
              {fileTexts.undo}
            </Button>
          )}
        </div>
      ))}
    </div>
  );
}
