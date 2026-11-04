// Layout invariants of ADR-0035 §5 / TASK-376: the data model. The page-side probe (probe.ts) returns a LayoutSnapshot,
// a pure function (evaluate.ts) turns it into violations, so every rule is testable without a browser.

export type LayoutRule =
  | "window-scroll"
  | "overflow-x"
  | "icon-button"
  | "status-quiet"
  | "raw-text"
  | "title-width"
  | "pane-scroll"
  | "clipped"
  | "truncated"
  | "console"
  | "agent-overlap";

export const allLayoutRules: readonly LayoutRule[] = [
  "window-scroll",
  "overflow-x",
  "icon-button",
  "status-quiet",
  "raw-text",
  "title-width",
  "pane-scroll",
  "clipped",
  "truncated",
  "console",
  "agent-overlap",
];

export interface LayoutViolation {
  rule: LayoutRule;
  /** A short readable locator of the element: tag, role/aria-label, text start, class path. */
  element: string;
  detail: string;
}

/** A console error or an uncaught page error seen since the previous check (watch.ts). */
export interface ConsoleRecord {
  kind: "console" | "pageerror";
  text: string;
  /** url:line of the console message, when the browser gave one. */
  location?: string;
}

/** A console record the caller chose to ignore. The reason is mandatory: nothing is ignored silently. */
export interface ConsoleIgnore {
  /** Regular expression source matched against the text of the record. */
  pattern: string;
  reason: string;
}

export interface LayoutOptions {
  /** Every rule is on unless switched off here (for tests). */
  rules?: Partial<Record<LayoutRule, boolean>>;
  /**
   * Slack in CSS px for scrollHeight/scrollWidth/right-edge comparisons. With a fractional devicePixelRatio (125 % on the
   * dev machine) the browser rounds scroll sizes to whole pixels, so an exact fit can read one pixel over. Default 1.
   */
  tolerancePx?: number;
  /** A visible text run of at least this many characters is a «title» (rows of lists, names). Default 12. */
  minTitleChars?: number;
  /** A targeted title (selectors below) of at least this many characters is checked too. Default 3. */
  minTargetChars?: number;
  /** A squeezed title is narrower than this (CSS px). Default 96. */
  minTitleWidth?: number;
  /** A text of >= minTitleChars wrapping into more lines than this while narrower than titleNarrowPx. Default 4. */
  maxTitleLines?: number;
  /** See maxTitleLines. Default 160. */
  titleNarrowPx?: number;
  /** CSS selectors of the row/list titles of the app. Default: defaultTitleSelectors. */
  titleSelectors?: string[];
  /**
   * Class names of tooltip elements (the text of a descendant with one of them, or with role=tooltip, is the tooltip of
   * the button). Default: defaultTooltipClasses.
   */
  tooltipClasses?: string[];
  /** Raw-text pattern (source and flags). Default: defaultRawText. */
  rawText?: { source: string; flags: string };
  /** Console records to ignore, each with a reason. */
  ignoreConsole?: ConsoleIgnore[];
  /** At most this many violations per rule are listed, the rest is one «ещё N» line. Default 15. */
  maxPerRule?: number;
  /** Agent thread: an overlap bigger than this many px in both directions is a violation. Default 2. */
  overlapPx?: number;
  /** Top-level scroll panes that must not scroll sideways (CSS selectors). Default: defaultPaneSelectors. */
  paneSelectors?: string[];
  /** Panes of paneSelectors (by selector text) that are MEANT to scroll sideways. Default: none. */
  sidewaysPanes?: string[];
  /** A control or text cut by an `overflow: hidden|clip` ancestor by at most this many px is not «clipped». Default 2. */
  clipSlackPx?: number;
}

/**
 * Tooltip classes of the app. `tip` is the side rail's (ui/Rail.tsx, the owner's design: an icon rail with tooltips),
 * `ia-tip` is IconAction's (ui/IconAction.tsx, TASK-368), `ag-ib-tip` the agent's message actions (agent/MessageActions.tsx).
 * All three are a span with the name of the button; `title` is the other accepted form.
 */
export const defaultTooltipClasses: readonly string[] = ["tip", "ia-tip", "ag-ib-tip"];

/**
 * Row and list title classes found in frontend/src (TSX): `.t` inside `.tx` (shared Row, file rows, task rows),
 * `.kb-name` (lessons of a subject), `.nm` (subject tree), `.ln-title` (schedule preview), `.tk-title` (tickets),
 * `.hw-title` (assignment page), `.today-t` (Сегодня), `.ag-asg-t` (assignment rows of the agent), `.own-note-title`,
 * `.del-title`.
 */
export const defaultTitleSelectors: readonly string[] = [
  ".tx > .t",
  ".kb-name",
  ".tree .nm",
  ".ln-title",
  ".tk-title",
  ".hw-title",
  ".today-t",
  ".ag-asg-t",
  ".own-note-title",
  ".del-title",
];

