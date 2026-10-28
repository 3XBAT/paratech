import { type DragEvent, type KeyboardEvent, useState, useRef } from "react";
import type { LibraryMaterial, LibrarySubject } from "../bindings/library.js";
import type { PlacementHint } from "../bindings/libraryInbox.js";
import {
  errorWay,
  fileMenu,
  fileTexts,
  folderChoices,
  hintLine,
  materialDragType,
  moreLabel,
  panelChoice,
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
  /** Creates a subject for the panel «Разложить» when there is none; it does not open it. */
  createSubject(name: string): Promise<string>;
  /** Re-reads the lists: the way out of a refusal that says the list is out of date. */
  refresh(): void;
  dragStart?(materialId: string): void;
  dragEnd?(): void;
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

/** The panel under a row: the subject and the folder, «Положить» and «Отмена». Esc cancels; the focus starts in the first field. */
export function PlacePanel(props: {
  subjects: readonly LibrarySubject[];
  subjectId: string;
  folderId: string;
  busy: boolean;
  onSubmit(subjectId: string, folderId: string): void;
  onCancel(): void;
  onCreateSubject(name: string): Promise<string>;
}) {
  const [subjectId, setSubjectId] = useState(props.subjectId);
  const [folderId, setFolderId] = useState(props.folderId);
  const [creating, setCreating] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState("");
  const choice = panelChoice(props.subjects, subjectId, folderId);
  const subject = props.subjects.find((s) => s.id === choice.subjectId);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      props.onCancel();
    }
  };

  const create = async (name: string) => {
    setCreateBusy(true);
    setCreateError("");
    const text = await props.onCreateSubject(name);
    setCreateBusy(false);
    if (text === "") {
      setCreating(false);
    } else {
      setCreateError(text);
    }
  };

  if (props.subjects.length === 0) {
    return (
      <div className="place-panel" role="group" aria-label={fileTexts.place} onKeyDown={onKeyDown}>
        {creating ? (
          <NameForm
            label={texts.subjectName}
            busy={createBusy}
            error={createError}
            primary={false}
            onSubmit={(n) => void create(n)}
            onCancel={() => setCreating(false)}
          />
        ) : (
          <>
            <span className="muted">{fileTexts.noSubjects}</span>
            <Button size="sm" autoFocus onClick={() => setCreating(true)}>
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
          <Select
            id={id}
            autoFocus
            value={choice.subjectId}
            onChange={(e) => {
              setSubjectId(e.target.value);
              setFolderId("");
            }}
          >
            {props.subjects.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field label={fileTexts.folder}>
        {(id) => (
          <Select id={id} value={choice.folderId} onChange={(e) => setFolderId(e.target.value)}>
            {folderChoices(subject).map((f) => (
              <option key={f.id} value={f.id}>
                {f.title}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <div className="acts start">
        <Button size="sm" disabled={props.busy || choice.folderId === ""} onClick={() => props.onSubmit(choice.subjectId, choice.folderId)}>
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

  const hint = a && !placed && !panel ? a.hints.get(m.id) : undefined;
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
        <span className="lrow-grip" draggable onDragStart={dragStart} onDragEnd={() => a.dragEnd?.()}>
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
        {hintPending && (
          <div className="lrow-hint small muted" aria-hidden="true">
            {fileTexts.hintLoading}
          </div>
        )}
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
              subjectId={hint?.kind === "existing" ? hint.subjectId : placed ? m.subjectId : ""}
              folderId={hint?.kind === "existing" ? hint.folderId : placed ? m.folderId : ""}
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
