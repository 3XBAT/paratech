import { useEffect, useMemo, useState } from "react";
// The .js extensions let the Node test runner load the compiled links (test/lessonEntry352.test.mjs reads their texts).
import { formatTime } from "../activityView.js";
import { activityApi } from "../bindings/activity.js";
import { callByName } from "../bindings/runtime.js";
import type { LessonTab } from "../knowledgeView.js";
import { useOpenLesson } from "../openLesson.js";
import { rowLessonTab } from "../recordingRowView.js";
import { lessonRecordings } from "../transcriptView.js";
import { Button } from "../ui/Button.js";

interface Link {
  id: string;
  startedAt: string;
  /** The tab the recording opens on: its lecture text, else the summary, else the transcript. */
  tab: LessonTab;
}

export const openPairTexts = {
  one: "Открыть пару",
  /** Several recordings of one pair: told apart by the time. */
  many: (time: string) => `Открыть пару · ${time}`,
} as const;

// «Открыть пару» for each recording of the lesson an assignment came from (TASK-352): the lesson screen holds the summary,
// the lecture text and the transcript as tabs, so there is one link per recording, on the tab the old links named first
// (the lecture text once it is ready, else the summary, else the transcript). Local only: the texts work while the
// homework still waits for the network. The link is offered from the chain the list already has, like the row of
// «Записи». Shows nothing when the lesson has no such recording, or when the list cannot be read (it is an extra here,
// not the page's subject).
export function TranscriptLinks(props: { lessonId: string }) {
  const { lessonId } = props;
  const open = useOpenLesson();
  const [links, setLinks] = useState<Link[]>([]);
  const activity = useMemo(() => activityApi(callByName), []);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const recs = lessonRecordings(await activity.listRecordings(), lessonId);
        if (!live) {
          return;
        }
        const found: Link[] = [];
        for (const r of recs) {
          const tab = rowLessonTab(r);
          if (tab !== null) {
            found.push({ id: r.id, startedAt: r.startedAt, tab });
          }
        }
        setLinks(found);
      } catch {
        // Nothing to offer.
      }
    })();
    return () => {
      live = false;
    };
  }, [activity, lessonId]);

  if (links.length === 0) {
    return null;
  }
  return (
    <div className="acts start tr-links">
      {links.map((l) => (
        <Button key={l.id} icon="doc" onClick={() => open(l.id, l.tab)}>
          {links.length === 1 ? openPairTexts.one : openPairTexts.many(formatTime(l.startedAt))}
        </Button>
      ))}
    </div>
  );
}
