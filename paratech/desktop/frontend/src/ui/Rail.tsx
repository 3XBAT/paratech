import { bottomGroupStart, type PageId, rail, railItemOf } from "../nav.js";
import { Icon } from "./Icon.js";

/**
 * The side rail: icons only (owner requirement). The name is a tooltip to the right (on hover and on keyboard focus) and
 * the button's aria-label; no visible text sits under the icon.
 */
export function Rail(props: { current: PageId; onOpen: (id: PageId) => void }) {
  const here = railItemOf(props.current).id;
  return (
    <nav className="rail" aria-label="Разделы">
      {rail.map((item) => (
        <button
          key={item.id}
          type="button"
          className={item.id === bottomGroupStart ? "low" : undefined}
          aria-label={item.label}
          aria-current={item.id === here ? "page" : undefined}
          onClick={() => props.onOpen(item.id)}
        >
          <Icon name={item.icon} />
          <span className="tip" aria-hidden="true">
            {item.label}
          </span>
        </button>
      ))}
    </nav>
  );
}