/** The scroll panes of the shell: main.sheet is the page body, aside.ag the agent panel, .ag-thread its message list. */
export const defaultPaneSelectors: readonly string[] = ["main.sheet", "aside.ag", ".ag-thread"];

/**
 * Text that must never reach the screen: JS leftovers (undefined, NaN, [object, null, Invalid Date, Error:, TypeError) and
 * the forms of backend / transport errors (a Connect code in brackets, «permanent failure», «hub: », context and network
 * errors, SQL). It is matched against visible text, the title / aria-label / placeholder of visible elements and the text
 * of tooltips. A container with `data-raw-ok` (the diagnostics log) is skipped.
 */
export const defaultRawText = {
  source:
    "\\bundefined\\b|\\bNaN\\b|\\[object |Error:|TypeError|\\bnull\\b|Invalid Date" +
    "|\\[(canceled|unknown|invalid_argument|deadline_exceeded|not_found|already_exists|permission_denied|resource_exhausted|failed_precondition|aborted|unimplemented|internal|unavailable|unauthenticated)\\]" +
    "|permanent failure|\\bhub: |context (canceled|deadline exceeded)|dial tcp|connection refused|sql: ",
  flags: "",
};

export interface ResolvedOptions {
  rules: Record<LayoutRule, boolean>;
  tolerancePx: number;
  minTitleChars: number;
  minTargetChars: number;
  minTitleWidth: number;
  maxTitleLines: number;
  titleNarrowPx: number;
  titleSelectors: string[];
  tooltipClasses: string[];
  rawText: { source: string; flags: string };
  ignoreConsole: ConsoleIgnore[];
  maxPerRule: number;
  overlapPx: number;
  paneSelectors: string[];
  sidewaysPanes: string[];
  clipSlackPx: number;
}

export function resolveOptions(o: LayoutOptions = {}): ResolvedOptions {
  const rules = {} as Record<LayoutRule, boolean>;
  for (const r of allLayoutRules) {
    rules[r] = o.rules?.[r] ?? true;
  }
  for (const ig of o.ignoreConsole ?? []) {
    if (ig.reason.trim() === "") {
      throw new Error(`layout: ignoreConsole ${JSON.stringify(ig.pattern)} needs a reason`);
    }
    new RegExp(ig.pattern); // a bad pattern fails here, not in the middle of a check
  }
  return {
    rules,
    tolerancePx: o.tolerancePx ?? 1,
    minTitleChars: o.minTitleChars ?? 12,
    minTargetChars: o.minTargetChars ?? 3,
    minTitleWidth: o.minTitleWidth ?? 96,
    maxTitleLines: o.maxTitleLines ?? 4,
    titleNarrowPx: o.titleNarrowPx ?? 160,
    titleSelectors: o.titleSelectors ?? [...defaultTitleSelectors],
    tooltipClasses: o.tooltipClasses ?? [...defaultTooltipClasses],
    rawText: o.rawText ?? { ...defaultRawText },
    ignoreConsole: o.ignoreConsole ?? [],
    maxPerRule: o.maxPerRule ?? 15,
    overlapPx: o.overlapPx ?? 2,
    paneSelectors: o.paneSelectors ?? [...defaultPaneSelectors],
    sidewaysPanes: o.sidewaysPanes ?? [],
    clipSlackPx: o.clipSlackPx ?? 2,
  };
}

/** What the page-side probe needs; plain JSON. */
export interface ProbeOptions {
  tolerancePx: number;
  minTitleChars: number;
  minTargetChars: number;
  minTitleWidth: number;
  maxTitleLines: number;
  titleNarrowPx: number;
  titleSelectors: string[];
  tooltipClasses: string[];
  rawText: { source: string; flags: string };
  paneSelectors: string[];
  clipSlackPx: number;
  maxItems: number;
}

export function probeOptions(o: ResolvedOptions): ProbeOptions {
  return {
    tolerancePx: o.tolerancePx,
    minTitleChars: o.minTitleChars,
    minTargetChars: o.minTargetChars,
    minTitleWidth: o.minTitleWidth,
    maxTitleLines: o.maxTitleLines,
    titleNarrowPx: o.titleNarrowPx,
    titleSelectors: o.titleSelectors,
    tooltipClasses: o.tooltipClasses,
    rawText: o.rawText,
    paneSelectors: o.paneSelectors,
    clipSlackPx: o.clipSlackPx,
    maxItems: 200,
  };
}

export interface BorderSide {
  side: "top" | "right" | "bottom" | "left";
  style: string;
  width: string;
  color: string;
}

