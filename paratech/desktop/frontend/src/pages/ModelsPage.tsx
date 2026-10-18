import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  catalogGroups,
  catalogLabels,
  confirmation,
  confirmStep,
  defaultBadge,
  downloadChip,
  downloadOf,
  downloadText,
  errorText,
  type ImportStep,
  installedText,
  keepsSelection,
  languageFilter,
  type LanguageFilter,
  languageOptions,
  loadFilter,
  type LoadFilter,
  loadLevels,
  loadOptions,
  modelTitle,
  progressPercent,
  shortSha,
  sizeText,
  statusNotes,
  texts,
} from "../modelsView";
import { Button } from "../ui/Button";
import { SpeechComponentNotice } from "./SpeechComponentNotice";
import { Banner, Card, Dialog, Field, Input, Progress, ResultLine, Select } from "../ui/controls";
import type { IconName } from "../ui/Icon";
import { StatusChip, type ChipKind } from "../ui/StatusChip";

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

  const deletingModel = view?.installed.find((m) => m.id === deleting) ?? null;

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">Распознавание речи работает без интернета</span>
          <h1>Модели речи</h1>
        </div>
        <div className="acts">
          <Button icon="up" disabled={dialogBusy} onClick={() => void choose()}>
            Импортировать файл модели…
          </Button>
        </div>
      </div>
      <p className="muted">Распознавание речи идёт только на этом компьютере. Модель — файл .bin.</p>

      <SpeechComponentNotice onReady={() => void refresh()} />
      {view !== null &&
        statusNotes(view).map((n, i) =>
          n.warning ? (
            <Banner key={i} kind="wait">
              <p>{n.text}</p>
            </Banner>
          ) : (
            <p key={i} className="muted">
              {n.text}
            </p>
          ),
        )}

      {error && (
        <Banner kind="fail" role="alert">
          {error}
        </Banner>
      )}
      {done && (
        <ResultLine kind="ok" role="status">
          {done}
        </ResultLine>
      )}

      {step.step === "choosing" && <p className="muted">{texts.choosing}</p>}
      {step.step === "installing" && <p className="muted">{texts.installing}</p>}
      {step.step === "confirm" && (
        <Card className="box preview" label="Импорт файла модели">
          <h2 className="breakable">
            Файл: <bdi>{step.selection.fileName}</bdi>
          </h2>
          <p className="small muted num">Размер: {sizeText(step.selection.sizeBytes)}</p>
          <p className="sha-line">
            <span className="small muted">SHA-256: </span>
            <code className="sha">
              <bdi>{step.selection.sha256}</bdi>
            </code>
          </p>
          <Banner kind="wait">
            <p>{texts.compareSha}</p>
          </Banner>
          <Field label="Название">
            {(id) => <Input id={id} value={step.name} disabled={dialogBusy} onChange={(e) => edit("name", e.target.value)} />}
          </Field>
          <Field label="Версия">
            {(id) => (
              <Input id={id} value={step.version} disabled={dialogBusy} onChange={(e) => edit("version", e.target.value)} />
            )}
          </Field>
          <Field label="Квантизация (можно оставить пустой)">
            {(id) => (
              <Input
                id={id}
                value={step.quantization}
                disabled={dialogBusy}
                onChange={(e) => edit("quantization", e.target.value)}
              />
            )}
          </Field>
          <div className="acts start">
            <Button variant="primary" disabled={dialogBusy || !canConfirm(step)} onClick={() => void confirm()}>
              SHA-256 совпадает, установить
            </Button>
            <Button disabled={dialogBusy} onClick={cancelImport}>
              Отмена
            </Button>
          </div>
        </Card>
      )}

      {loadError && (
        <Banner kind="fail" role="alert">
          {loadError}
        </Banner>
      )}

      <h2 className="group-title">Установленные</h2>
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
          className="clist-card"
          classNames={{ button: "btn ghost" }}
          renderContainer={(rows) => (
            <div className="card mrows" role="list" aria-label="Установленные модели">
              {rows}
            </div>
          )}
          renderItem={(m) => (
            <InstalledRow
              model={m}
              badge={defaultBadge(m)}
              busy={busy}
              onDefault={() => void act(`default:${m.id}`, () => api.setDefault(m.id))}
              onAskDelete={() => setDeleting(m.id)}
            />
          )}
        />
      )}
      <Dialog
        open={deletingModel !== null}
        title={texts.deleteTitle}
        confirmLabel="Удалить модель"
        cancelLabel="Отмена"
        danger
        onConfirm={() => {
          const id = deleting;
          setDeleting("");
          void act(`delete:${id}`, () => api.delete(id));
        }}
        onCancel={() => setDeleting("")}
      >
        {deletingModel !== null && (
          <p>
            <bdi className="breakable">{modelTitle(deletingModel)}</bdi>. {texts.deleteConfirm}
          </p>
        )}
      </Dialog>

      <h2 className="group-title">{texts.catalogTitle}</h2>
      {view !== null &&
        (view.available.length === 0 ? (
          <p className="muted">{texts.emptyCatalog}</p>
        ) : (
          <CatalogList
            view={view}
            busy={busy}
            dialogBusy={dialogBusy}
            onDownload={(m) => void download(m)}
            onCancel={(m) => void act(`cancel:${m.catalogId}`, () => api.cancelDownload())}
            onDiscard={(m) => void act(`discard:${m.catalogId}`, () => api.discardPartial(m.catalogId))}
            onImport={(m) => void importCatalog(m)}
            onSource={(m) => void act(`source:${m.catalogId}`, () => api.openSourcePage(m.catalogId))}
          />
        ))}

      <h2 className="group-title">Другие модели</h2>
      <p className="muted">{texts.otherModels}</p>
      <div className="acts start">
        <Button variant="ghost" disabled={busy.has("home")} onClick={openHome}>
          Открыть страницу моделей на Hugging Face
        </Button>
      </div>
      <div className="gap" />
    </section>
  );
}

