import { useEffect, useMemo, useState } from "react";
import { callByName } from "../bindings/runtime";
import { settingsApi, type Settings, type SettingsApi } from "../bindings/settings";
import { Card, ResultLine, Segmented, Toggle as Switch } from "../ui/controls";
import { currentTheme, setTheme, type ThemeChoice } from "../ui/theme";

type Toggle = keyof Settings;

const labels: Record<Toggle, string> = {
  recordingPromptsEnabled: "Автоматически предлагать запись в начале занятия",
  processOnlyOnAcPower: "Обрабатывать только от розетки",
};

const hints: Record<Toggle, string> = {
  recordingPromptsEnabled: "Перед началом пары появится окно с кнопкой «Начать запись».",
  processOnlyOnAcPower: "На батарее расшифровка и конспекты ждут подключения питания и начнутся сами.",
};

const setters: Record<Toggle, Exclude<keyof SettingsApi, "get">> = {
  recordingPromptsEnabled: "setRecordingPromptsEnabled",
  processOnlyOnAcPower: "setProcessOnlyOnAcPower",
};

const themeOptions: readonly { value: ThemeChoice; label: string }[] = [
  { value: "system", label: "Как в системе" },
  { value: "light", label: "Светлая" },
  { value: "dark", label: "Тёмная" },
];

// The page only shows and forwards the values; defaults and rules live in the Go core (ARCH-44). The theme is a
// choice of this window alone, kept in the webview's storage (ADR-0012), so it needs no binding.
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

  async function change(toggle: Toggle, enabled: boolean) {
    setSaving(true);
    setError("");
    try {
      setSettings(await api[setters[toggle]](enabled));
    } catch {
      setError("Не удалось сохранить настройки.");
    } finally {
      setSaving(false);
    }
  }

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
