import { type PageId, settingsAdvancedCaption, settingsAdvancedPages, settingsMainPages, settingsTabLabels } from "../nav.js";
import { Icon, type IconName } from "../ui/Icon.js";

export const settingsHead = { title: "Настройки" } as const;

const sectionIcons: Readonly<Record<string, IconName>> = {
  settings: "set",
  models: "model",
  connections: "link",
  diagnostics: "diag",
  profile: "doc",
  appearance: "wave",
};

/** The shared head of «Настройки»: one title for all its sections. */
export function SettingsHead() {
  return (
    <div className="head">
      <div>
        <h1>{settingsHead.title}</h1>
      </div>
    </div>
  );
}

/**
 * The left menu of «Настройки»: the main sections, then under a divider with the caption «Дополнительно» the support
 * tools (models, diagnostics). The caption names the second group for a screen reader.
 */
export function SettingsTabs(props: { current: PageId; onOpen: (id: PageId) => void }) {
  const tab = (id: PageId) => (
    <button key={id} type="button" aria-current={id === props.current ? "page" : undefined} onClick={() => props.onOpen(id)}>
      <Icon name={sectionIcons[id] ?? "set"} />
      {settingsTabLabels[id]}
    </button>
  );
  return (
    <nav className="set-nav" aria-label="Разделы настроек">
      {settingsMainPages.map(tab)}
      <div className="set-nav-group" role="group" aria-labelledby="set-nav-advanced">
        <span className="set-nav-caption" id="set-nav-advanced">
          {settingsAdvancedCaption}
        </span>
        {settingsAdvancedPages.map(tab)}
      </div>
    </nav>
  );
}
