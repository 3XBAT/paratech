import { useState } from "react";
import { bottomGroupStart, defaultPage, type PageId, pages } from "./nav";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { ModelsPage } from "./pages/ModelsPage";
import { RecordingsPage } from "./pages/RecordingsPage";
import { SchedulePage } from "./pages/SchedulePage";
import { SettingsPage } from "./pages/SettingsPage";
import { Icon, IconSprite } from "./ui/Icon";

function PageView(props: { id: PageId }) {
  switch (props.id) {
    case "recordings":
      return <RecordingsPage />;
    case "schedule":
      return <SchedulePage />;
    case "connections":
      return <ConnectionsPage />;
    case "models":
      return <ModelsPage />;
    case "diagnostics":
      return <DiagnosticsPage />;
    case "settings":
      return <SettingsPage />;
  }
}

// The window: a quiet side rail on the window background and the current section on a sheet (docs/design/brief.md 5).
export function App() {
  const [current, setCurrent] = useState<PageId>(defaultPage);

  return (
    <div className="shell">
      <IconSprite />
      <nav className="rail" aria-label="Разделы">
        {pages.map((p) => (
          <button
            key={p.id}
            type="button"
            className={p.id === bottomGroupStart ? "low" : undefined}
            aria-current={p.id === current ? "page" : undefined}
            onClick={() => setCurrent(p.id)}
          >
            <Icon name={p.icon} />
            {p.label}
          </button>
        ))}
      </nav>
      <main className="sheet">
        {/* A new key per section replays the entry animation. */}
        <div key={current} className="screen">
          <PageView id={current} />
        </div>
      </main>
    </div>
  );
}
