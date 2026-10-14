import { useEffect, useMemo, useState } from "react";
import { callByName } from "../bindings/runtime";
import { settingsApi, type Settings } from "../bindings/settings";

type Toggle = keyof Settings;

const labels: Record<Toggle, string> = {
  recordingPromptsEnabled: "Автоматически предлагать запись в начале занятия",
  processOnlyOnAcPower: "Обрабатывать только от розетки",
};

// The page only shows and forwards the values; defaults and rules live in the Go core (ARCH-44).
export function SettingsPage() {
  const api = useMemo(() => settingsApi(callByName), []);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

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
      const saved =
        toggle === "recordingPromptsEnabled"
          ? await api.setRecordingPromptsEnabled(enabled)
          : await api.setProcessOnlyOnAcPower(enabled);
      setSettings(saved);
    } catch {
      setError("Не удалось сохранить настройки.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <section>
      <h1>Настройки</h1>
      {settings === null && !error && <p className="muted">Загрузка…</p>}
      {settings !== null &&
        (Object.keys(labels) as Toggle[]).map((toggle) => (
          <label key={toggle} className="toggle">
            <input
              type="checkbox"
              checked={settings[toggle]}
              disabled={saving}
              onChange={(e) => void change(toggle, e.target.checked)}
            />
            {labels[toggle]}
          </label>
        ))}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
