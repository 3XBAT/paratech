// «Текст лекции» on the summary page (TASK-090): the state, the blocks with their times, copy and save. Headings and
// paragraphs are model output: React text nodes only, no HTML, no markdown, no links from them.

// The .js extensions let the Node test runner load the compiled modules (test/lecturetext.test.mjs renders these parts).
import type { SummarySaveFormat, SummaryView } from "../bindings/summary.js";
import {
  lectureBlocks,
  lectureEarlierNote,
  lectureLine,
  lectureStartLabel,
  lectureUpdatedText,
  showsEarlierLecture,
} from "../lectureTextView.js";
import { Button } from "../ui/Button.js";
import { Card, ResultLine, type ResultKind, Segmented } from "../ui/controls.js";
import { Line } from "./SummaryParts.js";

export interface LectureTextCardProps {
  view: SummaryView;
  /** The page's running action: "copy", "save", "start" or "" (the lecture card's keys only). */
  busy: string;
  /** Another action of the page runs. */
  disabled: boolean;
  format: SummarySaveFormat;
  onFormat: (f: SummarySaveFormat) => void;
  onCopy: () => void;
  onSave: () => void;
  onStart: () => void;
  onOpenTranscript: () => void;
  message: { kind: ResultKind; text: string } | null;
}

export function LectureTextCard(props: LectureTextCardProps) {
  const v = props.view;
  const line = lectureLine(v);
  const start = lectureStartLabel(v);
  const blocks = lectureBlocks(v);
  const updated = lectureUpdatedText(v);
  const ready = v.lectureText.ready;
  const off = props.disabled || props.busy !== "";
  return (
    <Card className="box sm-lecture">
      <h2>Текст лекции</h2>
      {line && <Line line={line} />}
      {showsEarlierLecture(v) && <p className="small muted sm-detail">{lectureEarlierNote}</p>}
      {start !== null && (
        <div className="acts start sm-retry">
          <Button busy={props.busy === "start"} busyLabel="Запуск…" disabled={off} onClick={props.onStart}>
            {start}
          </Button>
        </div>
      )}
      {/* Only a ready text has the tools: there is nothing to copy before. */}
      {ready && (
        <div className="tr-share sm-lecture-tools">
          <Button
            busy={props.busy === "copy"}
            busyLabel="Копирование…"
            disabled={off}
            onClick={props.onCopy}
          >
            Скопировать
          </Button>
          <Segmented
            label="Формат файла текста лекции"
            options={[
              { value: "txt", label: ".txt" },
              { value: "md", label: ".md" },
            ]}
            value={props.format}
            onChange={props.onFormat}
          />
          <Button
            icon="down"
            busy={props.busy === "save"}
            busyLabel="Сохранение…"
            disabled={off}
            onClick={props.onSave}
          >
            Сохранить как…
          </Button>
        </div>
      )}
      {props.message && (
        <ResultLine kind={props.message.kind} role={props.message.kind === "fail" ? "alert" : "status"}>
          {props.message.text}
        </ResultLine>
      )}
      {blocks.length > 0 && (
        <div className="sm-text sm-lecture-text" aria-label="Текст лекции">
          {blocks.map((b, i) => (
            <section key={i} className="sm-block">
              <h3 className="sm-h">
                {b.time !== "" && <span className="muted num sm-time">{b.time}</span>}
                {b.heading}
              </h3>
              {b.paragraphs.map((p, j) => (
                <p key={j}>{p}</p>
              ))}
            </section>
          ))}
        </div>
      )}
      {updated !== "" && <p className="small muted num sm-detail">{updated}</p>}
      {v.transcribed && (
        <div className="acts start">
          <Button variant="ghost" className="sm-source" onClick={props.onOpenTranscript}>
            Исходная расшифровка
          </Button>
        </div>
      )}
    </Card>
  );
}
