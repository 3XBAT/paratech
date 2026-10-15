import { useState } from "react";
import { defaultPage, pageById, pages, type PageId } from "./nav";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { ModelsPage } from "./pages/ModelsPage";
import { Placeholder } from "./pages/Placeholder";
import { RecordingsPage } from "./pages/RecordingsPage";
import { SchedulePage } from "./pages/SchedulePage";
import { SettingsPage } from "./pages/SettingsPage";

export function App() {
  const [current, setCurrent] = useState<PageId>(defaultPage);
  const page = pageById(current);

  return (
    <div className="layout">
      <nav className="nav">
        {pages.map((p) => (
          <button
            key={p.id}
            type="button"
            className={p.id === current ? "active" : ""}
            aria-current={p.id === current ? "page" : undefined}
            onClick={() => setCurrent(p.id)}
          >
            {p.label}
          </button>
        ))}
      </nav>
      <main className="content">
        {page.id === "settings" ? (
          <SettingsPage />
        ) : page.id === "recordings" ? (
          <RecordingsPage />
        ) : page.id === "models" ? (
          <ModelsPage />
        ) : page.id === "schedule" ? (
          <SchedulePage />
        ) : page.id === "connections" ? (
          <ConnectionsPage />
        ) : (
          <Placeholder title={page.label} />
        )}
      </main>
    </div>
  );
}
