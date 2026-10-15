import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { formatTime } from "../activityView";
import { CollapsibleList } from "../CollapsibleList";
import { initialListState, type ListState } from "../listView";
import {
  modelsApi,
  type AvailableModel,
  type InstalledModel,
  type ModelDownload,
  type ModelsView,
} from "../bindings/models";
import { callByName } from "../bindings/runtime";
import {
  canConfirm,
  catalogActions,
  confirmation,
  confirmStep,
  defaultBadge,
  downloadOf,
  downloadText,
  errorText,
  type ImportStep,
  installedText,
  keepsSelection,
  modelTitle,
  shortSha,
  sizeText,
  statusNotes,
  texts,
} from "../modelsView";

const pollMs = 2000;

type Field = "name" | "version" | "quantization";

// Model manager (PRD section 8): installed models, the catalog, downloads and the file import with a SHA-256 the user
// compares before installing (AC-23). The core decides everything; the page shows its state and its error texts.
// Text from a file or the catalog is plain text, each piece in its own <bdi>.
export function ModelsPage() {
  const api = useMemo(() => modelsApi(callByName), []);
  const [view, setView] = useState<ModelsView | null>(null);
  const [loadError, setLoadError] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const [step, setStep] = useState<ImportStep>({ step: "idle" });
  const [dialogBusy, setDialogBusy] = useState(false);
  const [deleting, setDeleting] = useState("");
  const [installedList, setInstalledList] = useState<ListState>(initialListState); // survives the polls
  const loading = useRef(false);
  const again = useRef(false);
  const mounted = useRef(true);
  const dialog = useRef(false); // one dialog or import at a time; a double click must not start a second one
  // The id of the shown file selection, discarded in the core when the page closes. An id the core has dropped already
  // is ignored there.
  const selection = useRef("");

  // One load at a time: a refresh asked for during a load runs once more after it.
  const refresh = useCallback(async () => {
    if (loading.current) {
      again.current = true;
      return;
    }
    loading.current = true;
    try {
      do {
        again.current = false;
        try {
          const v = await api.list();
          if (mounted.current) {
            setView(v);
            setLoadError("");
          }
        } catch (err) {
          if (mounted.current) {
            setLoadError(errorText(err));
          }
        }
      } while (again.current && mounted.current);
    } finally {
      loading.current = false;
    }
  }, [api]);

  useEffect(() => {
    mounted.current = true;
    void refresh();
    const timer = setInterval(() => void refresh(), pollMs);
    return () => {
      mounted.current = false;
      clearInterval(timer);
      discardSelection();
    };
  }, [refresh]);

  function discardSelection() {
    const id = selection.current;
    selection.current = "";
    if (id !== "") {
      void api.discardImport(id).catch(() => undefined); // nothing to report: the selection is gone either way
    }
  }

  function clearMessages() {
    setError("");
    setDone("");
  }

  // A row action; a returned text is shown as the error (a warning the core sent instead of failing).
  async function act(key: string, action: () => Promise<string | void>) {
    setBusy((b) => new Set(b).add(key));
    clearMessages();
    try {
      const warning = await action();
      if (warning && mounted.current) {
        setError(warning);
      }
    } catch (err) {
      if (mounted.current) {
        setError(errorText(err));
      }
    } finally {
      if (mounted.current) {
        setBusy((b) => {
          const next = new Set(b);
          next.delete(key);
          return next;
        });
      }
    }
    if (mounted.current) {
      await refresh();
    }
  }

  // Runs an action that opens the core's file dialog or installs a file; the core allows one dialog at a time.
  async function withDialog(action: () => Promise<void>) {
    if (dialog.current) {
      return;
    }
    dialog.current = true;
    setDialogBusy(true);
    clearMessages();
    try {
      await action();
    } finally {
      dialog.current = false;
      if (mounted.current) {
        setDialogBusy(false);
        await refresh();
      }
    }
  }

  const choose = () => {
    const previous = step;
    return withDialog(async () => {
      setStep({ step: "choosing" });
      try {
        const sel = await api.chooseImportFile();
        if (sel.cancelled) {
          if (mounted.current) {
            setStep(previous); // a closed dialog keeps the shown file
          }
          return;
        }
        if (!mounted.current) {
          void api.discardImport(sel.selectionId).catch(() => undefined); // the page closed while choosing
          return;
        }
        selection.current = sel.selectionId;
        setStep(confirmStep(sel));
      } catch (err) {
        discardSelection(); // the core dropped the shown file with the error
        if (mounted.current) {
          setStep({ step: "idle" });
          setError(errorText(err));
        }
      }
    });
  };

  const confirm = () => {
    const s = step;
    if (s.step !== "confirm" || !canConfirm(s)) {
      return;
    }
    return withDialog(async () => {
      // The core takes the selection at the start of the confirm. Forget it now, or closing the page mid-import would
      // send a discard that can reach the core first and cancel the import.
      selection.current = "";
      setStep({ step: "installing", selection: s.selection });
      try {
        const m = await api.confirmImport(confirmation(s));
        if (mounted.current) {
          setStep({ step: "idle" });
          setDone(installedText(m));
        }
      } catch (err) {
        if (!keepsSelection(err)) {
          if (mounted.current) {
            setStep({ step: "idle" });
            setError(errorText(err));
          }
          return;
        }
        // A bad name, version or quantization keeps the selection in the core: fix the field and confirm again.
        if (!mounted.current) {
          void api.discardImport(s.selection.selectionId).catch(() => undefined);
          return;
        }
        selection.current = s.selection.selectionId;
        setStep(s);
        setError(errorText(err));
      }
    });
  };

  const cancelImport = () => {
    discardSelection();
    setStep({ step: "idle" });
    clearMessages();
  };

  const edit = (field: Field, value: string) => setStep((s) => (s.step === "confirm" ? { ...s, [field]: value } : s));

  const importCatalog = (m: AvailableModel) =>
    withDialog(async () => {
      try {
        const r = await api.importCatalogFile(m.catalogId);
        if (r.model !== null && mounted.current) {
          setDone(installedText(r.model));
        }
      } catch (err) {
        if (mounted.current) {
          setError(errorText(err));
        }
      }
    });

  const download = (m: AvailableModel) =>
    act(`download:${m.catalogId}`, async () => {
      const plan = await api.preflightDownload(m.catalogId);
      if (!plan.enough) {
        return plan.warning || "Недостаточно места на диске"; // never start a download that cannot fit
      }
      await api.startDownload(m.catalogId);
    });

  const openHome = () => void act("home", () => api.openModelsHomePage());

  return (
    <section>
      <h1>Модели</h1>
      <p className="muted">Распознавание речи идёт только на этом компьютере. Модель — файл whisper.cpp (.bin).</p>

      {view !== null &&
        statusNotes(view).map((n, i) => (
          <p key={i} className={n.warning ? "warning" : "muted"}>
            {n.text}
          </p>
        ))}

      <div className="actions">
        <button type="button" disabled={dialogBusy} onClick={() => void choose()}>
          Импортировать файл модели…
        </button>
        <button type="button" disabled={busy.has("home")} onClick={openHome}>
          Открыть страницу моделей whisper.cpp
        </button>
      </div>

      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {done && <p role="status">{done}</p>}

      {step.step === "choosing" && <p className="muted">{texts.choosing}</p>}
      {step.step === "installing" && <p className="muted">{texts.installing}</p>}
      {step.step === "confirm" && (
        <div className="preview">
          <h2>
            Файл: <bdi>{step.selection.fileName}</bdi>
          </h2>
          <p>Размер: {sizeText(step.selection.sizeBytes)}</p>
          <p>
            SHA-256:{" "}
            <code className="sha">
              <bdi>{step.selection.sha256}</bdi>
            </code>
          </p>
          <p className="warning">{texts.compareSha}</p>
          <label className="field">
            Название
            <input value={step.name} disabled={dialogBusy} onChange={(e) => edit("name", e.target.value)} />
          </label>
          <label className="field">
            Версия
            <input value={step.version} disabled={dialogBusy} onChange={(e) => edit("version", e.target.value)} />
          </label>
          <label className="field">
            Квантизация (можно оставить пустой)
            <input
              value={step.quantization}
              disabled={dialogBusy}
              onChange={(e) => edit("quantization", e.target.value)}
            />
          </label>
          <div className="actions">
            <button type="button" disabled={dialogBusy || !canConfirm(step)} onClick={() => void confirm()}>
              SHA-256 совпадает, установить
            </button>
            <button type="button" disabled={dialogBusy} onClick={cancelImport}>
              Отмена
            </button>
          </div>
        </div>
      )}

      {loadError && (
        <p className="error" role="alert">
          {loadError}
        </p>
      )}

      <h2>Установленные</h2>
      {view === null ? (
        !loadError && <p className="muted">Загрузка…</p>
      ) : view.installed.length === 0 ? (
        <p className="muted">Моделей пока нет.</p>
      ) : (
        <CollapsibleList
          items={view.installed}
          itemKey={(m) => m.id}
          state={installedList}
          onStateChange={setInstalledList}
          renderContainer={(rows) => (
            <table className="list">
              <thead>
                <tr>
                  <th>Модель</th>
                  <th>Размер</th>
                  <th>SHA-256</th>
                  <th>Установлена</th>
                  <th />
                </tr>
              </thead>
              <tbody>{rows}</tbody>
            </table>
          )}
          renderItem={(m) => (
            <InstalledRow
              model={m}
              badge={defaultBadge(m)}
              busy={busy}
              deleting={deleting === m.id}
              onDefault={() => void act(`default:${m.id}`, () => api.setDefault(m.id))}
              onAskDelete={() => setDeleting(m.id)}
              onDelete={() => {
                setDeleting("");
                void act(`delete:${m.id}`, () => api.delete(m.id));
              }}
              onKeep={() => setDeleting("")}
            />
          )}
        />
      )}

      <h2>Каталог</h2>
      {view !== null &&
        (view.available.length === 0 ? (
          <>
            <p className="muted">{texts.emptyCatalog}</p>
            <p className="muted">
              Страница моделей: <bdi>{view.modelsHomePage}</bdi>
            </p>
          </>
        ) : (
          <>
            {!view.machineKnown && <p className="muted">{texts.machineUnknown}</p>}
            <table className="list">
              <thead>
                <tr>
                  <th>Модель</th>
                  <th>Размер</th>
                  <th>Требования</th>
                  <th>Состояние</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {view.available.map((m) => (
                  <CatalogRow
                    key={m.catalogId}
                    model={m}
                    download={view.download}
                    busy={busy}
                    dialogBusy={dialogBusy}
                    onDownload={() => void download(m)}
                    onCancel={() => void act(`cancel:${m.catalogId}`, () => api.cancelDownload())}
                    onDiscard={() => void act(`discard:${m.catalogId}`, () => api.discardPartial(m.catalogId))}
                    onImport={() => void importCatalog(m)}
                    onSource={() => void act(`source:${m.catalogId}`, () => api.openSourcePage(m.catalogId))}
                  />
                ))}
              </tbody>
            </table>
          </>
        ))}
    </section>
  );
}

