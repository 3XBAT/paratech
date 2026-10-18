import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { formatTime } from "../activityView";
import { CollapsibleList } from "../CollapsibleList";
import {
  type AssignmentDetail,
  type AssignmentItem,
  type DeliveryItem,
  type GenerationStatus,
  type HomeworkApi,
  homeworkApi,
  type SendPreparation,
  type VersionText,
} from "../bindings/homework";
import { callByName } from "../bindings/runtime";
import {
  assignmentDeliveryView,
  blockedByEmail,
  type ChipView,
  deliveriesInFlight,
  deliveryPollMs,
  deliveryView,
  dueText,
  errorText,
  generationGoesOn,
  generationPollMs,
  generationView,
  isStale,
  regenerateBlockedReason,
  versionTitle,
} from "../homeworkView";
import { initialListState, type ListState } from "../listView";
import { Button } from "../ui/Button";
import { Banner, Card, EmptyState, Field, Input, ResultLine, type ResultKind, Row } from "../ui/controls";
import { StatusChip } from "../ui/StatusChip";
import { SendAgainDialog, SendDialog, type SendResult } from "./HomeworkDialogs";
import { TranscriptLinks } from "./TranscriptLinks";

// «Домашние задания» (PRD section 10): the assignments, and for the opened one the draft editor, the extra
// instructions with «Сгенерировать заново», the version history, the export to a file and the send by email through
// the confirmation dialog (AC-13, AC-14, AC-15). The core decides what can be sent and what to offer for an email; the
// page shows it. Assignment, draft and instruction texts may be model output: they are plain React text and textarea
// values only, never HTML (hard rule 3).
export function HomeworkPage() {
  const api = useMemo(() => homeworkApi(callByName), []);
  const [openId, setOpenId] = useState("");

  return openId === "" ? (
    <AssignmentList api={api} onOpen={setOpenId} />
  ) : (
    <AssignmentEditor api={api} id={openId} onBack={() => setOpenId("")} />
  );
}

function Chip(props: { view: ChipView }) {
  return (
    <StatusChip kind={props.view.kind} icon={props.view.icon}>
      {props.view.label}
    </StatusChip>
  );
}

function MessageLine(props: { message: Message }) {
  const m = props.message;
  return (
    <ResultLine kind={m.kind} role={m.kind === "fail" ? "alert" : "status"}>
      {m.text}
    </ResultLine>
  );
}

function AssignmentList(props: { api: HomeworkApi; onOpen: (id: string) => void }) {
  const { api } = props;
  const [items, setItems] = useState<AssignmentItem[] | null>(null);
  const [deliveries, setDeliveries] = useState<Readonly<Record<string, DeliveryItem[]>>>({});
  const [error, setError] = useState("");
  const [list, setList] = useState<ListState>(initialListState);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const as = await api.listAssignments();
        if (!live) {
          return;
        }
        setItems(as);
        // The chip of a row is its newest email; a failed load leaves the row without a chip.
        const ds = await Promise.allSettled(as.map((a) => api.deliveries(a.id)));
        if (!live) {
          return;
        }
        const byId: Record<string, DeliveryItem[]> = {};
        ds.forEach((r, i) => {
          if (r.status === "fulfilled") {
            byId[as[i].id] = r.value;
          }
        });
        setDeliveries(byId);
      } catch (err) {
        if (live) {
          setError(errorText(err));
        }
      }
    })();
    return () => {
      live = false;
    };
  }, [api]);

  const row = (a: AssignmentItem) => {
    const chip = assignmentDeliveryView(Object.hasOwn(deliveries, a.id) ? deliveries[a.id] : undefined);
    return (
      <Row
        key={a.id}
        title={a.title}
        meta={<span className="num">{dueText(a.due)}</span>}
        status={chip && <Chip view={chip} />}
        actions={<Button onClick={() => props.onOpen(a.id)}>Открыть</Button>}
      />
    );
  };

  return (
    <section className="page hw">
      <div className="head">
        <h1>Домашние задания</h1>
      </div>
      {error && (
        <Banner kind="fail" role="alert">
          {error}
        </Banner>
      )}
      {items === null ? (
        !error && <p className="muted">Загрузка…</p>
      ) : items.length === 0 ? (
        <EmptyState label="Заданий пока нет" title="Здесь появятся домашние задания">
          Задание находится само в записи пары, после конспекта. Здесь можно подготовить по нему черновик, поправить его
          и отправить преподавателю по почте.
        </EmptyState>
      ) : (
        <CollapsibleList
          items={items}
          itemKey={(a) => a.id}
          renderItem={row}
          renderContainer={(rows) => (
            <div className="card rows" role="list">
              {rows}
            </div>
          )}
          state={list}
          onStateChange={setList}
          className="clist-card"
          classNames={{ button: "btn ghost" }}
        />
      )}
    </section>
  );
}

