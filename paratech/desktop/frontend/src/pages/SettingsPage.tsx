import { type CSSProperties, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { callByName } from "../bindings/runtime";
import { recognitionLoads, type RecognitionLoad } from "../bindings/load";
import {
  profileFields,
  settingsApi,
  type Settings,
  type SettingsApi,
  type SettingsToggle,
  type StudentProfile,
} from "../bindings/settings";
import { estimatesTip, estimateTexts, loadDescriptions, loadLabels, settingEstimateLine } from "../durationEstimate";
import { noteRecognitionLanguageHint, noteRecognitionModeFor, noteRecognitionOn, noteRecognitionTexts } from "../noteRecognitionView";
import {
  emptyProfile,
  maxProfileLength,
  profileChanged,
  profileErrorText,
  profileLabels,
  profilePlaceholders,
} from "../profileView";
import { Button } from "../ui/Button";
import { Card, Field, Input, ResultLine, Segmented, Toggle as Switch } from "../ui/controls";
import { type Accent, accents, currentAccent, setAccent } from "../ui/accent";
import { currentTheme, setTheme, type ThemeChoice } from "../ui/theme";

type Toggle = SettingsToggle;

/** The part of «Настройки» a tab shows. */
export type SettingsPart = "settings" | "profile" | "appearance";

const labels: Record<Toggle, string> = {
  recordingPromptsEnabled: "Автоматически предлагать запись в начале занятия",
  processOnlyOnAcPower: "Обрабатывать только от розетки",
  freeWorkNotice: "Предупреждать перед свободной работой",
};

const hints: Record<Toggle, string> = {
  recordingPromptsEnabled: "Перед началом пары появится окно с кнопкой «Начать запись».",
  processOnlyOnAcPower: "На батарее расшифровка ждёт подключения питания и начнётся сама.",
  freeWorkNotice: "Если задания нет, перед работой по теме в панели помощника появится карточка с вопросом.",
};

const setters: Record<Toggle, "setRecordingPromptsEnabled" | "setProcessOnlyOnAcPower" | "setFreeWorkNotice"> = {
  recordingPromptsEnabled: "setRecordingPromptsEnabled",
  processOnlyOnAcPower: "setProcessOnlyOnAcPower",
  freeWorkNotice: "setFreeWorkNotice",
};

const themeOptions: readonly { value: ThemeChoice; label: string }[] = [
  { value: "system", label: "Как в системе" },
  { value: "light", label: "Светлая" },
  { value: "dark", label: "Тёмная" },
];

const loadOptions = recognitionLoads.map((value) => ({ value, label: loadLabels[value] }));

// The page only shows and forwards the values; defaults and rules live in the Go core (ARCH-44). The theme is kept in
// the webview's storage (ADR-0012), so it needs no binding; the popup reads it from there and follows a change.
export function SettingsPage(props: { part?: SettingsPart }) {
  const part = props.part ?? "settings";
  const api = useMemo(() => settingsApi(callByName), []);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [theme, setThemeChoice] = useState<ThemeChoice>(currentTheme);
  const [accent, setAccentChoice] = useState<Accent>(currentAccent);

  useEffect(() => {
    let active = true;
    api.get().then(
      (s) => active && setSettings(s),
      () => active && setError("Не удалось загрузить настройки."),
    );
    return () => {
      active = false;
    };
  }, [api]);

  async function save(action: () => Promise<Settings>) {
    setSaving(true);
    setError("");
    try {
      setSettings(await action());
    } catch {
      setError("Не удалось сохранить настройки.");
    } finally {
      setSaving(false);
    }
  }

  function change(toggle: Toggle, enabled: boolean) {
    return save(() => api[setters[toggle]](enabled));
  }

  // The mode buttons cannot be disabled, so a click while saving or on the current mode is ignored here.
  function chooseLoad(load: RecognitionLoad) {
    if (saving || settings === null || settings.recognitionLoad === load) {
      return;
    }
    void save(() => api.setRecognitionLoad(load));
  }

  const languageHint = settings !== null ? noteRecognitionLanguageHint(settings) : null;
  const estimate =
    settings !== null ? settingEstimateLine(settings.loadEstimates, settings.loadEstimatesModel, settings.recognitionLoad) : null;

  function chooseAccent(a: Accent) {
    setAccent(a);
    setAccentChoice(a);
  }

  function chooseTheme(choice: ThemeChoice) {
    setTheme(choice);
    setThemeChoice(choice);
  }

  const toggleRow = (toggle: Toggle, s: Settings) => (
    <Switch
      label={labels[toggle]}
      checked={s[toggle]}
      disabled={saving}
      hint={hints[toggle]}
      onChange={(checked) => void change(toggle, checked)}
    />
  );

  // «Настройки» has three parts on its tabs: the switches ("settings"), «Оформление» and «Профиль студента».
  if (part === "settings") {
    return (
      <section className="page">
        {settings === null && !error && <p className="muted">Загрузка…</p>}
        {settings !== null && (
          <>
            <SettingsGroup caption="Запись">{toggleRow("recordingPromptsEnabled", settings)}</SettingsGroup>
            <SettingsGroup caption="Обработка">
              {toggleRow("processOnlyOnAcPower", settings)}
              <div className="set">
                <span>{estimateTexts.setting}</span>
                <Segmented
                  label={estimateTexts.setting}
                  options={loadOptions}
                  value={settings.recognitionLoad}
                  onChange={chooseLoad}
                />
                <ul className="small muted load-modes">
                  {recognitionLoads.map((l) => (
                    <li key={l}>
                      «{loadLabels[l]}» — {loadDescriptions[l]}
                    </li>
                  ))}
                </ul>
                <span className="small muted">{estimateTexts.nextRun}</span>
                {estimate !== null &&
                  (estimate.heavy ? (
                    <span className="small slow" title={estimatesTip(settings.loadEstimates)}>
                      <b>{estimate.text}</b>
                    </span>
                  ) : (
                    <span className="small muted" title={estimatesTip(settings.loadEstimates)}>
                      {estimate.text}
                    </span>
                  ))}
              </div>
            </SettingsGroup>
            <SettingsGroup caption="Помощник">{toggleRow("freeWorkNotice", settings)}</SettingsGroup>
            <SettingsGroup caption={noteRecognitionTexts.caption}>
              <Switch
                label={noteRecognitionTexts.label}
                checked={noteRecognitionOn(settings)}
                disabled={saving}
                hint={noteRecognitionTexts.hint}
                onChange={(checked) => void save(() => api.setNoteRecognition(noteRecognitionModeFor(checked)))}
              />
              {languageHint !== null && (
                <div className="set">
                  <span className="small slow" role="status">
                    {languageHint}
                  </span>
                </div>
              )}
            </SettingsGroup>
          </>
        )}
        {/* While a change is saved every switch waits for it; this line says why they are dimmed. */}
        {saving && (
          <p className="small muted group-foot" role="status">
            Сохраняется…
          </p>
        )}
        {error && (
          <ResultLine kind="fail" role="alert">
            {error}
          </ResultLine>
        )}
      </section>
    );
  }

  if (part === "profile") {
    return (
      <section className="page">
        <div className="head">
          <div>
            <span className="label">Для титульного листа домашнего задания</span>
            <h2>Профиль</h2>
          </div>
        </div>
        <ProfileCard api={api} />
      </section>
    );
  }

  // part === "appearance"
  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">Хранится на этом компьютере</span>
          <h2>Оформление</h2>
        </div>
      </div>
      <SettingsGroup caption="Оформление">
        <div className="set">
          <span>Тема</span>
          <Segmented label="Тема" options={themeOptions} value={theme} onChange={chooseTheme} />
          <span className="small muted">«Как в системе» следует настройке Windows: светлый или тёмный режим.</span>
        </div>
        <div className="set">
          <span>
            Цвет <span className="sw-name small muted">· {accent.name}</span>
          </span>
          <div className="swatches" role="radiogroup" aria-label="Акцентный цвет">
            {accents.map((a) => (
              <button
                key={a.id}
                type="button"
                className="sw"
                role="radio"
                aria-checked={a.id === accent.id}
                aria-label={a.name}
                title={a.name}
                style={{ "--c": a.hex } as CSSProperties}
                onClick={() => chooseAccent(a)}
              />
            ))}
          </div>
        </div>
      </SettingsGroup>
    </section>
  );
}