const badgeKinds: Readonly<Record<string, { kind: ChipKind; icon: IconName }>> = {
  "По умолчанию": { kind: "ok", icon: "ok" },
  Используется: { kind: "run", icon: "wave" },
};

function InstalledRow(props: {
  model: InstalledModel;
  badge: string;
  busy: ReadonlySet<string>;
  onDefault: () => void;
  onAskDelete: () => void;
}) {
  const m = props.model;
  const deleteBusy = props.busy.has(`delete:${m.id}`);
  const chip = Object.hasOwn(badgeKinds, props.badge) ? badgeKinds[props.badge] : null;
  return (
    <div className="mrow" role="listitem">
      <div className="row-main">
        <p className="t">
          <bdi>{modelTitle(m)}</bdi>
        </p>
        <span className="small muted num">
          {sizeText(m.sizeBytes)} · установлена {formatTime(m.installedAt)}
        </span>
        <details className="sha-details">
          <summary className="small muted">
            SHA-256 <code>{shortSha(m.sha256)}</code>
          </summary>
          <code className="sha">
            <bdi>{m.sha256}</bdi>
          </code>
        </details>
      </div>
      <div className="st">
        {chip !== null ? (
          <StatusChip kind={chip.kind} icon={chip.icon}>
            {props.badge}
          </StatusChip>
        ) : (
          <StatusChip kind="neutral" icon="ok">
            Установлена
          </StatusChip>
        )}
      </div>
      <div className="acts">
        {!m.isDefault && (
          <Button variant="ghost" disabled={props.busy.has(`default:${m.id}`)} onClick={props.onDefault}>
            Сделать по умолчанию
          </Button>
        )}
        <Button variant="danger" busy={deleteBusy} busyLabel="Удаление…" disabled={deleteBusy} onClick={props.onAskDelete}>
          Удалить
        </Button>
      </div>
    </div>
  );
}

type CatalogHandler = (m: AvailableModel) => void;

