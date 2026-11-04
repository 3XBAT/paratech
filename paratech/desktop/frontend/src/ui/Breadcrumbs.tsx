/**
 * One way back (ADR-0034 §2, TASK-350): the path of a nested screen, left above its title. Every level but the last is
 * a button that does what the old «Назад» did (the page passes its own callback, so the route history of the window
 * keeps working); the last level is the screen itself, plain text. Labels may be model or student text: React text only.
 */
export interface Crumb {
  label: string;
  /** The levels above the screen. Not used for the last crumb. */
  onClick?: () => void;
}

export const crumbsLabel = "Путь";
export const crumbsSeparator = "/";
/** Labels longer than this (in characters) are the ones an ancestor crumb may be cut at; keep in step with the floor in styles.css. */
export const crumbLongFrom = 12;

export function Breadcrumbs(props: { items: readonly Crumb[] }) {
  const { items } = props;
  return (
    <nav className="crumbs" aria-label={crumbsLabel}>
      <ol>
        {items.map((c, i) => {
          const last = i === items.length - 1;
          // Every crumb has the whole text as a tooltip: whether the line cuts it depends on the width of the sheet,
          // unknown at render time.
          const title = c.label;
          // Only an ancestor that the line can really cut gets the floor (CSS .long); a short one keeps its own width.
          const cls = !last && c.label.length > crumbLongFrom ? "long" : undefined;
          return (
            <li key={`${i}:${c.label}`} className={cls}>
              {last || c.onClick === undefined ? (
                <span className="crumb cur" aria-current={last ? "page" : undefined} title={title}>
                  {c.label}
                </span>
              ) : (
                <button type="button" className="crumb link" title={title} onClick={c.onClick}>
                  {c.label}
                </button>
              )}
              {!last && (
                <span className="crumb-sep" aria-hidden="true">
                  {crumbsSeparator}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