function InstalledRow(props: {
  model: InstalledModel;
  badge: string;
  busy: ReadonlySet<string>;
  deleting: boolean;
  onDefault: () => void;
  onAskDelete: () => void;
  onDelete: () => void;
  onKeep: () => void;
}) {
  const m = props.model;
  const deleteBusy = props.busy.has(`delete:${m.id}`);
  return (
    <tr>
      <td>
        <bdi>{modelTitle(m)}</bdi>
        {props.badge && <span className="badge">{props.badge}</span>}
      </td>
      <td>{sizeText(m.sizeBytes)}</td>
      <td>
        <details>
          <summary>
            <code>{shortSha(m.sha256)}</code>
          </summary>
          <code className="sha">
            <bdi>{m.sha256}</bdi>
          </code>
        </details>
      </td>
      <td>{formatTime(m.installedAt)}</td>
      <td>
        {props.deleting ? (
          <div className="row-actions">
            <span>{texts.deleteConfirm}</span>
            <button type="button" onClick={props.onDelete}>
              Да, удалить
            </button>
            <button type="button" onClick={props.onKeep}>
              Отмена
            </button>
          </div>
        ) : (
          <div className="row-actions">
            {!m.isDefault && (
              <button type="button" disabled={props.busy.has(`default:${m.id}`)} onClick={props.onDefault}>
                Сделать по умолчанию
              </button>
            )}
            <button type="button" disabled={deleteBusy} onClick={props.onAskDelete}>
              {deleteBusy ? "Удаление…" : "Удалить"}
            </button>
          </div>
        )}
      </td>
    </tr>
  );
}