export interface LayoutSnapshot {
  url: string;
  viewport: { width: number; height: number; dpr: number };
  doc: { scrollHeight: number; scrollWidth: number; bodyScrollWidth: number; scrollTop: number };
  /** The visible, unclipped element with the lowest bottom edge (named when the window scrolls). */
  lowest: { el: string; bottom: number } | null;
  /** Outermost visible elements whose right edge is past the viewport and that no ancestor clips inside it. */
  overflowX: Array<{ el: string; left: number; right: number; width: number }>;
  /** Visible buttons/links/[role=button|tab|menuitem] without visible text, or with a lone glyph (×, …, +) as the text. */
  iconButtons: Array<{ el: string; label: string; title: string; tip: string; glyph?: string }>;
  iconButtonsChecked: number;
  statuses: Array<{
    el: string;
    status: string;
    backgroundColor: string;
    backgroundImage: string;
    boxShadow: string;
    outlineStyle: string;
    outlineWidth: string;
    focused: boolean;
    borders: BorderSide[];
  }>;
  /** `source` is where the text was found: visible text (default), an attribute, or a tooltip element. */
  rawText: Array<{ el: string; match: string; context: string; source?: RawSource }>;
  /** Top-level scroll panes (paneSelectors) whose content is wider than the pane. */
  panes: Array<{ selector: string; el: string; scrollWidth: number; clientWidth: number }>;
  /** Content cut for good by an `overflow: hidden|clip` ancestor or box (no scrollbar, no line-clamp). */
  clipped: Array<
    | { kind: "control"; el: string; by: string; axis: "x" | "y"; cutPx: number }
    | { kind: "text"; el: string; text: string; scrollHeight: number; clientHeight: number }
  >;
  /** Texts cut by overflow (ellipsis, line-clamp, row titles) and whether a tooltip carries the full text. */
  truncated: Array<{ el: string; text: string; axis: "x" | "y"; hasTip: boolean }>;
  /** Narrow or tall text runs (a prefilter of the probe; evaluate decides). */
  textBoxes: Array<{
    el: string;
    text: string;
    chars: number;
    width: number;
    lines: number;
    clipped: boolean;
    target: boolean;
  }>;
  console: ConsoleRecord[];
  /** What the probe really examined, so a pass cannot be vacuous (see formatCounts). */
  counts: LayoutCounts;
}

export type RawSource = "text" | "title" | "aria-label" | "placeholder" | "tooltip";

export interface LayoutCounts {
  /** Visible elements of the window. */
  visibleElements: number;
  /** Visible buttons, links and role=button|tab|menuitem elements. */
  buttons: number;
  /** Of those, the icon-only ones (no visible text, or a lone glyph). */
  iconButtons: number;
  /** Visible [data-status] elements. */
  statuses: number;
  /** Title candidates: visible text runs of at least minTitleChars, or of minTargetChars when a row-title selector matches. */
  titles: number;
  /** Visible non-empty text nodes. */
  textNodes: number;
  /** Visible top-level scroll panes (paneSelectors). */
  panes: number;
  /** Visible controls examined for clipping. */
  controls: number;
  /** Texts that are cut by overflow (ellipsis, line-clamp). */
  cutTexts: number;
}

export function emptyCounts(): LayoutCounts {
  return { visibleElements: 0, buttons: 0, iconButtons: 0, statuses: 0, titles: 0, textNodes: 0, panes: 0, controls: 0, cutTexts: 0 };
}

export function emptySnapshot(): LayoutSnapshot {
  return {
    url: "about:blank",
    viewport: { width: 1000, height: 700, dpr: 1 },
    doc: { scrollHeight: 700, scrollWidth: 1000, bodyScrollWidth: 1000, scrollTop: 0 },
    lowest: null,
    overflowX: [],
    iconButtons: [],
    iconButtonsChecked: 0,
    statuses: [],
    rawText: [],
    panes: [],
    clipped: [],
    truncated: [],
    textBoxes: [],
    console: [],
    counts: emptyCounts(),
  };
}

// --- agent thread (agent.ts) ---

export interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export interface AgentBlocker {
  el: string;
  /** `row`: the «Агент отвечает…» / «Стоп» row (.ag-think, .ag-stop); `floating`: any other sticky/fixed element. */
  kind: "row" | "floating";
  position: string;
  box: Box;
}

export interface AgentText {
  el: string;
  text: string;
  box: Box;
  /** Indices of blockers this text is an ancestor or a descendant of (those are not overlaps). */
  related: number[];
}

export interface AgentPosition {
  label: string;
  scrollTop: number;
  blockers: AgentBlocker[];
  texts: AgentText[];
}

export interface AgentThreadSnapshot {
  /** A visible `.ag-think` / `.ag-stop` was found (the row exists only while the agent works). */
  rowSeen: boolean;
  /** Why nothing could be sampled (no thread, hidden panel). */
  note?: string;
  positions: AgentPosition[];
}
