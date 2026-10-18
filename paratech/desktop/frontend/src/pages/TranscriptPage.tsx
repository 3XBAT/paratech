import { memo, useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { formatTime } from "../activityView";
import { callByName } from "../bindings/runtime";
import { type SaveFormat, type TranscriptApi, transcriptApi, type TranscriptStatus } from "../bindings/transcript";
import {
  applyPage,
  emptyLoaded,
  errorText,
  type Loaded,
  localMatch,
  matchCountText,
  morePages,
  nextFetch,
  noAudioHint,
  noSelectionHint,
  pageSize,
  type Paragraph,
  paragraphPieces,
  paragraphs,
  playFailedText,
  playSeconds,
  pollMs,
  saveResultText,
  searchParagraphs,
  searchQuery,
  shouldPoll,
  statusView,
  stepMatch,
} from "../transcriptView";
import { Button } from "../ui/Button";
import { Banner, Card, Input, Progress, ResultLine, type ResultKind, Segmented } from "../ui/controls";

// «Расшифровка» of one recording (TASK-079): the local transcript, live while the recognition runs, with search,
// copy, save and playback from a timestamp. Works offline: everything comes from the core on this computer. The text
// is recognised speech and is rendered as React text only; the highlight is built from text pieces.
export function TranscriptPage(props: { recordingId: string; onBack: () => void }) {
  const { recordingId: id } = props;
  const api = useMemo(() => transcriptApi(callByName), []);
  const [status, setStatus] = useState<TranscriptStatus | null>(null);
  const [loaded, setLoaded] = useState<Loaded>(emptyLoaded);
  const [error, setError] = useState("");

  // One status and its pages at a time: the next poll is planned only after the previous answer.
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let doc = emptyLoaded;
    let last: TranscriptStatus | null = null;

    const tick = async () => {
      try {
        const st = await api.status(id);
        if (stopped) {
          return;
        }
        last = st;
        setStatus(st);
        setError("");
        const plan = nextFetch(st, doc);
        if (plan.kind === "clear") {
          doc = emptyLoaded;
          setLoaded(doc);
        }
        // The first page shows at once; the rest of a long transcript follows page by page.
        let from = plan.kind === "fetch" ? plan.from : null;
        let base = plan.kind === "fetch" && plan.reset ? emptyLoaded : doc;
        while (from !== null) {
          const page = await api.segments(id, from, pageSize);
          if (stopped) {
            return;
          }
          const next = applyPage(base, page);
          if (next === null) {
            // The document changed between pages (the recognition ended): read it again from the start.
            from = 0;
            base = emptyLoaded;
            continue;
          }
          doc = next;
          base = next;
          setLoaded(doc);
          from = morePages(doc, page);
        }
      } catch (err) {
        if (stopped) {
          return;
        }
        setError(errorText(err));
      }
      if (!stopped && shouldPoll(last)) {
        timer = setTimeout(() => void tick(), pollMs);
      }
    };
    void tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [api, id]);

  const view = status && statusView(status, loaded.segments.length);
  const title = status?.title || "Запись";

  return (
    <section className="page tr">
      <div className="hw-back">
        <Button variant="ghost" onClick={props.onBack}>
          Назад
        </Button>
      </div>
      <div className="head">
        <div>
          <h1>Расшифровка</h1>
          <p className="tr-sub">
            <span className="tr-title">{title}</span>
            {status && status.startedAt !== "" && <span className="muted num"> · {formatTime(status.startedAt)}</span>}
          </p>
        </div>
      </div>
      {error && (
        <Banner kind="fail" role="alert">
          {error}
        </Banner>
      )}
      {status === null ? (
        !error && <p className="muted">Загрузка…</p>
      ) : (
        <Viewer api={api} status={status} loaded={loaded} view={view!} />
      )}
    </section>
  );
}

interface Message {
  kind: ResultKind;
  text: string;
}

