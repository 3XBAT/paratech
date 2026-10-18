import { useEffect, useMemo, useState } from "react";
import { formatTime } from "../activityView";
import { activityApi } from "../bindings/activity";
import { callByName } from "../bindings/runtime";
import { transcriptApi } from "../bindings/transcript";
import { useOpenTranscript } from "../openTranscript";
import { canOpenTranscript, lessonRecordings } from "../transcriptView";
import { Button } from "../ui/Button";

interface Link {
  id: string;
  startedAt: string;
}

// «Открыть расшифровку» for the recordings of a lesson (TASK-079). Local only: it works while the summary and the
// homework still wait for the network. Shows nothing when the lesson has no recording with recognised text, or when
// the lists cannot be read (the transcript is an extra here, not the page's subject).
export function TranscriptLinks(props: { lessonId: string }) {
  const { lessonId } = props;
  const open = useOpenTranscript();
  const [links, setLinks] = useState<Link[]>([]);
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
        const out: Link[] = [];
        sts.forEach((s, i) => {
          if (s.status === "fulfilled" && canOpenTranscript(s.value)) {
            out.push({ id: recs[i].id, startedAt: recs[i].startedAt });
          }
        });
        setLinks(out);
      } catch {
        // Nothing to offer.
      }
    })();
    return () => {
      live = false;
    };
  }, [apis, lessonId]);

  if (links.length === 0) {
    return null;
  }
  return (
    <div className="acts start tr-links">
      {links.map((l) => (
        <Button key={l.id} icon="doc" onClick={() => open(l.id)}>
          {links.length === 1 ? "Открыть расшифровку" : `Открыть расшифровку · ${formatTime(l.startedAt)}`}
        </Button>
      ))}
    </div>
  );
}
