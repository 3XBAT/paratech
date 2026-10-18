import { useCallback, useState } from "react";
import { bottomGroupStart, closeTranscript, initialRoute, type PageId, pages, routeToPage, routeToTranscript } from "./nav";
import { OpenTranscriptContext } from "./openTranscript";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { HomeworkPage } from "./pages/HomeworkPage";
import { ModelsPage } from "./pages/ModelsPage";
import { RecordingsPage } from "./pages/RecordingsPage";
import { SchedulePage } from "./pages/SchedulePage";
import { SettingsPage } from "./pages/SettingsPage";
import { TranscriptPage } from "./pages/TranscriptPage";
import { Icon, IconSprite } from "./ui/Icon";

function PageView(props: { id: PageId; visit: number; onOpen: (id: PageId) => void }) {
  switch (props.id) {
    case "recordings":
      return <RecordingsPage onOpen={props.onOpen} />;
    case "homework":
      return <HomeworkPage />;
    case "schedule":
      return <SchedulePage />;
    case "connections":
      return <ConnectionsPage visit={props.visit} />;
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
  const [route, setRoute] = useState(initialRoute);
  // Counts the rail clicks: a click on the current section takes a page with sub-pages back to its first level.
  const [visit, setVisit] = useState(0);
  const current = route.page;
  const openTranscript = useCallback((id: string) => setRoute((r) => routeToTranscript(r, id)), []);
  const openPage = useCallback((id: PageId) => setRoute(routeToPage(id)), []);

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
            onClick={() => {
              openPage(p.id);
              setVisit((v) => v + 1);
            }}
          >
            <Icon name={p.icon} />
            {p.label}
          </button>
        ))}
      </nav>
      <main className="sheet">
        {/* A new key per section replays the entry animation. The section stays mounted under an open transcript, so
            «Назад» finds it as the user left it. */}
        <div key={current} className="screen" hidden={route.transcript !== ""}>
          <OpenTranscriptContext.Provider value={openTranscript}>
            <PageView id={current} visit={visit} onOpen={openPage} />
          </OpenTranscriptContext.Provider>
        </div>
        {route.transcript !== "" && (
          <div key={`transcript:${route.transcript}`} className="screen">
            <TranscriptPage recordingId={route.transcript} onBack={() => setRoute(closeTranscript)} />
          </div>
        )}
      </main>
    </div>
  );
}