// The catalog with its filters; the filters are presentation only and start with every model shown.
function CatalogList(props: {
  view: ModelsView;
  busy: ReadonlySet<string>;
  dialogBusy: boolean;
  onDownload: CatalogHandler;
  onCancel: CatalogHandler;
  onDiscard: CatalogHandler;
  onImport: CatalogHandler;
  onSource: CatalogHandler;
}) {
  const [lang, setLang] = useState<LanguageFilter>("all");
  const [load, setLoad] = useState<LoadFilter>("any");
  const v = props.view;
  const groups = catalogGroups(v.available, lang, load, v.download);
  return (
    <>
      {!v.machineKnown && <p className="muted">{texts.machineUnknown}</p>}
      <p className="muted">{texts.variantsNote}</p>
      <div className="mfilters">
        <Field label={texts.languageFilter}>
          {(id) => (
            <Select id={id} value={lang} onChange={(e) => setLang(languageFilter(e.target.value))}>
              {languageOptions.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label={texts.loadFilter}>
          {(id) => (
            <Select id={id} value={load} onChange={(e) => setLoad(loadFilter(e.target.value))}>
              {loadOptions.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </Select>
          )}
        </Field>
      </div>
      {groups.length === 0 ? (
        <p className="muted" role="status">
          {texts.noMatch}
        </p>
      ) : (
        <div className="card mrows">
          {groups.map((g) => (
            <Fragment key={g.title}>
              {g.title !== "" && <h3 className="mgroup-title">{g.title}</h3>}
              <div className="mgroup" role="list" aria-label={g.title !== "" ? g.title : texts.catalogTitle}>
                {g.models.map((m) => (
                  <CatalogRow
                    key={m.catalogId}
                    model={m}
                    download={v.download}
                    busy={props.busy}
                    dialogBusy={props.dialogBusy}
                    onDownload={() => props.onDownload(m)}
                    onCancel={() => props.onCancel(m)}
                    onDiscard={() => props.onDiscard(m)}
                    onImport={() => props.onImport(m)}
                    onSource={() => props.onSource(m)}
                  />
                ))}
              </div>
            </Fragment>
          ))}
        </div>
      )}
    </>
  );
}

// Five segments and the words; the words are the accessible name, so the scale is never colour only.
function LoadScale(props: { load: number; heavy: boolean; text: string }) {
  return (
    <span className={props.heavy ? "mload heavy" : "mload"} role="img" aria-label={props.text}>
      <span className="mload-bar" aria-hidden="true">
        {Array.from({ length: loadLevels }, (_, i) => (
          <i key={i} className={i < props.load ? "on" : undefined} />
        ))}
      </span>
      <span className="small" aria-hidden="true">
        {props.text}
      </span>
    </span>
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
  const chip = d !== null ? downloadChip(d) : null;
  const key = (op: string) => props.busy.has(`${op}:${m.catalogId}`);
  const l = catalogLabels(m);
  return (
    <div className="mrow" role="listitem">
      <div className="row-main">
        <p className="t mname">
          <bdi>{l.name}</bdi>
          <span className="msize muted num">{sizeText(m.sizeBytes)}</span>
          {l.recommended !== "" && <span className="mtag">{l.recommended}</span>}
        </p>
        {l.description !== "" && <span className="small">{l.description}</span>}
        <span className="small muted">
          {l.language}
          {l.accuracy !== "" && ` · ${l.accuracy}`}
        </span>
        <span className="small muted" title={l.variantHint}>
          {l.variant}
        </span>
        {l.load !== "" && <LoadScale load={m.load} heavy={m.heavy} text={l.load} />}
        {l.warning !== "" && <span className="small slow">{l.warning}</span>}
        {m.likelySlow && <span className="small slow">{texts.likelySlow}</span>}
        <span className="label">
          <bdi>{m.catalogId}</bdi>
        </span>
      </div>
      <div className="st">
        <span className="chips">
          {m.installed && (
            <StatusChip kind="neutral" icon="ok">
              Установлена
            </StatusChip>
          )}
          {chip !== null && (
            <StatusChip kind={chip.kind} icon={chip.icon}>
              {chip.label}
            </StatusChip>
          )}
          {!m.installed && chip === null && <StatusChip kind="neutral">Не установлена</StatusChip>}
        </span>
        {d !== null && d.phase === "downloading" && d.totalBytes > 0 && (
          <Progress value={progressPercent(d)} label={`Скачивание ${l.name} (${m.catalogId})`} text={false} />
        )}
        {d !== null && (
          <span className={d.phase === "failed" || d.phase === "cancelled" ? "small problem" : "small muted num"}>
            {downloadText(d)}
          </span>
        )}
      </div>
      <div className="acts">
        {a.download && (
          <Button variant="primary" icon="down" disabled={key("download")} onClick={props.onDownload}>
            Скачать
          </Button>
        )}
        {a.cancel && (
          <Button disabled={key("cancel")} onClick={props.onCancel}>
            Остановить скачивание
          </Button>
        )}
        {a.discardPartial && (
          <Button variant="danger" disabled={key("discard")} onClick={props.onDiscard}>
            Удалить недокачанный файл
          </Button>
        )}
        {a.openSource && (
          <Button variant="ghost" disabled={key("source")} onClick={props.onSource}>
            Открыть страницу источника
          </Button>
        )}
        {a.importFile && (
          <Button variant="ghost" disabled={props.dialogBusy} onClick={props.onImport}>
            Импортировать скачанный файл
          </Button>
        )}
      </div>
    </div>
  );
}
