// The decision half of the layout check: a pure function from a snapshot to violations (no browser, no DOM). The rules
// and their thresholds are described in docs/tasks/TASK-376-e2e-layout-invariants.md; the options in types.ts.
import {
  type AgentThreadSnapshot,
  type Box,
  type ConsoleRecord,
  type LayoutCounts,
  type LayoutOptions,
  type LayoutRule,
  type LayoutSnapshot,
  type LayoutViolation,
  type ResolvedOptions,
  resolveOptions,
} from "./types.js";

/**
 * The opacity of a computed CSS colour (Chrome gives rgb()/rgba(), `transparent`, or the modern color()/lab()/oklch()
 * forms with `/ alpha`). Undefined when the text is not a colour this function understands.
 */
export function colorAlpha(css: string): number | undefined {
  const s = css.trim().toLowerCase();
  if (s === "transparent") {
    return 0;
  }
  const m = /^[a-z0-9-]+\(([^)]*)\)$/.exec(s);
  if (m === null || m[1] === undefined) {
    return undefined;
  }
  const inner = m[1].trim();
  let token: string | undefined;
  if (inner.includes("/")) {
    token = inner.split("/")[1]?.trim();
  } else if (inner.includes(",")) {
    const parts = inner.split(",").map((p) => p.trim());
    token = parts.length >= 4 ? parts[3] : undefined;
  }
  if (token === undefined || token === "") {
    return /^(rgb|hsl|color|hwb|lab|lch|oklab|oklch)/.test(s) ? 1 : undefined;
  }
  if (token === "none") {
    return 0;
  }
  const v = token.endsWith("%") ? Number(token.slice(0, -1)) / 100 : Number(token);
  return Number.isFinite(v) ? Math.min(1, Math.max(0, v)) : undefined;
}

const px = (v: string): number => {
  const n = Number.parseFloat(v);
  return Number.isFinite(n) ? n : 0;
};

function matchesIgnore(rec: ConsoleRecord, o: ResolvedOptions): string | undefined {
  for (const ig of o.ignoreConsole) {
    if (new RegExp(ig.pattern).test(rec.text)) {
      return ig.reason;
    }
  }
  return undefined;
}

