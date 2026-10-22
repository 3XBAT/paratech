import { type PageId, settingsPages, settingsTabLabels } from "../nav.js";

/** The tabs of «Настройки»: the general settings and the screens that moved inside (models, connections, ...). */
export function SettingsTabs(props: { current: PageId; onOpen: (id: PageId) => void }) {
  return (
    <nav className="set-tabs" aria-label="Разделы настроек">
      {settingsPages.map((id) => (
        <button key={id} type="button" aria-current={id === props.current ? "page" : undefined} onClick={() => props.onOpen(id)}>
          {settingsTabLabels[id]}
        </button>
      ))}
    </nav>
  );
}
