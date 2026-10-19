import { useEffect, useMemo, useState } from "react";
import { formatTime } from "../activityView";
import { activityApi } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import { transcriptApi } from "../bindings/transcript";
import { useOpenSummary } from "../openSummary";
import { useOpenTranscript } from "../openTranscript";
import { rowSummaryOffer } from "../summaryView";
import { canOpenTranscript, lessonRecordings } from "../transcriptView";
import { Button } from "../ui/Button";

interface Link {
  id: string;
  startedAt: string;
}

interface Links {
  summaries: Link[];
  transcripts: Link[];
}

const noLinks: Links = { summaries: [], transcripts: [] };

// «Открыть конспект» (TASK-087) and «Открыть расшифровку» (TASK-079) for the recordings of a lesson. Local only: the
// transcript works while the summary and the homework still wait for the network. The summary is offered from the
// chain the list already has, like the row of «Записи и задачи». Shows nothing when the lesson has no such recording,
// or when the lists cannot be read (they are extras here, not the page's subject).
export function TranscriptLinks(props: { lessonId: string }) {
  const { lessonId } = props;
  const open = useOpenTranscript();
  const openSummary = useOpenSummary();
  const [links, setLinks] = useState<Links>(noLinks);
  const apis = useMemo(() => ({ activity: activityApi(callByName), transcript: transcriptApi(callByName) }), []);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const recs = lessonRecordings(await apis.activity.listRecordings(), lessonId);
        const sts = await Promise.allSettled(recs.map((r) => apis.transcript.status(r.id)));
        if (!live) {
          return;
        }
        const transcripts: Link[] = [];
        sts.forEach((s, i) => {
          if (s.status === "fulfilled" && canOpenTranscript(s.value)) {
            transcripts.push({ id: recs[i].id, startedAt: recs[i].startedAt });
          }
        });
        const summaries = recs
          .filter((r) => rowSummaryOffer(r) !== null)
          .map((r) => ({ id: r.id, startedAt: r.startedAt }));
        setLinks({ summaries, transcripts });
      } catch {
        // Nothing to offer.
      }
    })();
    return () => {
      live = false;
    };
  }, [apis, lessonId]);

  const { summaries, transcripts } = links;
  if (summaries.length === 0 && transcripts.length === 0) {
    return null;
  }
  return (
    <div className="acts start tr-links">
      {summaries.map((l) => (
        <Button key={`s:${l.id}`} icon="doc" onClick={() => openSummary(l.id)}>
          {summaries.length === 1 ? "Открыть конспект" : `Открыть конспект · ${formatTime(l.startedAt)}`}
        </Button>
      ))}
      {transcripts.map((l) => (
        <Button key={`t:${l.id}`} icon="doc" onClick={() => open(l.id)}>
          {transcripts.length === 1 ? "Открыть расшифровку" : `Открыть расшифровку · ${formatTime(l.startedAt)}`}
        </Button>
      ))}
    </div>
  );
}
