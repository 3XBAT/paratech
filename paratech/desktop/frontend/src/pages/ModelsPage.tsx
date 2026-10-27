import { Fragment, useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
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
  catalogAction,
  catalogActions,
  catalogEntryOf,
  catalogGroups,
  catalogMenu,
  catalogRow,
  confirmation,
  confirmStep,
  defaultBadge,
  downloadChip,
  downloadOf,
  errorText,
  type ImportStep,
  installedMenu,
  installedName,
  installedRow,
  installedText,
  keepsSelection,
  languageFilter,
  type LanguageFilter,
  languageOptions,
  loadFilter,
  type LoadFilter,
  loadHint,
  loadLevels,
  loadOptions,
  loadText,
  type MenuItem,
  type MenuOp,
  type ModelRow,
  progressPercent,
  progressText,
  sizeText,
  statusNotes,
  texts,
} from "../modelsView";
import { Button } from "../ui/Button";
import { SpeechComponentNotice } from "./SpeechComponentNotice";
import { Banner, Card, Dialog, Field, Input, Progress, ResultLine, Select } from "../ui/controls";
import type { IconName } from "../ui/Icon";
import { RowMenu as SharedRowMenu } from "../ui/RowMenu";
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
          setDone(installedText(m, view?.available ?? []));
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
          setDone(installedText(r.model, view?.available ?? []));
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
          <h2>Модели речи</h2>
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
              entry={catalogEntryOf(m, view.available)}
              catalog={view.available}
              badge={defaultBadge(m)}
              busy={busy}
              onDefault={() => void act(`default:${m.id}`, () => api.setDefault(m.id))}
              onSource={(id) => void act(`source:${id}`, () => api.openSourcePage(id))}
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
            <bdi className="breakable">{installedName(deletingModel, view?.available ?? [])}</bdi>. {texts.deleteConfirm}
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
            onAskDelete={(m) => setDeleting(m.installedId)}
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

/** «Small Q5»: the name with the suffix, for the labels of the menu and the progress bar. */
function fullName(r: ModelRow): string {
  return r.suffix !== "" ? `${r.name} ${r.suffix}` : r.name;
}

