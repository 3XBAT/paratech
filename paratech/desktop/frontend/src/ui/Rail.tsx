import { bottomGroupStart, type PageId, rail, railItemOf } from "../nav.js";
import { Icon } from "./Icon.js";

/**
 * The side rail: the icon with the name under it (TASK-348). The name is visible text, so there is no tooltip; the button
 * keeps the name as its aria-label.
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
          <span className="rail-label" aria-hidden="true">
            {item.label}
          </span>
        </button>
      ))}
    </nav>
  );
}