interface Message {
  kind: ResultKind;
  text: string;
  /** Shown under the draft or under the emails, next to the button that led to it. */
  at: "draft" | "mail";
}

/** The open send dialog: the core's preparation and the version written to the attached file. */
interface SendOpen {
  prep: SendPreparation;
  version: number;
}

interface Generation {
  jobId: string;
  status: GenerationStatus;
}

function AssignmentEditor(props: { api: HomeworkApi; id: string; onBack: () => void }) {
  const { api, id } = props;
  const [detail, setDetail] = useState<AssignmentDetail | null>(null);
  const [loadError, setLoadError] = useState("");
  // The version shown in the editor (its saved text) and the text in the fields.
  const [shown, setShown] = useState<VersionText | null>(null);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [instruction, setInstruction] = useState("");
  const [gen, setGen] = useState<Generation | null>(null);
  const [deliveries, setDeliveries] = useState<DeliveryItem[] | null>(null);
  const [deliveriesError, setDeliveriesError] = useState("");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState<Message | null>(null);
  const [sendOpen, setSendOpen] = useState<SendOpen | null>(null);
  const [sendAgain, setSendAgain] = useState<DeliveryItem | null>(null);
  const mounted = useRef(true);
  const acting = useRef(false); // one action at a time; a double click must not start a second one
  const loadingDeliveries = useRef(false);
  const deliveriesAgain = useRef(false);
  // The send the open dialog shows: cancelled in the core if the page goes away without an answer.
  const pendingSend = useRef("");

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      const p = pendingSend.current;
      pendingSend.current = "";
      if (p !== "") {
        void api.cancelSend(p).catch(() => undefined);
      }
    };
  }, [api]);

  // keepText: the fields keep what the student typed; otherwise they show the latest version.
  const reload = useCallback(
    async (keepText: boolean) => {
      const d = await api.openAssignment(id);
      if (!mounted.current) {
        return;
      }
      setDetail(d);
      setShown(d.latest);
      if (!keepText) {
        setTitle(d.latest?.title ?? "");
        setBody(d.latest?.body ?? "");
      }
    },
    [api, id],
  );

  // One load at a time: a refresh asked for during a load runs once more after it.
  const loadDeliveries = useCallback(async () => {
    if (loadingDeliveries.current) {
      deliveriesAgain.current = true;
      return;
    }
    loadingDeliveries.current = true;
    try {
      do {
        deliveriesAgain.current = false;
        try {
          const ds = await api.deliveries(id);
          if (mounted.current) {
            setDeliveries(ds);
            setDeliveriesError("");
          }
        } catch (err) {
          if (mounted.current) {
            setDeliveriesError(errorText(err));
          }
        }
      } while (deliveriesAgain.current && mounted.current);
    } finally {
      loadingDeliveries.current = false;
    }
  }, [api, id]);

  useEffect(() => {
    reload(false).catch((err) => {
      if (mounted.current) {
        setLoadError(errorText(err));
      }
    });
    void loadDeliveries();
  }, [reload, loadDeliveries]);

  // Emails on their way change by themselves.
  const inFlight = deliveries !== null && deliveriesInFlight(deliveries);
  useEffect(() => {
    if (!inFlight) {
      return;
    }
    const timer = setInterval(() => void loadDeliveries(), deliveryPollMs);
    return () => clearInterval(timer);
  }, [inFlight, loadDeliveries]);

  // A running generation is asked for again only after the previous answer, so calls never overlap.
  const jobId = gen?.jobId ?? "";
  useEffect(() => {
    if (jobId === "") {
      return;
    }
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
      let status: GenerationStatus;
      try {
        status = await api.generationStatus(jobId);
      } catch (err) {
        if (!stopped) {
          setGen(null);
          setMessage({ kind: "fail", text: errorText(err), at: "draft" });
        }
        return;
      }
      if (stopped) {
        return;
      }
      setGen({ jobId, status });
      if (generationGoesOn(status.state)) {
        timer = setTimeout(() => void tick(), generationPollMs);
      } else if (status.state === "done") {
        setInstruction("");
        try {
          await reload(false);
        } catch (err) {
          if (!stopped) {
            setMessage({ kind: "fail", text: errorText(err), at: "draft" });
          }
        }
      }
    };
    timer = setTimeout(() => void tick(), generationPollMs);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [api, jobId, reload]);

  async function act(key: string, action: () => Promise<void>, at: Message["at"] = "draft") {
    if (acting.current) {
      return;
    }
    acting.current = true;
    setBusy(key);
    setMessage(null);
    try {
      await action();
    } catch (err) {
      if (mounted.current) {
        setMessage({ kind: "fail", text: errorText(err), at });
        if (isStale(err)) {
          // Unsaved text stays in the fields, now over the new latest version.
          await reload(dirty).catch(() => undefined);
        }
        if (blockedByEmail(err)) {
          void loadDeliveries();
        }
      }
    } finally {
      acting.current = false;
      if (mounted.current) {
        setBusy("");
      }
    }
  }

  const latest = detail?.latest ?? null;
  const isLatest = shown !== null && latest !== null && shown.number === latest.number;
  const dirty = isLatest && (title !== shown.title || body !== shown.body);
  const generating = gen !== null && generationGoesOn(gen.status.state);
  const readOnly = !isLatest || generating;
  const idle = busy === "" && sendOpen === null && sendAgain === null;
  const savedReason = dirty ? "Сначала сохраните или отмените правку." : undefined;
  const regenerateReason = regenerateBlockedReason(shown !== null && !isLatest, dirty);

  const save = () =>
    act("save", async () => {
      if (shown === null) {
        return;
      }
      // The base is the shown version: the core refuses the edit if a newer one appeared meanwhile.
      const v = await api.editDraft(id, shown.number, title, body);
      await reload(false);
      setMessage({ kind: "ok", text: `Правка сохранена: версия ${v.number}.`, at: "draft" });
    });

  const revert = () => {
    if (shown !== null) {
      setTitle(shown.title);
      setBody(shown.body);
    }
  };

  const generate = () =>
    act("generate", async () => {
      // A new version always starts from the latest one; the button is off while an older one is shown.
      const g = await api.generate(id, latest?.number ?? 0, instruction);
      setGen({ jobId: g.jobId, status: { state: "generating", message: "" } });
    });

  const showVersion = (n: number) =>
    act("version", async () => {
      const v = latest !== null && n === latest.number ? latest : await api.draftVersion(id, n);
      setShown(v);
      setTitle(v.title);
      setBody(v.body);
    });

  // An older text continues as an edit of the latest version: saving it adds a new version.
  const takeText = () => {
    if (latest !== null) {
      setShown(latest);
    }
  };

  const exportFile = () =>
    act("export", async () => {
      if (shown === null) {
        return;
      }
      const r = await api.exportDraft(id, shown.number);
      setMessage({ kind: "ok", text: `Версия ${r.version} сохранена в файл «${r.fileName}».`, at: "draft" });
    });

  // The shown version goes to the file the email attaches; the core then checks the send and fills the dialog. A
  // refused export (exportLocked: an earlier email is unresolved) throws here, so no send is prepared.
  const startSend = () =>
    act("send", async () => {
      if (shown === null) {
        return;
      }
      const exported = await api.exportDraft(id, shown.number);
      const prep = await api.prepareSend(id);
      if (!mounted.current) {
        if (prep.status === "ready") {
          void api.cancelSend(prep.proposalId).catch(() => undefined); // nobody saw this dialog
        }
        return;
      }
      if (prep.status === "ready") {
        pendingSend.current = prep.proposalId;
        setSendOpen({ prep, version: exported.version });
      } else {
        setMessage({ kind: "fail", text: prep.message, at: "draft" });
      }
    });

  const sendClosed = (result: SendResult) => {
    pendingSend.current = "";
    setSendOpen(null);
    // After a failed confirmation the dialog has shown the reason; the list of emails tells the rest.
    if (result === "sent") {
      setMessage({ kind: "ok", text: "Письмо отправляется: его состояние — в списке «Письма» ниже.", at: "draft" });
    } else if (result === "cancelled") {
      setMessage({ kind: "wait", text: "Отправка отменена: письмо не отправлено.", at: "draft" });
    }
    void loadDeliveries();
  };

  const sendAgainClosed = (sent: boolean) => {
    setSendAgain(null);
    if (sent) {
      void loadDeliveries();
    }
  };

  const markSent = (d: DeliveryItem) =>
    act(
      `mark:${d.id}`,
      async () => {
        await api.markSent(d.id);
        await loadDeliveries();
      },
      "mail",
    );

  if (detail === null) {
    return (
      <section className="page hw">
        <div className="hw-back">
          <Button variant="ghost" onClick={props.onBack}>
            Все задания
          </Button>
        </div>
        {loadError ? (
          <Banner kind="fail" role="alert">
            {loadError}
          </Banner>
        ) : (
          <p className="muted">Загрузка…</p>
        )}
      </section>
    );
  }

  const a = detail.assignment;
  const versions = [...detail.versions].reverse(); // newest first
  const genView = gen && generationView(gen.status);

  return (
    <section className="page hw">
      <div className="hw-back">
        <Button variant="ghost" onClick={props.onBack}>
          Все задания
        </Button>
      </div>
      <div className="head">
        <h1 className="hw-title">{a.title}</h1>
        <span className="muted num">{dueText(a.due)}</span>
      </div>
      {a.description !== "" && <p className="hw-desc">{a.description}</p>}
      <TranscriptLinks lessonId={a.lessonId} />

      <Card className="box">
        <h2>Черновик</h2>
        {shown === null ? (
          <p className="small muted box-desc">Черновика ещё нет: его подготовит сервис конспектов по тексту задания.</p>
        ) : (
          <>
            <span className="small muted box-desc num">
              {versionTitle(shown)} · {formatTime(shown.createdAt)}
              {isLatest ? " · последняя" : ""}
            </span>
            {!isLatest && (
              <Banner kind="wait">
                Это старая версия, она только для просмотра. Её можно сохранить в файл или отправить, а чтобы продолжить с
                её текстом — «Взять этот текст».
              </Banner>
            )}
            <Field label="Заголовок">
              {(fid) => <Input id={fid} value={title} readOnly={readOnly} onChange={(e) => setTitle(e.target.value)} />}
            </Field>
            <Field label="Текст">
              {(fid) => (
                <textarea
                  id={fid}
                  className="input hw-text"
                  rows={14}
                  value={body}
                  readOnly={readOnly}
                  onChange={(e) => setBody(e.target.value)}
                />
              )}
            </Field>
            <div className="acts start">
              {isLatest && dirty && (
                <>
                  <Button variant="primary" busy={busy === "save"} busyLabel="Сохранение…" disabled={!idle} onClick={save}>
                    Сохранить правку
                  </Button>
                  <Button variant="ghost" disabled={!idle} onClick={revert}>
                    Отменить правку
                  </Button>
                </>
              )}
              {!isLatest && (
                <>
                  <Button disabled={!idle || generating} onClick={takeText}>
                    Взять этот текст
                  </Button>
                  <Button variant="ghost" disabled={!idle} onClick={() => latest && void showVersion(latest.number)}>
                    К последней версии
                  </Button>
                </>
              )}
              <Button
                icon="down"
                busy={busy === "export"}
                busyLabel="Сохранение…"
                disabled={!idle || dirty}
                disabledReason={savedReason}
                onClick={exportFile}
              >
                Сохранить в файл
              </Button>
              <Button
                icon="mail"
                busy={busy === "send"}
                busyLabel="Подготовка…"
                disabled={!idle || dirty}
                onClick={startSend}
              >
                Отправить по почте…
              </Button>
            </div>
            {isLatest && !dirty && <p className="small muted hw-note">Правка сохраняется новой версией, старые остаются.</p>}
          </>
        )}
        {message?.at === "draft" && <MessageLine message={message} />}
      </Card>

      <Card className="box">
        <h2>{latest === null ? "Подготовить черновик" : "Доработать черновик"}</h2>
        <Field
          label="Дополнительные указания"
          hint="Необязательно. Например: «короче», «добавь пример решения», «по-английски»."
        >
          {(fid) => (
            <textarea
              id={fid}
              className="input hw-instr"
              rows={3}
              value={instruction}
              readOnly={generating}
              onChange={(e) => setInstruction(e.target.value)}
            />
          )}
        </Field>
        <div className="acts start">
          <Button
            variant={latest === null ? "primary" : "secondary"}
            busy={generating || busy === "generate"}
            busyLabel="Генерация…"
            disabled={!idle || generating || regenerateReason !== undefined}
            disabledReason={regenerateReason}
            onClick={generate}
          >
            {latest === null ? "Сгенерировать черновик" : "Сгенерировать заново"}
          </Button>
        </div>
        {genView && (
          <div className="hw-gen" role="status">
            <Chip view={genView} />
            {genView.hint !== "" && <span className="small muted">{genView.hint}</span>}
          </div>
        )}
      </Card>

      {versions.length > 0 && (
        <>
          <h2 className="group-title">Версии</h2>
          {dirty && <p className="small muted">Чтобы открыть другую версию, сохраните или отмените правку.</p>}
          <div className="card rows" role="list">
            {versions.map((v) => (
              <Row
                key={v.number}
                title={
                  <>
                    {versionTitle(v)}
                    {v.latest ? " · последняя" : ""}
                  </>
                }
                meta={
                  <>
                    <span className="num">{formatTime(v.createdAt)}</span>
                    {v.instruction !== "" && <span className="hw-quote">Указания: {v.instruction}</span>}
                  </>
                }
                actions={
                  shown !== null && shown.number === v.number ? (
                    <span className="small muted">Открыта</span>
                  ) : (
                    <Button disabled={!idle || dirty || generating} onClick={() => void showVersion(v.number)}>
                      Открыть
                    </Button>
                  )
                }
              />
            ))}
          </div>
        </>
      )}

      <h2 className="group-title">Письма</h2>
      {deliveriesError && (
        <Banner kind="fail" role="alert">
          {deliveriesError}
        </Banner>
      )}
      {message?.at === "mail" && <MessageLine message={message} />}
      {deliveries === null ? (
        !deliveriesError && <p className="muted">Загрузка…</p>
      ) : deliveries.length === 0 ? (
        <p className="muted">Писем по этому заданию ещё не было.</p>
      ) : (
        <div className="card rows" role="list">
          {deliveries.map((d) => {
            const v = deliveryView(d);
            return (
              <Row
                key={d.id}
                title={<bdi>{d.recipient}</bdi>}
                meta={<span className="num">Создано {formatTime(d.createdAt)}</span>}
                status={
                  <>
                    <Chip view={v} />
                    {v.hint !== "" && <span className="small muted">{v.hint}</span>}
                    {v.problem !== "" && <span className="small row-problem">{v.problem}</span>}
                  </>
                }
                actions={
                  <>
                    {d.canMarkSent && (
                      <Button
                        busy={busy === `mark:${d.id}`}
                        busyLabel="Сохранение…"
                        disabled={!idle}
                        onClick={() => void markSent(d)}
                      >
                        Письмо дошло
                      </Button>
                    )}
                    {d.canSendAgain && (
                      <Button variant="danger" disabled={!idle} onClick={() => setSendAgain(d)}>
                        Отправить ещё раз…
                      </Button>
                    )}
                  </>
                }
              />
            );
          })}
        </div>
      )}

      {sendOpen && <SendDialog api={api} prep={sendOpen.prep} version={sendOpen.version} onClose={sendClosed} />}
      {sendAgain && <SendAgainDialog api={api} delivery={sendAgain} onClose={sendAgainClosed} />}
    </section>
  );
}
