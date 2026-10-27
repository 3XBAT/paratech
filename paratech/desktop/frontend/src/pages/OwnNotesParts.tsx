import { dropAttributes, type NoteTarget, type NoteView } from "../bindings/notes.js";
import {
  checkPaste,
  maxPasteBytes,
  ownNotesTexts as t,
  partCountText,
  partRows,
  pasteCounterText,
  pasteLimitText,
  type ResultLine,
  shortName,
  type UndoAction,
} from "../ownNotesView.js";
import { Button } from "../ui/Button.js";
import { Card } from "../ui/controls.js";
import { Icon } from "../ui/Icon.js";
import { StatusChip } from "../ui/StatusChip.js";

// The «Мой конспект» block (TASK-154). Everything from the student or from files (titles, names, text) is rendered as
// React text children only: no HTML, no Markdown, no links. The state lives in OwnNotes.tsx.

export interface OwnNotesModel {
  notes: NoteView[];
  loading: boolean;
  /** "" when no library subject matches the lesson: adding is off. */
  subjectId: string;
  target: NoteTarget;
  busy: boolean;
  undo: UndoAction | null;
  /** The one-line state after an action («Файл заменён.») or a refusal. */
  status: ResultLine | null;
  /** The report of the last add, one line per file. */
  lines: ResultLine[];
  pasteOpen: boolean;
  pasteText: string;
}

export interface OwnNotesActions {
  onAddFiles(): void;
  onTogglePaste(): void;
  onPasteText(text: string): void;
  onPasteSave(): void;
  onReplace(noteId: string, materialId: string): void;
  onMove(note: NoteView, index: number, delta: -1 | 1): void;
  onRemove(noteId: string): void;
  onUndo(): void;
}

function NoteCard(props: { note: NoteView; model: OwnNotesModel; actions: OwnNotesActions }) {
  const { note, model, actions } = props;
  const off = model.busy;
  if (note.removed) {
    return (
      <Card className="box own-note">
        <p className="own-removed">{t.removedLine}</p>
        {model.undo?.kind === "remove" && model.undo.noteId === note.id && (
          <Button variant="secondary" size="sm" disabled={off} onClick={actions.onUndo}>
            {t.undo}
          </Button>
        )}
      </Card>
    );
  }
  const rows = partRows(note);
  return (
    <Card className="box own-note" label={shortName(note.title)}>
      <div className="own-note-head">
        <div>
          <StatusChip kind="neutral" icon="doc">
            {t.ownChip}
          </StatusChip>
          <h3 className="own-note-title" title={note.title}>
            {shortName(note.title)}
          </h3>
          <p className="small muted">{partCountText(rows.length)}</p>
        </div>
        <Button variant="ghost" size="sm" disabled={off} onClick={() => actions.onRemove(note.id)}>
          {t.remove}
        </Button>
      </div>
      {rows.length > 0 && (
        <ol className="own-parts" aria-label={t.partsLabel}>
          {rows.map((r, i) => (
            <li key={r.materialId} className="own-part">
              <span className="own-part-main">
                <span className="own-part-name" title={r.name}>
                  {shortName(r.name)}
                </span>
                {r.meta !== "" && <span className="small muted">{r.meta}</span>}
              </span>
              <span className="own-part-acts">
                <Button
                  variant="ghost"
                  size="sm"
                  icon="up"
                  disabled={off || !r.canUp}
                  aria-label={`${t.moveUp}: ${shortName(r.name)}`}
                  onClick={() => actions.onMove(note, i, -1)}
                >
                  {t.moveUp}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  icon="down"
                  disabled={off || !r.canDown}
                  aria-label={`${t.moveDown}: ${shortName(r.name)}`}
                  onClick={() => actions.onMove(note, i, 1)}
                >
                  {t.moveDown}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  icon="retry"
                  disabled={off}
                  aria-label={`${t.replace}: ${shortName(r.name)}`}
                  onClick={() => actions.onReplace(note.id, r.materialId)}
                >
                  {t.replace}
                </Button>
              </span>
            </li>
          ))}
        </ol>
      )}
    </Card>
  );
}

