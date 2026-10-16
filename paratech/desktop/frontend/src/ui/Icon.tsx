// Inline SVG sprite: the icons drawn for the concept (docs/design/concept-final.html), 24 grid, stroked in currentColor.
// Only the icons the UI uses are here; a missing one is copied from Lucide and listed in docs/refs/ui-fonts-icons.md.

export type IconName =
  | "rec"
  | "cal"
  | "link"
  | "model"
  | "set"
  | "diag"
  | "stop"
  | "play"
  | "plug"
  | "off"
  | "ok"
  | "warn"
  | "wave"
  | "clock"
  | "chev"
  | "up"
  | "down"
  | "lock"
  | "key"
  | "doc"
  | "mail"
  | "x";

/** Rendered once per window, before any Icon. */
export function IconSprite() {
  return (
    <svg className="icon-sprite" aria-hidden="true" focusable="false">
      <symbol id="i-rec" viewBox="0 0 24 24">
        <rect x="9" y="3" width="6" height="11" rx="3" />
        <path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21" />
      </symbol>
      <symbol id="i-cal" viewBox="0 0 24 24">
        <rect x="3.5" y="5" width="17" height="15" rx="2" />
        <path d="M3.5 10h17M8 3v4M16 3v4" />
      </symbol>
      <symbol id="i-link" viewBox="0 0 24 24">
        <path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1" />
      </symbol>
      <symbol id="i-model" viewBox="0 0 24 24">
        <path d="M4 12h.01M8 8v8M12 4.5v15M16 8.5v7M20 12h.01" />
      </symbol>
      <symbol id="i-set" viewBox="0 0 24 24">
        <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
        <circle cx="16" cy="7" r="2" />
        <circle cx="10" cy="17" r="2" />
      </symbol>
      <symbol id="i-diag" viewBox="0 0 24 24">
        <path d="M4 16a8 8 0 0 1 16 0M12 16l3.6-4.2M12 8v1.6M6.3 10.3l1.1 1.1M17.7 10.3l-1.1 1.1" />
      </symbol>
      <symbol id="i-stop" viewBox="0 0 24 24">
        <rect x="7" y="7" width="10" height="10" rx="1.5" />
      </symbol>
      <symbol id="i-play" viewBox="0 0 24 24">
        <path d="M8 5.5v13l10-6.5z" />
      </symbol>
      <symbol id="i-plug" viewBox="0 0 24 24">
        <path d="M9 3v5M15 3v5M6 8h12v3a6 6 0 0 1-12 0zM12 17v4" />
      </symbol>
      <symbol id="i-off" viewBox="0 0 24 24">
        <path d="M3 3l18 18M8.5 16.5a5 5 0 0 1 7 0M5 12.5a10 10 0 0 1 4-2.3M19 12.5a10 10 0 0 0-3.3-2M12 20h.01" />
      </symbol>
      <symbol id="i-ok" viewBox="0 0 24 24">
        <path d="M5 12.5l4.5 4.5L19 7.5" />
      </symbol>
      <symbol id="i-warn" viewBox="0 0 24 24">
        <path d="M12 4l9 16H3zM12 10v4M12 17h.01" />
      </symbol>
      <symbol id="i-wave" viewBox="0 0 24 24">
        <path d="M3 12h2l2-5 3 10 3-14 3 12 2-3h3" />
      </symbol>
      <symbol id="i-clock" viewBox="0 0 24 24">
        <circle cx="12" cy="12" r="8.5" />
        <path d="M12 7.5V12l3 2" />
      </symbol>
      <symbol id="i-chev" viewBox="0 0 24 24">
        <path d="M6 9l6 6 6-6" />
      </symbol>
      <symbol id="i-up" viewBox="0 0 24 24">
        <path d="M12 15V4M7.5 8.5L12 4l4.5 4.5M5 15v4h14v-4" />
      </symbol>
      <symbol id="i-down" viewBox="0 0 24 24">
        <path d="M12 4v11M7.5 10.5L12 15l4.5-4.5M5 19h14" />
      </symbol>
      <symbol id="i-key" viewBox="0 0 24 24">
        <circle cx="8" cy="15" r="4" />
        <path d="M11 12l8-8M16 7l2 2M14 9l2 2" />
      </symbol>
      <symbol id="i-lock" viewBox="0 0 24 24">
        <rect x="5" y="10.5" width="14" height="10" rx="2" />
        <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5" />
      </symbol>
      <symbol id="i-doc" viewBox="0 0 24 24">
        <path d="M6.5 3.5h7.5l3.5 3.5v13.5h-11z" />
        <path d="M13.5 3.5V7.5h4M9.5 12h5M9.5 15.5h5" />
      </symbol>
      <symbol id="i-mail" viewBox="0 0 24 24">
        <rect x="3.5" y="5.5" width="17" height="13" rx="2" />
        <path d="M4.5 7.5l7.5 5.5 7.5-5.5" />
      </symbol>
      <symbol id="i-x" viewBox="0 0 24 24">
        <path d="M6 6l12 12M18 6L6 18" />
      </symbol>
    </svg>
  );
}

/** A decorative icon: the text next to it carries the meaning. */
export function Icon(props: { name: IconName; className?: string }) {
  return (
    <svg className={props.className ? `i ${props.className}` : "i"} aria-hidden="true" focusable="false">
      <use href={`#i-${props.name}`} />
    </svg>
  );
}
