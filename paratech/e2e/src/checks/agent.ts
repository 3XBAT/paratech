// The agent thread check (bug of 04.11: a sticky «Стоп» strip floated over the reply text). The «Агент отвечает…/Стоп»
// row (`.ag-think`, `.ag-stop`) lives in the flow of the thread (`.ag-thread`, the scrolling container) and must never
// overlap the text of a message at any scroll position. The row exists only while the agent works; when it is absent
// the result says `rowSeen: false` and the spec reports «не проверено» for it.
import type { Page } from "@playwright/test";

import { evaluateAgentThread } from "./evaluate.js";
import { evalInPage } from "./inpage.js";
import type { AgentThreadSnapshot, LayoutOptions, LayoutViolation } from "./types.js";
import { resolveOptions } from "./types.js";

/** Runs in the page (async): samples the thread at the top, the middle and the bottom. Self-contained like probeLayout. */
export async function probeAgentThread(): Promise<AgentThreadSnapshot> {
  const thread = document.querySelector<HTMLElement>(".ag-thread");
  const empty = (note: string): AgentThreadSnapshot => ({ rowSeen: false, note, positions: [] });
  if (thread === null) {
    return empty("в окне нет ленты помощника (.ag-thread)");
  }
  const panel = thread.closest<HTMLElement>(".ag");
  if (panel !== null && getComputedStyle(panel).visibility === "hidden") {
    return empty("панель помощника закрыта");
  }

  const norm = (t: string): string => t.replace(/\s+/g, " ").trim();
  const cut = (t: string, n: number): string => (t.length > n ? t.slice(0, n - 1) + "…" : t);
  const part = (e: Element): string => {
    let s = e.localName;
    for (const c of Array.from(e.classList).slice(0, 2)) {
      s += "." + c;
    }
    return s;
  };
  const describe = (e: Element): string => {
    const chain: string[] = [];
    let cur: Element | null = e;
    for (let i = 0; cur !== null && cur !== thread && i < 3; i++) {
      chain.unshift(part(cur));
      cur = cur.parentElement;
    }
    return ".ag-thread > " + chain.join(" > ");
  };
  const shown = (e: Element): boolean => {
    for (let cur: Element | null = e; cur !== null; cur = cur.parentElement) {
      const s = getComputedStyle(cur);
      if (s.display === "none" || Number(s.opacity) === 0) {
        return false;
      }
    }
    return true;
  };
  const boxed = (e: Element): boolean => {
    const s = getComputedStyle(e);
    const r = e.getBoundingClientRect();
    return shown(e) && s.visibility === "visible" && r.width > 0 && r.height > 0 && !(r.width <= 1 && r.height <= 1);
  };
  const ownText = (e: Element): string => {
    let t = "";
    for (const n of Array.from(e.childNodes)) {
      if (n.nodeType === 3) {
        t += n.nodeValue ?? "";
      }
    }
    return norm(t);
  };
  // two frames so the scroll has been laid out; the timer is the way out when the window draws no frames (hidden)
  const frame = (): Promise<void> =>
    new Promise((resolve) => {
      let done = false;
      const finish = (): void => {
        if (!done) {
          done = true;
          resolve();
        }
      };
      requestAnimationFrame(() => requestAnimationFrame(finish));
      setTimeout(finish, 300);
    });

  const saved = thread.scrollTop;
  const max = Math.max(0, thread.scrollHeight - thread.clientHeight);
  const stops: Array<[string, number]> = [
    ["верх", 0],
    ["середина", Math.round(max / 2)],
    ["низ", max],
  ];
  const positions: AgentThreadSnapshot["positions"] = [];
  let rowSeen = false;

  for (const [label, top] of stops) {
    thread.scrollTop = top;
    await frame();
    const view = thread.getBoundingClientRect();
    const all = Array.from(thread.querySelectorAll("*")).filter(boxed);
    const rows = all.filter((e) => e.matches(".ag-think, .ag-stop"));
    if (rows.length > 0) {
      rowSeen = true;
    }
    const floating = all.filter((e) => {
      const p = getComputedStyle(e).position;
      return (p === "sticky" || p === "fixed") && !rows.includes(e);
    });
    const blockerEls = [...rows, ...floating];
    const blockers: AgentThreadSnapshot["positions"][number]["blockers"] = blockerEls.map((e) => {
      const r = e.getBoundingClientRect();
      return {
        el: describe(e),
        kind: rows.includes(e) ? ("row" as const) : ("floating" as const),
        position: getComputedStyle(e).position,
        box: { left: r.left, top: r.top, right: r.right, bottom: r.bottom },
      };
    });
    const texts: AgentThreadSnapshot["positions"][number]["texts"] = [];
    if (blockerEls.length > 0) {
      for (const e of all) {
        const t = ownText(e);
        if (t === "" || texts.length >= 400) {
          continue;
        }
        const r = e.getBoundingClientRect();
        // only what is in view of the thread can be covered
        if (r.bottom <= view.top || r.top >= view.bottom || r.right <= view.left || r.left >= view.right) {
          continue;
        }
        const related: number[] = [];
        blockerEls.forEach((b, i) => {
          if (b === e || b.contains(e) || e.contains(b)) {
            related.push(i);
          }
        });
        texts.push({ el: describe(e), text: cut(t, 40), box: { left: r.left, top: r.top, right: r.right, bottom: r.bottom }, related });
      }
    }
    positions.push({ label, scrollTop: thread.scrollTop, blockers, texts });
  }
  thread.scrollTop = saved;
  return { rowSeen, positions };
}

export const probeAgentThreadSource: string = probeAgentThread.toString();

export interface AgentThreadResult {
  /** A visible `.ag-think`/`.ag-stop` row existed (it does only while the agent works). */
  rowSeen: boolean;
  note?: string;
  violations: LayoutViolation[];
}

/** Samples the thread of the agent panel of the page and reports overlaps of the «Стоп»/progress row with text. */
export async function checkAgentThread(page: Page, options: LayoutOptions = {}): Promise<AgentThreadResult> {
  const o = resolveOptions(options);
  const snap = await evalInPage<AgentThreadSnapshot>(page, probeAgentThreadSource);
  const result: AgentThreadResult = { rowSeen: snap.rowSeen, violations: evaluateAgentThread(snap, o) };
  if (snap.note !== undefined) {
    result.note = snap.note;
  }
  return result;
}