function CatalogRow(props: {
  model: AvailableModel;
  download: ModelDownload;
  busy: ReadonlySet<string>;
  dialogBusy: boolean;
  onDownload: () => void;
  onCancel: () => void;
  onDiscard: () => void;
  onImport: () => void;
  onSource: () => void;
}) {
  const m = props.model;
  const a = catalogActions(m, props.download);
  const d = downloadOf(m, props.download);
  const key = (op: string) => props.busy.has(`${op}:${m.catalogId}`);
  return (
    <tr>
      <td>
        <bdi>{modelTitle(m)}</bdi>
        {m.recommended && <span className="badge">Рекомендуется</span>}
        {m.installed && <span className="badge">Установлена</span>}
        {m.likelySlow && <div className="warning">{texts.likelySlow}</div>}
      </td>
      <td>{sizeText(m.sizeBytes)}</td>
      <td>
        от {m.minCpuCores} ядер, {sizeText(m.minRamMb * 1024 * 1024)} памяти
      </td>
      <td className={d !== null && (d.phase === "failed" || d.phase === "cancelled") ? "problem" : ""}>
        {d !== null && downloadText(d)}
      </td>
      <td>
        <div className="row-actions">
          {a.download && (
            <button type="button" disabled={key("download")} onClick={props.onDownload}>
              Скачать
            </button>
          )}
          {a.cancel && (
            <button type="button" disabled={key("cancel")} onClick={props.onCancel}>
              Остановить скачивание
            </button>
          )}
          {a.discardPartial && (
            <button type="button" disabled={key("discard")} onClick={props.onDiscard}>
              Удалить недокачанный файл
            </button>
          )}
          {a.openSource && (
            <button type="button" disabled={key("source")} onClick={props.onSource}>
              Открыть страницу источника
            </button>
          )}
          {a.importFile && (
            <button type="button" disabled={props.dialogBusy} onClick={props.onImport}>
              Импортировать скачанный файл
            </button>
          )}
        </div>
      </td>
    </tr>
  );
}
