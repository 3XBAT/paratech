import { bottomGroupStart, type PageId, rail, railItemOf } from "../nav.js";
import { Icon } from "./Icon.js";

/** The side rail: six icons, the name as a tooltip to the right (on hover and on keyboard focus) and as the label. */
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
