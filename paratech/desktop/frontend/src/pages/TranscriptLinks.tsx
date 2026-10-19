import { useEffect, useMemo, useState } from "react";
import { formatTime } from "../activityView";
import { activityApi, type RecordingItem } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import { transcriptApi } from "../bindings/transcript";
import { useOpenSummary } from "../openSummary";
import { useOpenTranscript } from "../openTranscript";
import { rowLinks } from "../recordingRowView";
import { rowSummaryOffer } from "../summaryView";
import { canOpenTranscript, lessonRecordings } from "../transcriptView";
import { Button } from "../ui/Button";

interface Link {
  id: string;
  startedAt: string;
  /** The recording has a lecture text: its transcript is only the source (TASK-090). */
  lecture: boolean;
}

interface Links {
  lectures: Link[];
  summaries: Link[];
  transcripts: Link[];
}

const noLinks: Links = { lectures: [], summaries: [], transcripts: [] };

// «Открыть текст лекции» (TASK-090), «Открыть конспект» (TASK-087) and «Открыть расшифровку» (TASK-079) for the
// recordings of a lesson; next to a lecture text the transcript is the small «Исходная расшифровка». Local only: the
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
        const link = (r: RecordingItem): Link => ({ id: r.id, startedAt: r.startedAt, lecture: rowLinks(r).lectureText });
        const transcripts: Link[] = [];
        sts.forEach((s, i) => {
          if (s.status === "fulfilled" && canOpenTranscript(s.value)) {
            transcripts.push(link(recs[i]));
          }
        });
        const lectures = recs.map(link).filter((l) => l.lecture);
        const summaries = recs.filter((r) => rowSummaryOffer(r) !== null).map(link);
        setLinks({ lectures, summaries, transcripts });
      } catch {
        // Nothing to offer.
      }
    })();
    return () => {
      live = false;
    };
  }, [apis, lessonId]);

  const { lectures, summaries, transcripts } = links;
  if (lectures.length === 0 && summaries.length === 0 && transcripts.length === 0) {
    return null;
  }
  const several = (text: string, all: readonly Link[], l: Link) =>
    all.length === 1 ? text : `${text} · ${formatTime(l.startedAt)}`;
  return (
    <div className="acts start tr-links">
      {lectures.map((l) => (
        <Button key={`l:${l.id}`} icon="doc" onClick={() => openSummary(l.id, "lecture")}>
          {several("Открыть текст лекции", lectures, l)}
        </Button>
      ))}
      {summaries.map((l) => (
        <Button key={`s:${l.id}`} icon="doc" onClick={() => openSummary(l.id)}>
          {summaries.length === 1 ? "Открыть конспект" : `Открыть конспект · ${formatTime(l.startedAt)}`}
        </Button>
      ))}
      {transcripts.map((l) =>
        l.lecture ? (
          <Button key={`t:${l.id}`} variant="ghost" className="row-source" onClick={() => open(l.id)}>
            {several("Исходная расшифровка", transcripts, l)}
          </Button>
        ) : (
          <Button key={`t:${l.id}`} icon="doc" onClick={() => open(l.id)}>
            {several("Открыть расшифровку", transcripts, l)}
          </Button>
        ),
      )}
    </div>
  );
}