function Viewer(props: {
  api: TranscriptApi;
  status: TranscriptStatus;
  loaded: Loaded;
  view: ReturnType<typeof statusView>;
}) {
  const { api, status, view } = props;
  const id = status.recordingId;
  const paras = useMemo(() => paragraphs(props.loaded.segments), [props.loaded.segments]);
  const audio = useRef<HTMLAudioElement>(null);
  const textBox = useRef<HTMLDivElement>(null);
  const mounted = useRef(true);
  const acting = useRef(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<Message | null>(null);
  const [withTimestamps, setWithTimestamps] = useState(true);
  const [format, setFormat] = useState<SaveFormat>("txt");
  const [hasSelection, setHasSelection] = useState(false);
  const [playError, setPlayError] = useState("");

  const [query, setQuery] = useState("");
  const [current, setCurrent] = useState(0);
  const [jump, setJump] = useState(0);
  // Typing stays quick in a long transcript: the search runs on the deferred text.
  const folded = searchQuery(useDeferredValue(query));
  const search = useMemo(() => searchParagraphs(paras, folded), [paras, folded]);
  const cur = search.total === 0 ? -1 : Math.min(current, search.total - 1);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // Only a new query or a step moves the text; new segments of a running recognition do not.
  useEffect(() => {
    textBox.current?.querySelector("mark.cur")?.scrollIntoView({ block: "center" });
  }, [jump, folded]);

  useEffect(() => {
    const onChange = () => setHasSelection(selectionIn(textBox.current) !== "");
    document.addEventListener("selectionchange", onChange);
    return () => document.removeEventListener("selectionchange", onChange);
  }, []);

  const step = (dir: 1 | -1) => {
    setCurrent(stepMatch(cur, search.total, dir));
    setJump((j) => j + 1);
  };

  const playable = status.audioUrl !== "";
  const play = useCallback((startMs: number) => {
    const a = audio.current;
    if (a === null) {
      return;
    }
    setPlayError("");
    a.currentTime = playSeconds(startMs);
    a.play().catch(() => {
      if (mounted.current) {
        setPlayError(playFailedText);
      }
    });
  }, []);

  async function act(key: string, action: () => Promise<Message | null>) {
    if (acting.current) {
      return;
    }
    acting.current = true;
    setBusy(key);
    setMessage(null);
    try {
      const m = await action();
      if (mounted.current) {
        setMessage(m);
      }
    } catch (err) {
      if (mounted.current) {
        setMessage({ kind: "fail", text: errorText(err) });
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setBusy("");
      }
    }
  }

  const copyAll = () =>
    act("all", async () => {
      await api.copyAll(id, withTimestamps);
      return { kind: "ok", text: "Расшифровка скопирована в буфер обмена." };
    });

  const copySelection = () => {
    const text = selectionIn(textBox.current);
    if (text === "") {
      setMessage({ kind: "wait", text: noSelectionHint });
      return;
    }
    void act("selection", async () => {
      await api.copyText(text);
      return { kind: "ok", text: "Выделенный текст скопирован в буфер обмена." };
    });
  };

  const save = () =>
    act("save", async () => {
      const r = saveResultText(await api.saveAs(id, format, withTimestamps));
      return r && { kind: r.ok ? "ok" : "fail", text: r.text };
    });

  const hasText = status.segments > 0;
  const idle = busy === "";
  const noText = hasText ? undefined : "Пока нечего копировать.";

  return (
    <>
      <Card className="box tr-head">
        {view.failed ? (
          <ResultLine kind="fail" role="status">
            {view.message}
          </ResultLine>
        ) : (
          <p className="tr-state" role="status">
            {view.message}
          </p>
        )}
        {view.progress !== null && <Progress value={view.progress} label="Распознавание" text={false} />}
        {view.partial !== "" && <p className="small muted tr-note">{view.partial}</p>}
        {playable ? (
          <audio ref={audio} className="tr-audio" controls preload="none" src={status.audioUrl}>
            Проигрыватель недоступен.
          </audio>
        ) : (
          <p className="small muted tr-note">{noAudioHint}</p>
        )}
        {playError !== "" && (
          <ResultLine kind="fail" role="alert">
            {playError}
          </ResultLine>
        )}
      </Card>

      <Card className="box tr-tools">
        <div className="tr-search">
          <Input
            type="search"
            aria-label="Поиск по расшифровке"
            placeholder="Поиск по тексту"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setCurrent(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                step(e.shiftKey ? -1 : 1);
              }
            }}
          />
          <span className="small muted num tr-count" aria-live="polite">
            {matchCountText(cur, search.total, folded)}
          </span>
          <Button variant="ghost" icon="up" disabled={search.total === 0} onClick={() => step(-1)} title="Shift+Enter">
            Предыдущее
          </Button>
          <Button variant="ghost" icon="down" disabled={search.total === 0} onClick={() => step(1)} title="Enter">
            Следующее
          </Button>
        </div>
        <div className="tr-share">
          <label className="tr-switch">
            <input
              type="checkbox"
              role="switch"
              className="switch"
              checked={withTimestamps}
              onChange={(e) => setWithTimestamps(e.target.checked)}
            />
            С метками времени
          </label>
          <Button
            busy={busy === "all"}
            busyLabel="Копирование…"
            disabled={!idle || !hasText}
            disabledReason={idle ? noText : undefined}
            onClick={() => void copyAll()}
          >
            Скопировать всё
          </Button>
          <Button
            busy={busy === "selection"}
            busyLabel="Копирование…"
            disabled={!idle || !hasSelection}
            disabledReason={idle && hasText ? noSelectionHint : undefined}
            // Keeps the selection: the click must not move it before it is read.
            onMouseDown={(e) => e.preventDefault()}
            onClick={copySelection}
          >
            Скопировать выделенное
          </Button>
          <Segmented
            label="Формат файла"
            options={[
              { value: "txt", label: ".txt" },
              { value: "md", label: ".md" },
            ]}
            value={format}
            onChange={setFormat}
          />
          <Button
            icon="down"
            busy={busy === "save"}
            busyLabel="Сохранение…"
            disabled={!idle || !hasText}
            disabledReason={idle ? noText : undefined}
            onClick={() => void save()}
          >
            Сохранить как…
          </Button>
        </div>
        {message && (
          <ResultLine kind={message.kind} role={message.kind === "fail" ? "alert" : "status"}>
            {message.text}
          </ResultLine>
        )}
        <p className="small muted tr-note">
          Расшифровка хранится на этом компьютере. Копирование и сохранение — только по кнопке, в сеть ничего не уходит.
        </p>
      </Card>

      {paras.length === 0 ? (
        view.empty !== "" && <p className="muted">{view.empty}</p>
      ) : (
        <div ref={textBox} className="tr-text" aria-label="Текст расшифровки">
          {paras.map((p, i) => (
            <ParagraphView
              key={p.first}
              para={p}
              folded={folded}
              current={localMatch(search, i, cur)}
              playable={playable}
              onPlay={play}
            />
          ))}
        </div>
      )}
    </>
  );
}