/** The violations of a snapshot under the options; each rule can be switched off. */
export function evaluateLayout(snap: LayoutSnapshot, options: LayoutOptions = {}): LayoutViolation[] {
  const o = resolveOptions(options);
  const out: LayoutViolation[] = [];
  const add = (rule: LayoutRule, element: string, detail: string) => out.push({ rule, element, detail });
  const tol = o.tolerancePx;
  const { viewport: vp, doc } = snap;

  if (o.rules["window-scroll"] && doc.scrollHeight > vp.height + tol) {
    const lowest = snap.lowest === null ? "" : `; ниже всех: ${snap.lowest.el} (низ ${snap.lowest.bottom} px)`;
    add(
      "window-scroll",
      "окно (document.scrollingElement)",
      `страница прокручивается: scrollHeight ${doc.scrollHeight} > innerHeight ${vp.height} (на ${doc.scrollHeight - vp.height} px, допуск ${tol})${lowest}`,
    );
  }

  if (o.rules["overflow-x"]) {
    if (doc.bodyScrollWidth > vp.width + tol) {
      add("overflow-x", "body", `body.scrollWidth ${doc.bodyScrollWidth} > innerWidth ${vp.width} (допуск ${tol})`);
    }
    for (const e of snap.overflowX) {
      add("overflow-x", e.el, `правый край ${e.right} px за краем окна (innerWidth ${vp.width}), ширина ${e.width} px`);
    }
  }

  if (o.rules["icon-button"]) {
    for (const b of snap.iconButtons) {
      const lacks: string[] = [];
      if (b.label === "") {
        lacks.push("нет aria-label");
      }
      if (b.title === "" && b.tip === "") {
        lacks.push("нет подсказки (непустой title или элемент .tip/role=tooltip с текстом)");
      }
      if (lacks.length > 0) {
        const what = b.glyph === undefined ? "кнопка без видимого текста" : `кнопка только с символом «${b.glyph}»`;
        add("icon-button", b.el, `${what}: ${lacks.join("; ")}`);
      }
    }
  }

  if (o.rules["status-quiet"]) {
    for (const s of snap.statuses) {
      const bad: string[] = [];
      const bg = colorAlpha(s.backgroundColor);
      if (bg !== 0) {
        bad.push(`background-color ${s.backgroundColor}`);
      }
      if (s.backgroundImage !== "none" && s.backgroundImage !== "") {
        bad.push(`background-image ${s.backgroundImage}`);
      }
      for (const b of s.borders) {
        const visibleBorder = b.style !== "none" && b.style !== "hidden" && px(b.width) > 0 && colorAlpha(b.color) !== 0;
        if (visibleBorder) {
          bad.push(`border-${b.side} ${b.width} ${b.style} ${b.color}`);
        }
      }
      if (s.boxShadow !== "none" && s.boxShadow !== "") {
        bad.push(`box-shadow ${s.boxShadow}`);
      }
      if (!s.focused && s.outlineStyle !== "none" && px(s.outlineWidth) > 0) {
        bad.push(`outline ${s.outlineWidth} ${s.outlineStyle}`);
      }
      if (bad.length > 0) {
        add("status-quiet", s.el, `статус «${s.status}» не тихий: ${bad.join("; ")}`);
      }
    }
  }

  if (o.rules["raw-text"]) {
    for (const t of snap.rawText) {
      const where = t.source === undefined || t.source === "text" ? "на экране" : t.source === "tooltip" ? "в подсказке" : `в атрибуте ${t.source}`;
      add("raw-text", t.el, `${where} служебный текст «${t.match}»: «${t.context}»`);
    }
  }

  if (o.rules["pane-scroll"]) {
    for (const p of snap.panes) {
      if (o.sidewaysPanes.includes(p.selector)) {
        continue;
      }
      add(
        "pane-scroll",
        p.el,
        `панель ${p.selector} прокручивается вбок: scrollWidth ${p.scrollWidth} > clientWidth ${p.clientWidth} (допуск ${tol}); слишком широкая строка или таблица внутри`,
      );
    }
  }

  if (o.rules["clipped"]) {
    for (const c of snap.clipped) {
      if (c.kind === "control") {
        add(
          "clipped",
          c.el,
          `элемент управления обрезан на ${c.cutPx} px по ${c.axis === "x" ? "горизонтали" : "вертикали"} предком ${c.by} (overflow hidden|clip, прокрутки между ними нет; допуск ${o.clipSlackPx})`,
        );
      } else {
        add("clipped", c.el, `текст «${c.text}» обрезан навсегда: scrollHeight ${c.scrollHeight} > clientHeight ${c.clientHeight} при overflow hidden|clip и без line-clamp`);
      }
    }
  }

  if (o.rules["truncated"]) {
    for (const t of snap.truncated) {
      if (!t.hasTip) {
        add(
          "truncated",
          t.el,
          `текст «${t.text}» обрезан по ${t.axis === "x" ? "ширине" : "высоте"}, а подсказки с полным текстом нет (title у него или у двух предков, aria-describedby, элемент подсказки)`,
        );
      }
    }
  }

  if (o.rules["title-width"]) {
    for (const t of snap.textBoxes) {
      const longEnough = t.chars >= o.minTitleChars || (t.target && t.chars >= o.minTargetChars);
      if (!longEnough) {
        continue;
      }
      // Narrow alone is fine for a shrink-wrapped one-line text (a chip, a tab); it is a squeeze when the text had to
      // wrap or was cut because the box is narrower than it needs.
      const squeezed = t.width < o.minTitleWidth && (t.lines >= 2 || t.clipped);
      const tall = t.chars >= o.minTitleChars && t.lines > o.maxTitleLines && t.width < o.titleNarrowPx;
      if (squeezed) {
        add(
          "title-width",
          t.el,
          `заголовок «${t.text}» (${t.chars} зн.) сжат: ширина ${t.width} px < ${o.minTitleWidth}, строк ${t.lines}${t.clipped ? ", обрезан" : ""}${t.target ? " (заголовок строки списка)" : ""}`,
        );
      } else if (tall) {
        add("title-width", t.el, `текст «${t.text}» (${t.chars} зн.) в ${t.lines} строках при ширине ${t.width} px < ${o.titleNarrowPx} (больше ${o.maxTitleLines} строк)`);
      }
    }
  }

  if (o.rules["console"]) {
    for (const r of snap.console) {
      if (matchesIgnore(r, o) !== undefined) {
        continue;
      }
      const where = r.location ? ` (${r.location})` : "";
      add("console", r.kind === "pageerror" ? "страница (pageerror)" : "консоль (error)", `${r.text}${where}`);
    }
  }

  return capPerRule(out, o.maxPerRule);
}