function InstalledRow(props: {
  model: InstalledModel;
  entry: AvailableModel | null;
  catalog: readonly AvailableModel[];
  badge: string;
  busy: ReadonlySet<string>;
  onDefault: () => void;
  onSource: (catalogId: string) => void;
  onAskDelete: () => void;
}) {
  const m = props.model;
  const r = installedRow(m, props.catalog);
  const deleteBusy = props.busy.has(`delete:${m.id}`);
  const chip = Object.hasOwn(badgeKinds, props.badge) ? badgeKinds[props.badge] : null;
  const catalogId = props.entry?.catalogId ?? "";
  const items = installedMenu(m, props.entry).map((i) => ({
    ...i,
    disabled: i.op === "default" ? props.busy.has(`default:${m.id}`) : props.busy.has(`source:${catalogId}`),
  }));
  return (
    <div className="mrow" role="listitem">
      <ModelText row={r} tip={`${r.tip}\nУстановлена ${formatTime(m.installedAt)}`} />
      <div className="st mst">
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
        <Button variant="danger" busy={deleteBusy} busyLabel="Удаление…" disabled={deleteBusy} onClick={props.onAskDelete}>
          Удалить
        </Button>
      </div>
      <RowMenu
        name={fullName(r)}
        items={items}
        onPick={(op) => (op === "default" ? props.onDefault() : props.onSource(catalogId))}
      />
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
  onAskDelete: CatalogHandler;
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
                    onAskDelete={() => props.onAskDelete(m)}
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

// Five segments and the short words; the accessible name has the full words, so the scale is never colour only.
function LoadScale(props: { load: number; heavy: boolean }) {
  const hint = loadHint(props.load);
  return (
    <span className={props.heavy ? "mload heavy" : "mload"} role="img" aria-label={hint} title={hint}>
      <span className="mload-bar" aria-hidden="true">
        {Array.from({ length: loadLevels }, (_, i) => (
          <i key={i} className={i < props.load ? "on" : undefined} />
        ))}
      </span>
      <span aria-hidden="true">{loadText(props.load)}</span>
    </span>
  );
}

// Name, «Q5», size and the two tags; one muted line; a warning line only for a heavy model. The technical id is only
// in the tooltip. Catalog and file text is plain text in its own <bdi>.
function ModelText(props: { row: ModelRow; tip: string }) {
  const r = props.row;
  return (
    <div className="row-main">
      <p className="t mname" title={props.tip}>
        <bdi className="mname-text">{r.name}</bdi>
        {r.suffix !== "" && (
          <span className="mq" title={texts.q5Hint}>
            {r.suffix}
            <span className="vh">, {texts.q5Hint}</span>
          </span>
        )}
        <span className="msize muted num">{r.size}</span>
        {r.tags.map((t) => (
          <span key={t} className="mtag">
            {t}
          </span>
        ))}
      </p>
      {(r.load > 0 || r.details.length > 0 || r.slow !== "") && (
        <p className="mline small muted">
          {r.load > 0 && <LoadScale load={r.load} heavy={r.heavy} />}
          {r.details.map((d) => (
            <span key={d}>
              <bdi>{d}</bdi>
            </span>
          ))}
          {r.slow !== "" && <span className="slow">{r.slow}</span>}
        </p>
      )}
      {r.warning !== "" && <p className="mwarn small slow">{r.warning}</p>}
      {r.estimate !== "" && (
        <p className={r.estimateHeavy ? "mest small slow" : "mest small muted"} title={r.estimateTip}>
          {r.estimateHeavy ? <b>{r.estimate}</b> : r.estimate}
        </p>
      )}
    </div>
  );
}

// The «⋯» menu of a model row (ui/RowMenu).
function RowMenu(props: {
  name: string;
  items: readonly (MenuItem & { disabled: boolean })[];
  onPick: (op: MenuOp) => void;
}) {
  return (
    <SharedRowMenu
      label={`${texts.more}: ${props.name}`}
      title={texts.more}
      items={props.items.map((i) => ({ key: i.op, label: i.label, disabled: i.disabled }))}
      onPick={props.onPick}
    />
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
  onAskDelete: () => void;
}) {
  const m = props.model;
  const a = catalogActions(m, props.download);
  const action = catalogAction(m, props.download);
  const d = downloadOf(m, props.download);
  const reasonId = useId();
  const chip = d !== null ? downloadChip(d) : null;
  const key = (op: string) => props.busy.has(`${op}:${m.catalogId}`);
  const r = catalogRow(m);
  const deleteBusy = props.busy.has(`delete:${m.installedId}`);
  const items = catalogMenu(m, props.download).map((i) => ({
    ...i,
    disabled: i.op === "import" ? props.dialogBusy : key(i.op),
  }));
  const pick: Readonly<Record<MenuOp, () => void>> = {
    source: props.onSource,
    discard: props.onDiscard,
    import: props.onImport,
    default: () => undefined, // never in a catalog menu
  };
  return (
    <div className="mrow" role="listitem">
      <ModelText row={r} tip={r.tip} />
      <div className="st mst">
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
          <Progress value={progressPercent(d)} label={`Скачивание ${fullName(r)}`} text={false} />
        )}
        {d !== null && (
          <span className={d.phase === "failed" || d.phase === "cancelled" ? "small problem" : "small muted num"}>
            {progressText(d)}
          </span>
        )}
      </div>
      <div className="acts">
        {action === "cancel" && (
          <Button disabled={key("cancel")} onClick={props.onCancel}>
            Отменить
          </Button>
        )}
        {action === "delete" && (
          <Button variant="danger" busy={deleteBusy} busyLabel="Удаление…" disabled={deleteBusy} onClick={props.onAskDelete}>
            Удалить
          </Button>
        )}
        {action === "download" && (
          <Button
            variant="primary"
            icon="down"
            disabled={!a.download || key("download")}
            title={a.download ? undefined : texts.oneDownload}
            aria-describedby={a.download ? undefined : reasonId}
            onClick={props.onDownload}
          >
            Скачать
          </Button>
        )}
        {action === "download" && !a.download && (
          <span id={reasonId} className="vh">
            {texts.oneDownload}
          </span>
        )}
      </div>
      <RowMenu name={fullName(r)} items={items} onPick={(op) => pick[op]()} />
    </div>
  );
}
