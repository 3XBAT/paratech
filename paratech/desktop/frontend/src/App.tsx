import { type CSSProperties, useCallback, useState } from "react";
import {
  closeTranscript,
  initialRoute,
  type PageId,
  railItemOf,
  routeToAssignment,
  routeToPage,
  routeToSummary,
  routeToTranscript,
} from "./nav";
import { AgentPanel } from "./agent/AgentPanel";
import { PanelResizeHandle, useResizablePanel } from "./agent/PanelResizeHandle";
import { OpenSummaryContext, type SummarySection } from "./openSummary";
import { OpenTranscriptContext } from "./openTranscript";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { DiagnosticsPage } from "./pages/DiagnosticsPage";
import { HomeworkPage } from "./pages/HomeworkPage";
import { KnowledgeBasePage } from "./pages/KnowledgeBasePage";
import { ModelsPage } from "./pages/ModelsPage";
import { DropZone } from "./pages/DropZone";
import { JournalPage } from "./pages/JournalPage";
import { SchedulePage } from "./pages/SchedulePage";
import { SettingsPage } from "./pages/SettingsPage";
import { SettingsTabs } from "./pages/SettingsTabs";
import { SubjectsPage } from "./pages/SubjectsPage";
import { SummaryPage } from "./pages/SummaryPage";
import { TodayPage } from "./pages/TodayPage";
import { TranscriptPage } from "./pages/TranscriptPage";
import { IconSprite } from "./ui/Icon";
import { Rail } from "./ui/Rail";

function PageView(props: {
  id: PageId;
  visit: number;
  assignment: string;
  /** Counts the files added elsewhere (a drop on the window): the library pages read again. */
  library: number;
  onOpen: (id: PageId) => void;
  onOpenAssignment: (id: string) => void;
}) {
  switch (props.id) {
    case "today":
      return <TodayPage onOpen={props.onOpen} onOpenAssignment={props.onOpenAssignment} />;
    case "subjects":
      return <SubjectsPage onOpen={props.onOpen} reload={props.library} visit={props.visit} />;
    case "journal":
      return <JournalPage />;
    case "homework":
      return <HomeworkPage openAssignment={props.assignment} />;
    case "knowledge":
      return <KnowledgeBasePage visit={props.visit} onOpenAssignment={props.onOpenAssignment} />;
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
    case "profile":
      return <SettingsPage part="profile" />;
    case "appearance":
      return <SettingsPage part="appearance" />;
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
  // Counts the files the drop zone added: «Предметы» reads the library again.
  const [library, setLibrary] = useState(0);
  const panel = useResizablePanel();
  const { open, width } = panel.pref;
  const overlay = route.transcript !== "" || route.summary !== "";

  return (
    <div
      className={`shell with-agent${open ? "" : " agent-closed"}${panel.dragging ? " agent-dragging" : ""}`}
      style={{ "--ag-w": `${open ? width : 0}px` } as CSSProperties}
    >
      <IconSprite />
      <Rail
        current={current}
        onOpen={(id) => {
          openPage(id);
          setVisit((v) => v + 1);
        }}
      />
      <main className="sheet">
        {/* A new key per section replays the entry animation. The section stays mounted under an open transcript or
            summary, so «Назад» finds it as the user left it. */}
        <div key={`${current}:${jump}`} className="screen" hidden={overlay}>
          {railItemOf(current).id === "settings" && <SettingsTabs current={current} onOpen={openPage} />}
          <OpenTranscriptContext.Provider value={openTranscript}>
            <OpenSummaryContext.Provider value={openSummary}>
              <PageView
                id={current}
                visit={visit}
                assignment={route.assignment}
                library={library}
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
              onDeleted={() => openPage("today")}
              onCheckSound={() => openPage("diagnostics")}
            />
          </div>
        )}
      </main>
      <AgentPanel page={current} assignmentId={route.assignment} reveal={panel.reveal} />
      <PanelResizeHandle panel={panel} />
      <DropZone onImported={() => setLibrary((n) => n + 1)} />
    </div>
  );
}
