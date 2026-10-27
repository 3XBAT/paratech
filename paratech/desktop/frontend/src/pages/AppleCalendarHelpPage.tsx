import { useEffect, useMemo, useRef, useState } from "react";
// The .js extensions let the Node test runner load the compiled page (test/help.test.mjs renders HelpView).
import { helpApi, type HelpPageKey } from "../bindings/help.js";
import { callByName } from "../bindings/runtime.js";
import {
  helpOptions,
  helpTexts,
  linksNote,
  openHelpLink,
  troublePoints,
  verdictTexts,
  type HelpBlock,
  type Verdict,
} from "../appleHelpView.js";
import { Button } from "../ui/Button.js";
import { Banner, Card, ResultLine } from "../ui/controls.js";
import { StatusChip, type ChipKind } from "../ui/StatusChip.js";
import type { IconName } from "../ui/Icon.js";

/** A failed open: the text and the block whose button failed. */
export interface HelpFailure {
  block: string;
  text: string;
}

// «Как увидеть события в Apple Calendar», opened from «Подключения». The page sends only keys; the core opens the
// official pages in the browser (ARCH-44).
export function AppleCalendarHelpPage(props: { onBack: () => void }) {
  const api = useMemo(() => helpApi(callByName), []);
  const [failure, setFailure] = useState<HelpFailure | null>(null);
  const mounted = useRef(true);
  const running = useRef(false);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function open(block: string, key: HelpPageKey) {
    if (running.current) {
      return;
    }
    running.current = true;
    setFailure(null);
    try {
      const text = await openHelpLink(api, key);
      if (mounted.current && text !== "") {
        setFailure({ block, text });
      }
    } finally {
      running.current = false;
    }
  }

  return <HelpView failure={failure} onOpen={(block, key) => void open(block, key)} onBack={props.onBack} />;
}

const verdictChips: Readonly<Record<Verdict, { kind: ChipKind; icon: IconName | null }>> = {
  recommended: { kind: "ok", icon: "ok" },
  fallback: { kind: "neutral", icon: null },
  notRecommended: { kind: "fail", icon: "warn" },
};

/** The screen itself, without the binding. */
export function HelpView(props: {
  failure: HelpFailure | null;
  onOpen: (block: string, key: HelpPageKey) => void;
  onBack: () => void;
}) {
  const title = useRef<HTMLHeadingElement>(null);

  // The screen opens from the bottom of «Подключения»: move to its top and announce the title.
  useEffect(() => {
    title.current?.focus();
  }, []);

  return (
    <section className="page help">
      <div className="head">
        <div>
          <span className="label">{helpTexts.lead}</span>
          <h2 ref={title} tabIndex={-1}>
            {helpTexts.title}
          </h2>
          <p className="small muted">{helpTexts.entryText}</p>
        </div>
        <Button onClick={props.onBack}>{helpTexts.back}</Button>
      </div>

      <p>{helpTexts.intro}</p>
      <Banner kind="wait">
        <p>
          <b>{helpTexts.editTitle}</b> {helpTexts.editText}
        </p>
      </Banner>

      {helpOptions.map((o) => {
        const chip = verdictChips[o.verdict];
        return (
          <Card key={o.id} className="box" label={`${o.label}. ${o.title}`}>
            <div className="help-title">
              <h2>
                {o.label}. {o.title}
              </h2>
              <StatusChip kind={chip.kind} icon={chip.icon}>
                {verdictTexts[o.verdict]}
              </StatusChip>
            </div>
            <span className="small muted box-desc">{o.summary}</span>
            {o.blocks.map((b, i) => (
              <div key={b.id}>
                {i > 0 && <hr className="sep" />}
                <Block block={b} failure={props.failure} onOpen={props.onOpen} />
              </div>
            ))}
          </Card>
        );
      })}

      <Card className="box" label={helpTexts.troubleTitle}>
        <h2>{helpTexts.troubleTitle}</h2>
        <ul className="help-list">
          {troublePoints.map((t) => (
            <li key={t}>{t}</li>
          ))}
        </ul>
      </Card>

      <div className="acts start help-back">
        <Button onClick={props.onBack}>{helpTexts.back}</Button>
      </div>
    </section>
  );
}

function Block(props: {
  block: HelpBlock;
  failure: HelpFailure | null;
  onOpen: (block: string, key: HelpPageKey) => void;
}) {
  const b = props.block;
  return (
    <>
      {b.title && <h3>{b.title}</h3>}
      {b.text && <p className="help-text">{b.text}</p>}
      {b.points && (
        <ul className="help-list">
          {b.points.map((t) => (
            <li key={t}>{t}</li>
          ))}
        </ul>
      )}
      {b.steps && (
        <ol className="help-list">
          {b.steps.map((t) => (
            <li key={t}>{t}</li>
          ))}
        </ol>
      )}
      {b.note && <p className="small muted">{b.note}</p>}
      {b.links && (
        <div className="acts start">
          {b.links.map((l) => (
            <Button key={l.key} variant="ghost" onClick={() => props.onOpen(b.id, l.key)}>
              {l.caption}
            </Button>
          ))}
          <span className="small muted">{linksNote(b.links.length)}</span>
        </div>
      )}
      {props.failure !== null && props.failure.block === b.id && (
        <ResultLine kind="fail" role="alert">
          {props.failure.text}
        </ResultLine>
      )}
    </>
  );
}