interface ParagraphProps {
  para: Paragraph;
  folded: string;
  current: number;
  playable: boolean;
  onPlay: (startMs: number) => void;
}

function sameParagraph(a: ParagraphProps, b: ParagraphProps): boolean {
  const x = a.para.segments;
  const y = b.para.segments;
  return (
    x.length === y.length &&
    x[0] === y[0] &&
    x[x.length - 1] === y[y.length - 1] &&
    a.folded === b.folded &&
    a.current === b.current &&
    a.playable === b.playable &&
    a.onPlay === b.onPlay
  );
}

// Re-renders only when its own text, matches or current match change, so a long transcript stays cheap to update.
const ParagraphView = memo(function ParagraphView(props: ParagraphProps) {
  const { para } = props;
  const pieces = paragraphPieces(para, props.folded);
  return (
    <div className="tr-par">
      <button
        type="button"
        className="tr-time num"
        disabled={!props.playable}
        title={props.playable ? `Слушать с ${para.start}` : noAudioHint}
        onClick={() => props.onPlay(para.startMs)}
      >
        {para.start}
      </button>
      <p>
        {pieces.map((seg, si) => (
          <span key={para.segments[si].index}>
            {si > 0 && " "}
            {seg.map((pc, pi) =>
              pc.match < 0 ? (
                pc.text
              ) : (
                <mark key={pi} className={pc.match === props.current ? "cur" : undefined}>
                  {pc.text}
                </mark>
              ),
            )}
          </span>
        ))}
      </p>
    </div>
  );
}, sameParagraph);

/** The selected text when the selection lies inside el, "" otherwise. */
function selectionIn(el: HTMLElement | null): string {
  const s = window.getSelection();
  if (el === null || s === null || s.isCollapsed || s.rangeCount === 0) {
    return "";
  }
  return el.contains(s.getRangeAt(0).commonAncestorContainer) ? s.toString() : "";
}