/**
 * What a check really looked at, as one readable line: a pass that examined nothing would otherwise look like a pass.
 * «кнопок-значков» = icon-only buttons, «статусов» = [data-status] elements, «заголовков» = title candidates,
 * «текстов» = text nodes.
 */
export function formatCounts(c: LayoutCounts): string {
  return `кнопок-значков ${c.iconButtons}, статусов ${c.statuses}, заголовков ${c.titles}, текстов ${c.textNodes}`;
}

/** At most `max` per rule are kept, with one line that counts the rest, so a broken page does not print a thousand lines. */
export function capPerRule(list: LayoutViolation[], max: number): LayoutViolation[] {
  const seen = new Map<LayoutRule, number>();
  const out: LayoutViolation[] = [];
  const more = new Map<LayoutRule, number>();
  for (const v of list) {
    const n = (seen.get(v.rule) ?? 0) + 1;
    seen.set(v.rule, n);
    if (n <= max) {
      out.push(v);
    } else {
      more.set(v.rule, (more.get(v.rule) ?? 0) + 1);
    }
  }
  for (const [rule, n] of more) {
    out.push({ rule, element: "(остальные)", detail: `и ещё ${n} нарушений этого правила` });
  }
  return out;
}

// --- agent thread ---

function overlap(a: Box, b: Box): { w: number; h: number } {
  return { w: Math.min(a.right, b.right) - Math.max(a.left, b.left), h: Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) };
}

/** Overlaps of the «Стоп»/progress row (and of any sticky/fixed element) with message text, per scroll position. */
export function evaluateAgentThread(snap: AgentThreadSnapshot, options: LayoutOptions = {}): LayoutViolation[] {
  const o = resolveOptions(options);
  if (!o.rules["agent-overlap"]) {
    return [];
  }
  const out: LayoutViolation[] = [];
  for (const pos of snap.positions) {
    pos.blockers.forEach((b, bi) => {
      for (const t of pos.texts) {
        if (t.related.includes(bi)) {
          continue;
        }
        const ov = overlap(b.box, t.box);
        if (ov.w > o.overlapPx && ov.h > o.overlapPx) {
          const what = b.kind === "row" ? "строка «Агент отвечает…/Стоп»" : `плавающий элемент (position: ${b.position})`;
          out.push({
            rule: "agent-overlap",
            element: b.el,
            detail: `${what} перекрывает текст «${t.text}» (${t.el}) на ${Math.round(ov.w)}x${Math.round(ov.h)} px; прокрутка ленты: ${pos.label} (scrollTop ${Math.round(pos.scrollTop)})`,
          });
        }
      }
    });
  }
  return capPerRule(out, o.maxPerRule);
}

/** The text of the error thrown for a screen: starts with `Раскладка «<screen>»`, then one line per violation. */
export function formatViolations(screen: string, violations: LayoutViolation[]): string {
  const first = violations[0];
  const head = `Раскладка «${screen}»: ${violations.length} ${pluralViolations(violations.length)}${first ? `, первое: [${first.rule}] ${first.element}` : ""}`;
  return [head, ...violations.map(lineOf)].join("\n");
}

export function lineOf(v: LayoutViolation): string {
  return `  - [${v.rule}] ${v.element} — ${v.detail}`;
}

export function pluralViolations(n: number): string {
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) {
    return "нарушение";
  }
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) {
    return "нарушения";
  }
  return "нарушений";
}
