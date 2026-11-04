// The page-side probe of the layout invariants. probeLayout runs INSIDE the page (page.evaluate of its source), so it is
// self-contained: no imports, no closures, only DOM globals and its own argument. It measures and describes; it decides
// nothing about pass/fail except which elements are worth reporting (evaluate.ts decides).
import type { LayoutSnapshot, ProbeOptions } from "./types.js";

export function probeLayout(opts: ProbeOptions): LayoutSnapshot {
  const doc = document;
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const tol = opts.tolerancePx;

  const styles = new WeakMap<Element, CSSStyleDeclaration>();
  const style = (e: Element): CSSStyleDeclaration => {
    let s = styles.get(e);
    if (s === undefined) {
      s = getComputedStyle(e);
      styles.set(e, s);
    }
    return s;
  };

  // shown: neither the element nor an ancestor is display:none or fully transparent (opacity 0 hides the subtree: the
  // rail's .tip is opacity 0 until hover).
  const shownCache = new WeakMap<Element, boolean>();
  const shown = (e: Element): boolean => {
    const c = shownCache.get(e);
    if (c !== undefined) {
      return c;
    }
    const s = style(e);
    let v = s.display !== "none" && Number(s.opacity) !== 0;
    if (v && e.parentElement !== null) {
      v = shown(e.parentElement);
    }
    shownCache.set(e, v);
    return v;
  };

  // visible: shown, visibility:visible, has a box, and is not a 1x1 visually-hidden helper (.ag-vh, .vh, .ia-vh use a
  // 1px clipped box and carry text for screen readers only).
  const visible = (e: Element): boolean => {
    if (!shown(e)) {
      return false;
    }
    const s = style(e);
    if (s.visibility !== "visible" || s.display === "contents") {
      return false;
    }
    const r = e.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && !(r.width <= 1 && r.height <= 1);
  };

  const norm = (t: string): string => t.replace(/\s+/g, " ").trim();
  const cut = (t: string, n: number): string => (t.length > n ? t.slice(0, n - 1) + "…" : t);

  const ownText = (e: Element): string => {
    let t = "";
    for (const n of Array.from(e.childNodes)) {
      if (n.nodeType === 3) {
        t += n.nodeValue ?? "";
      }
    }
    return norm(t);
  };

  const part = (e: Element): string => {
    let s = e.localName;
    if (e.id !== "" && !/[:«]/.test(e.id) && e.id.length <= 40) {
      s += "#" + e.id;
    }
    for (const c of Array.from(e.classList).slice(0, 2)) {
      s += "." + c;
    }
    const role = e.getAttribute("role");
    if (role) {
      s += `[role=${role}]`;
    }
    const label = e.getAttribute("aria-label");
    if (label) {
      s += `[aria-label="${cut(label, 30)}"]`;
    }
    return s;
  };

  const describe = (e: Element): string => {
    const chain: string[] = [];
    let cur: Element | null = e;
    let n = 0;
    while (cur !== null && cur !== doc.body && cur !== doc.documentElement && n < 3) {
      chain.unshift(part(cur));
      cur = cur.parentElement;
      n++;
    }
    let s = chain.join(" > ");
    if (cur !== null && cur !== doc.body && cur !== doc.documentElement) {
      s = "… > " + s;
    }
    const own = ownText(e) || (e.children.length === 0 ? norm(e.textContent ?? "") : "");
    if (own !== "") {
      s += ` «${cut(own, 40)}»`;
    }
    return s;
  };

  const isTip = (e: Element): boolean => {
    if (e.getAttribute("role") === "tooltip") {
      return true;
    }
    for (const c of opts.tooltipClasses) {
      if (e.classList.contains(c)) {
        return true;
      }
    }
    return false;
  };

  // Is `e`, which sticks out past the viewport edge on `axis`, clipped by an ancestor whose own edge is inside the
  // viewport? Then it is a pane that scrolls or cuts its content, not a window overflow. An absolute element is clipped
  // only by its containing block and the ancestors above it (transforms are not tracked); a fixed one by none.
  const clippedInside = (e: Element, axis: "x" | "y"): boolean => {
    let mode = style(e).position;
    let cur = e.parentElement;
    while (cur !== null && cur !== doc.body && cur !== doc.documentElement) {
      if (mode === "fixed") {
        return false;
      }
      const cs = style(cur);
      if (mode !== "absolute" || cs.position !== "static") {
        const clips = axis === "x" ? cs.overflowX !== "visible" : cs.overflowY !== "visible";
        if (clips) {
          const r = cur.getBoundingClientRect();
          if ((axis === "x" ? r.right : r.bottom) <= (axis === "x" ? vw : vh) + tol) {
            return true;
          }
        }
        if (mode === "absolute") {
          mode = cs.position;
        }
      }
      cur = cur.parentElement;
    }
    return false;
  };

  const all = Array.from(doc.body.querySelectorAll("*"));
  const se = doc.scrollingElement ?? doc.documentElement;

  const snap: LayoutSnapshot = {
    url: location.href,
    viewport: { width: vw, height: vh, dpr: window.devicePixelRatio },
    doc: { scrollHeight: se.scrollHeight, scrollWidth: se.scrollWidth, bodyScrollWidth: doc.body.scrollWidth, scrollTop: se.scrollTop },
    lowest: null,
    overflowX: [],
    iconButtons: [],
    iconButtonsChecked: 0,
    statuses: [],
    rawText: [],
    textBoxes: [],
    console: [],
  };

  // 1. The element that makes the window scroll vertically: the lowest visible one no ancestor clips.
  if (se.scrollHeight > vh + tol) {
    let best: Element | null = null;
    let bestBottom = -Infinity;
    for (const e of all) {
      if (!visible(e)) {
        continue;
      }
      const b = e.getBoundingClientRect().bottom + se.scrollTop;
      if (b > bestBottom && b > vh + tol && !clippedInside(e, "y")) {
        best = e;
        bestBottom = b;
      }
    }
    if (best !== null) {
      snap.lowest = { el: describe(best), bottom: Math.round(bestBottom) };
    }
  }

  // 2. Right edge past the viewport. Skipped on purpose: hidden or zero-size elements (not visible), 1x1 visually-hidden
  // helpers (visible() above), elements an ancestor clips inside the viewport (clippedInside), and aria-hidden elements
  // that lie wholly right of the viewport (off-canvas by design). Only the outermost offender of a subtree is listed.
  const offenders = new Set<Element>();
  const offenderList: Element[] = [];
  for (const e of all) {
    if (!visible(e)) {
      continue;
    }
    const r = e.getBoundingClientRect();
    if (r.right <= vw + tol) {
      continue;
    }
    if (e.closest('[aria-hidden="true"]') !== null && r.left >= vw - tol) {
      continue;
    }
    if (clippedInside(e, "x")) {
      continue;
    }
    offenders.add(e);
    offenderList.push(e);
  }
  for (const e of offenderList) {
    const p = e.parentElement;
    if (p !== null && offenders.has(p)) {
      continue;
    }
    if (snap.overflowX.length >= opts.maxItems) {
      break;
    }
    const r = e.getBoundingClientRect();
    snap.overflowX.push({ el: describe(e), left: Math.round(r.left), right: Math.round(r.right), width: Math.round(r.width) });
  }

  // 3. Buttons and links without visible text. The text of an icon button is empty once svgs, tooltip elements and
  // invisible content are left out: the rail's .tip is opacity 0 but turns visible under the mouse, so it is skipped by
  // class, not by its current opacity.
  const visibleText = (root: Element): string => {
    let out = "";
    const walk = (n: Node): void => {
      if (n.nodeType === 3) {
        out += n.nodeValue ?? "";
        return;
      }
      if (n.nodeType !== 1) {
        return;
      }
      const el = n as Element;
      if (el.localName === "svg" || isTip(el) || !shown(el)) {
        return;
      }
      const s = style(el);
      if (s.visibility !== "visible") {
        return;
      }
      if (s.display !== "contents") {
        const r = el.getBoundingClientRect();
        if (r.width <= 1 && r.height <= 1) {
          return;
        }
      }
      for (const c of Array.from(el.childNodes)) {
        walk(c);
      }
    };
    for (const c of Array.from(root.childNodes)) {
      walk(c);
    }
    return norm(out);
  };

  const labelOf = (e: Element): string => {
    const direct = norm(e.getAttribute("aria-label") ?? "");
    if (direct !== "") {
      return direct;
    }
    const ids = norm(e.getAttribute("aria-labelledby") ?? "");
    if (ids === "") {
      return "";
    }
    return norm(
      ids
        .split(" ")
        .map((id) => doc.getElementById(id)?.textContent ?? "")
        .join(" "),
    );
  };

  const tipOf = (e: Element): string => {
    for (const d of Array.from(e.querySelectorAll("*"))) {
      if (isTip(d)) {
        const t = norm(d.textContent ?? "");
        if (t !== "") {
          return t;
        }
      }
    }
    return "";
  };

  for (const e of Array.from(doc.body.querySelectorAll('button, a[href], [role="button"]'))) {
    if (!visible(e)) {
      continue;
    }
    snap.iconButtonsChecked++;
    if (visibleText(e) !== "") {
      continue;
    }
    if (snap.iconButtons.length < opts.maxItems) {
      snap.iconButtons.push({ el: describe(e), label: labelOf(e), title: norm(e.getAttribute("title") ?? ""), tip: tipOf(e) });
    }
  }

  // 4. Status elements.
  for (const e of Array.from(doc.body.querySelectorAll("[data-status]"))) {
    if (!visible(e) || snap.statuses.length >= opts.maxItems) {
      continue;
    }
    const s = style(e);
    let focused = false;
    try {
      focused = e === doc.activeElement && e.matches(":focus-visible");
    } catch {
      focused = e === doc.activeElement;
    }
    snap.statuses.push({
      el: describe(e),
      status: e.getAttribute("data-status") ?? "",
      backgroundColor: s.backgroundColor,
      backgroundImage: s.backgroundImage,
      boxShadow: s.boxShadow,
      outlineStyle: s.outlineStyle,
      outlineWidth: s.outlineWidth,
      focused,
      borders: [
        { side: "top", style: s.borderTopStyle, width: s.borderTopWidth, color: s.borderTopColor },
        { side: "right", style: s.borderRightStyle, width: s.borderRightWidth, color: s.borderRightColor },
        { side: "bottom", style: s.borderBottomStyle, width: s.borderBottomWidth, color: s.borderBottomColor },
        { side: "left", style: s.borderLeftStyle, width: s.borderLeftWidth, color: s.borderLeftColor },
      ],
    });
  }

  // 5. Raw text: visible text nodes only (attributes and input values are not rendered text runs).
  const re = new RegExp(opts.rawText.source, opts.rawText.flags);
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n !== null; n = walker.nextNode()) {
    const p = n.parentElement;
    const v = n.nodeValue ?? "";
    if (p === null || v.trim() === "" || snap.rawText.length >= opts.maxItems) {
      continue;
    }
    if (["script", "style", "noscript", "textarea", "title"].includes(p.localName)) {
      continue;
    }
    const m = re.exec(v);
    if (m === null || !visible(p)) {
      continue;
    }
    const at = m.index;
    snap.rawText.push({ el: describe(p), match: m[0], context: norm(v.slice(Math.max(0, at - 20), at + m[0].length + 20)) });
  }

  // 6. Text runs squeezed into a narrow box. A «text box» is an element with a direct text run of at least minTitleChars
  // characters, or of at least minTargetChars when it matches a title selector of the app. Its width is its border box
  // (an inline element: the union of its text rects); lines are counted from the rects of its text. The probe lists only
  // narrow or tall ones; evaluate.ts applies the thresholds.
  const targetSel = opts.titleSelectors.join(",");
  const skipTags = ["script", "style", "option", "select", "textarea", "input", "svg", "title"];
  for (const e of all) {
    if (snap.textBoxes.length >= opts.maxItems || skipTags.includes(e.localName)) {
      continue;
    }
    const text = ownText(e);
    const chars = Array.from(text).length;
    if (chars < opts.minTargetChars) {
      continue;
    }
    let target = false;
    try {
      target = targetSel !== "" && e.matches(targetSel);
    } catch {
      target = false;
    }
    if (!target && chars < opts.minTitleChars) {
      continue;
    }
    if (!visible(e)) {
      continue;
    }
    const rects: DOMRect[] = [];
    for (const n of Array.from(e.childNodes)) {
      if (n.nodeType === 3 && norm(n.nodeValue ?? "") !== "") {
        const range = doc.createRange();
        range.selectNodeContents(n);
        rects.push(...Array.from(range.getClientRects()).filter((r) => r.width > 0 && r.height > 0));
      }
    }
    if (rects.length === 0) {
      continue;
    }
    rects.sort((a, b) => a.top - b.top || a.left - b.left);
    let lines = 0;
    let lineBottom = -Infinity;
    for (const r of rects) {
      if (r.top >= lineBottom - 1) {
        lines++;
        lineBottom = r.bottom;
      } else {
        lineBottom = Math.max(lineBottom, r.bottom);
      }
    }
    const s = style(e);
    const inline = s.display === "inline";
    const width = inline
      ? Math.max(...rects.map((r) => r.right)) - Math.min(...rects.map((r) => r.left))
      : e.getBoundingClientRect().width;
    const clipped = !inline && s.overflowX !== "visible" && e.scrollWidth > e.clientWidth + 1;
    if (width < Math.max(opts.minTitleWidth, opts.titleNarrowPx) || lines > opts.maxTitleLines) {
      snap.textBoxes.push({ el: describe(e), text: cut(text, 40), chars, width: Math.round(width), lines, clipped, target });
    }
  }

  return snap;
}

/** The source evaluated in the page: `(${probeLayoutSource})(options)`. */
export const probeLayoutSource: string = probeLayout.toString();