/** The student's block: separate from paratech's «Конспект», labelled «ваш конспект». */
export function OwnNotesView(props: { model: OwnNotesModel; actions: OwnNotesActions }) {
  const { model, actions } = props;
  const check = checkPaste(model.pasteText);
  const canAdd = model.subjectId !== "";
  const drop = canAdd ? dropAttributes(model.target) : {};
  const showUndo = model.undo !== null && model.undo.kind !== "remove";

  return (
    <section className="own-notes" aria-label={t.title} {...drop}>
      <div className="own-head">
        <h2 className="own-title">{t.title}</h2>
        <StatusChip kind="neutral" icon="doc">
          {t.ownChip}
        </StatusChip>
      </div>
      <div className="acts start">
        <Button variant="secondary" icon="plus" disabled={!canAdd || model.busy} onClick={actions.onAddFiles}>
          {t.addFiles}
        </Button>
        <Button
          variant="secondary"
          icon="doc"
          aria-expanded={model.pasteOpen}
          disabled={!canAdd || model.busy}
          onClick={actions.onTogglePaste}
        >
          {t.paste}
        </Button>
      </div>
      {canAdd ? <p className="small muted">{t.dropHint}</p> : <p className="small muted">{t.noSubject}</p>}
      {model.pasteOpen && canAdd && (
        <div className="own-paste">
          <label htmlFor="own-paste-text">{t.pasteLabel}</label>
          <textarea
            id="own-paste-text"
            className="input own-paste-text"
            value={model.pasteText}
            onChange={(e) => actions.onPasteText(e.target.value)}
            aria-describedby="own-paste-limit"
            aria-invalid={check.kind === "long"}
          />
          <p id="own-paste-limit" className="small muted">
            {pasteLimitText()} {pasteCounterText(model.pasteText)}
          </p>
          <div className="acts start">
            <Button
              variant="primary"
              disabled={model.busy || check.kind !== "ok"}
              disabledReason={check.kind === "long" ? `Текст больше ${maxPasteBytes / (1024 * 1024)} МБ.` : undefined}
              onClick={actions.onPasteSave}
            >
              {t.pasteSave}
            </Button>
            <Button variant="ghost" onClick={actions.onTogglePaste}>
              {t.pasteCancel}
            </Button>
          </div>
        </div>
      )}
      <div className="own-status" role="status" aria-live="polite" aria-label={t.statusLabel}>
        {model.status && (
          <p className={model.status.ok ? "result s-ok" : "result s-fail"}>
            <Icon name={model.status.ok ? "ok" : "warn"} />
            <span>{model.status.text}</span>
          </p>
        )}
        {model.lines.length > 0 && (
          <ul className="own-lines">
            {model.lines.map((l, i) => (
              <li key={i} className={l.ok ? "ok" : "fail"}>
                <Icon name={l.ok ? "ok" : "warn"} />
                <span>{l.text}</span>
              </li>
            ))}
          </ul>
        )}
        {showUndo && model.undo && (
          <Button variant="secondary" size="sm" disabled={model.busy} onClick={actions.onUndo}>
            {t.undo}
          </Button>
        )}
      </div>
      {model.loading ? (
        <p className="muted">{t.loading}</p>
      ) : model.notes.length === 0 ? (
        <p className="muted">{t.empty}</p>
      ) : (
        <div className="own-list">
          {model.notes.map((n) => (
            <NoteCard key={n.id} note={n} model={model} actions={actions} />
          ))}
        </div>
      )}
    </section>
  );
}

/** The chip above paratech's own «Конспект»: the two are never mixed. */
export function AiNoteLabel() {
  return (
    <div className="own-head">
      <h2 className="own-title">{t.paratechBlock}</h2>
      <StatusChip kind="neutral" icon="model">
        {t.paratechChip}
      </StatusChip>
    </div>
  );
}