// One settings group: the caption above, its rows in a card.
function SettingsGroup(props: { caption: string; children: ReactNode }) {
  return (
    <div className="gsec">
      <h2 className="group-cap">{props.caption}</h2>
      <Card className="settings" label={props.caption}>
        {props.children}
      </Card>
    </div>
  );
}

// «Профиль студента» (TASK-094): the title page of a homework Word file names these. Unlike the switches it is saved
// with a button, so a half-typed name is not stored.
function ProfileCard(props: { api: SettingsApi }) {
  const { api } = props;
  const [stored, setStored] = useState<StudentProfile | null>(null);
  const [form, setForm] = useState<StudentProfile>(emptyProfile);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const running = useRef(false); // a double click comes before the re-render that disables the button

  useEffect(() => {
    let active = true;
    api.getProfile().then(
      (p) => {
        if (active) {
          setStored(p);
          setForm(p);
        }
      },
      () => active && setError("Не удалось загрузить профиль."),
    );
    return () => {
      active = false;
    };
  }, [api]);

  async function onSave() {
    if (running.current) {
      return;
    }
    running.current = true;
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      const p = await api.saveProfile(form);
      setStored(p);
      setForm(p);
      setSaved(true);
    } catch (err) {
      setError(profileErrorText(err));
    } finally {
      running.current = false;
      setBusy(false);
    }
  }

  const changed = stored !== null && profileChanged(form, stored);
  // iOS-style group: the caption above it, the explanation under it, the action below the group.
  return (
    <div className="gsec">
      <h2 className="group-cap">Профиль студента</h2>
      <Card className="settings profile" label="Профиль студента">
        {stored === null && !error && <p className="muted">Загрузка…</p>}
        {stored !== null &&
          profileFields.map((f) => (
            <Field key={f} label={profileLabels[f]}>
              {(fid) => (
                <Input
                  id={fid}
                  value={form[f]}
                  maxLength={maxProfileLength}
                  placeholder={profilePlaceholders[f]}
                  readOnly={busy}
                  onChange={(e) => {
                    const v = e.target.value;
                    setSaved(false);
                    setForm((p) => ({ ...p, [f]: v }));
                  }}
                />
              )}
            </Field>
          ))}
      </Card>
      <p className="small muted group-foot">
        Для титульного листа файла Word с домашним заданием. Все поля необязательные: пустые на титульный лист не
        попадут.
      </p>
      {stored !== null && (
        <div className="acts start group-acts">
          <Button variant="primary" busy={busy} busyLabel="Сохранение…" disabled={busy || !changed} onClick={() => void onSave()}>
            Сохранить профиль
          </Button>
        </div>
      )}
      {saved && (
        <ResultLine kind="ok" role="status">
          Профиль сохранён.
        </ResultLine>
      )}
      {error && (
        <ResultLine kind="fail" role="alert">
          {error}
        </ResultLine>
      )}
    </div>
  );
}
