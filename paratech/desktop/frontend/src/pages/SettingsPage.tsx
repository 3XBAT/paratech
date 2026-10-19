import { useEffect, useMemo, useState } from "react";
import { callByName } from "../bindings/runtime";
import { recognitionLoads, type RecognitionLoad } from "../bindings/load";
import { settingsApi, type Settings, type SettingsApi, type SettingsToggle } from "../bindings/settings";
import { estimatesTip, estimateTexts, loadDescriptions, loadLabels, settingEstimateLine } from "../durationEstimate";
import { Card, ResultLine, Segmented, Toggle as Switch } from "../ui/controls";
import { currentTheme, setTheme, type ThemeChoice } from "../ui/theme";

type Toggle = SettingsToggle;

const labels: Record<Toggle, string> = {
  recordingPromptsEnabled: "Автоматически предлагать запись в начале занятия",
  processOnlyOnAcPower: "Обрабатывать только от розетки",
};

const hints: Record<Toggle, string> = {
  recordingPromptsEnabled: "Перед началом пары появится окно с кнопкой «Начать запись».",
  processOnlyOnAcPower: "На батарее расшифровка ждёт подключения питания и начнётся сама.",
};

const setters: Record<Toggle, Exclude<keyof SettingsApi, "get" | "setRecognitionLoad">> = {
  recordingPromptsEnabled: "setRecordingPromptsEnabled",
  processOnlyOnAcPower: "setProcessOnlyOnAcPower",
};

const themeOptions: readonly { value: ThemeChoice; label: string }[] = [
  { value: "system", label: "Как в системе" },
  { value: "light", label: "Светлая" },
  { value: "dark", label: "Тёмная" },
];

const loadOptions = recognitionLoads.map((value) => ({ value, label: loadLabels[value] }));

// The page only shows and forwards the values; defaults and rules live in the Go core (ARCH-44). The theme is kept in
// the webview's storage (ADR-0012), so it needs no binding; the popup reads it from there and follows a change.
export function SettingsPage() {
  const api = useMemo(() => settingsApi(callByName), []);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [theme, setThemeChoice] = useState<ThemeChoice>(currentTheme);

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

  const estimate =
    settings !== null ? settingEstimateLine(settings.loadEstimates, settings.loadEstimatesModel, settings.recognitionLoad) : null;

  function chooseTheme(choice: ThemeChoice) {
    setTheme(choice);
    setThemeChoice(choice);
  }

  return (
    <section className="page">
      <div className="head">
        <div>
          <span className="label">Изменения сохраняются сразу</span>
          <h1>Настройки</h1>
        </div>
      </div>
      <Card className="box settings" label="Настройки">
        {settings === null && !error && <p className="muted">Загрузка…</p>}
        {settings !== null &&
          (Object.keys(labels) as Toggle[]).map((toggle) => (
            <Switch
              key={toggle}
              label={labels[toggle]}
              checked={settings[toggle]}
              disabled={saving}
              hint={hints[toggle]}
              onChange={(checked) => void change(toggle, checked)}
            />
          ))}
        {settings !== null && (
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
        )}
        {/* While a change is saved every switch waits for it; this line says why they are dimmed. */}
        {saving && (
          <p className="small muted" role="status">
            Сохраняется…
          </p>
        )}
        {error && (
          <ResultLine kind="fail" role="alert">
            {error}
          </ResultLine>
        )}
        <div className="set">
          <span>Тема</span>
          <Segmented label="Тема" options={themeOptions} value={theme} onChange={chooseTheme} />
          <span className="small muted">«Как в системе» следует настройке Windows: светлый или тёмный режим.</span>
        </div>
      </Card>
    </section>
  );
}
