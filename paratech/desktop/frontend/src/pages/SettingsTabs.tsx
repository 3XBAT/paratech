import { type PageId, settingsPages, settingsTabLabels } from "../nav.js";
import { Icon, type IconName } from "../ui/Icon.js";

export const settingsHead = { lead: "Изменения сохраняются сразу", title: "Настройки" } as const;

const sectionIcons: Readonly<Record<string, IconName>> = {
  settings: "set",
  models: "model",
  connections: "link",
  diagnostics: "diag",
  profile: "doc",
  appearance: "wave",
};

/** The shared head of «Настройки»: one over-line and title for all its sections. */
export function SettingsHead() {
  return (
    <div className="head">
      <div>
        <span className="label">{settingsHead.lead}</span>
        <h1>{settingsHead.title}</h1>
      </div>
    </div>
  );
}

/** The left menu of «Настройки»: the general settings and the screens that moved inside (models, connections, ...). */
export function SettingsTabs(props: { current: PageId; onOpen: (id: PageId) => void }) {
  return (
    <nav className="set-nav" aria-label="Разделы настроек">
      {settingsPages.map((id) => (
        <button key={id} type="button" aria-current={id === props.current ? "page" : undefined} onClick={() => props.onOpen(id)}>
          <Icon name={sectionIcons[id] ?? "set"} />
          {settingsTabLabels[id]}
        </button>
      ))}
    </nav>
  );
}
