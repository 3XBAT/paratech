import { useCallback, useState } from "react";
import {
  bottomGroupStart,
  closeTranscript,
  initialRoute,
  type PageId,
  pages,
  routeToAssignment,
  routeToPage,
  routeToSummary,
  routeToTranscript,
} from "./nav";
import { OpenSummaryContext, type SummarySection } from "./openSummary";
import { OpenTranscriptContext } from "./openTranscript";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { HomeworkPage } from "./pages/HomeworkPage";
import { ModelsPage } from "./pages/ModelsPage";
import { RecordingsPage } from "./pages/RecordingsPage";
import { SchedulePage } from "./pages/SchedulePage";
import { SettingsPage } from "./pages/SettingsPage";
import { SummaryPage } from "./pages/SummaryPage";
import { TranscriptPage } from "./pages/TranscriptPage";
import { Icon, IconSprite } from "./ui/Icon";

function PageView(props: {
  id: PageId;
  visit: number;
  assignment: string;
  onOpen: (id: PageId) => void;
  onOpenAssignment: (id: string) => void;
}) {
  switch (props.id) {
    case "recordings":
      return <RecordingsPage onOpen={props.onOpen} onOpenAssignment={props.onOpenAssignment} />;
    case "homework":
      return <HomeworkPage openAssignment={props.assignment} />;
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
  // Where the summary page opens (TASK-090); not part of the route: «Назад» and a new open reset it.
  const [summarySection, setSummarySection] = useState<SummarySection | undefined>(undefined);
  const openSummary = useCallback((id: string, section?: SummarySection) => {
    setSummarySection(section);
    setRoute((r) => routeToSummary(r, id));
  }, []);
  const openPage = useCallback((id: PageId) => setRoute(routeToPage(id)), []);
  // Counts the jumps to an assignment: each one opens «Домашние задания» afresh on that assignment.
  const [jump, setJump] = useState(0);
  const openAssignment = useCallback((id: string) => {
    setRoute(routeToAssignment(id));
    setJump((j) => j + 1);
  }, []);
  const overlay = route.transcript !== "" || route.summary !== "";

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
        {/* A new key per section replays the entry animation. The section stays mounted under an open transcript or
            summary, so «Назад» finds it as the user left it. */}
        <div key={`${current}:${jump}`} className="screen" hidden={overlay}>
          <OpenTranscriptContext.Provider value={openTranscript}>
            <OpenSummaryContext.Provider value={openSummary}>
              <PageView
                id={current}
                visit={visit}
                assignment={route.assignment}
                onOpen={openPage}
                onOpenAssignment={openAssignment}
              />
            </OpenSummaryContext.Provider>
          </OpenTranscriptContext.Provider>
        </div>
        {route.transcript !== "" && (
          <div key={`transcript:${route.transcript}`} className="screen">
            <TranscriptPage
              recordingId={route.transcript}
              onBack={() => setRoute(closeTranscript)}
              onOpenSummary={openSummary}
            />
          </div>
        )}
        {route.summary !== "" && (
          <div key={`summary:${route.summary}`} className="screen">
            <SummaryPage
              recordingId={route.summary}
              section={summarySection}
              onBack={() => setRoute(closeTranscript)}
              onOpenTranscript={openTranscript}
              onOpenAssignment={openAssignment}
              onDeleted={() => openPage("recordings")}
            />
          </div>
        )}
      </main>
    </div>
  );
}
