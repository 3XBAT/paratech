import { Fragment, type Key, type ReactNode } from "react";
// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { defaultLimit, type ListState, listTexts, listWindow, visibleRows } from "./listView.js";

/** Extra classes per part, added to the default ones (clist-header, clist-toggle, ...); body is the default <ul>. */
export interface CollapsibleListClassNames {
  header?: string;
  toggle?: string;
  count?: string;
  body?: string;
  footer?: string;
  button?: string;
}

export interface CollapsibleListProps<T> {
  items: readonly T[];
  itemKey: (item: T, index: number) => Key;
  /** Renders one row element: an <li> in the default <ul>, a <tr> in a table container. */
  renderItem: (item: T, index: number) => ReactNode;
  /** Controlled by the parent, so a refreshed list keeps it. */
  state: ListState;
  onStateChange: (next: ListState) => void;
  limit?: number;
  /** With a title, the header button folds the whole group. */
  title?: ReactNode;
  heading?: "h2" | "h3" | "h4";
  /** Shown next to the title; items.length by default. */
  count?: number;
  labels?: { more?: (hidden: number) => string; less?: string };
  /** Wraps the rows, e.g. in a table with its head; a <ul> by default. */
  renderContainer?: (rows: ReactNode) => ReactNode;
  /** Shown above and below the rows; folded with the group. */
  before?: ReactNode;
  after?: ReactNode;
  className?: string;
  classNames?: CollapsibleListClassNames;
}

const cx = (...names: (string | undefined)[]) => names.filter((n) => n).join(" ");

// A long list that renders at most `limit` rows until «Показать ещё». Rows are sliced before rendering: hidden rows
// are not in the DOM. The component only arranges what the caller renders; it adds no HTML of its own from data.
export function CollapsibleList<T>(props: CollapsibleListProps<T>) {
  const limit = props.limit ?? defaultLimit;
  const s = props.state;
  const w = listWindow(props.items.length, s, limit);
  const cn = props.classNames ?? {};
  const Heading = props.heading ?? "h3";
  const more = props.labels?.more ?? listTexts.more;
  const less = props.labels?.less ?? listTexts.less;

  const rows = visibleRows(props.items, s, limit).map((item, i) => (
    <Fragment key={props.itemKey(item, i)}>{props.renderItem(item, i)}</Fragment>
  ));

  return (
    <div className={cx("clist", props.className)}>
      {props.title !== undefined && (
        <Heading className={cx("clist-header", cn.header)}>
          <button
            type="button"
            className={cx("clist-toggle", cn.toggle)}
            aria-expanded={!s.collapsed}
            onClick={() => props.onStateChange({ ...s, collapsed: !s.collapsed })}
          >
            <span aria-hidden="true">{s.collapsed ? "▸ " : "▾ "}</span>
            {props.title} <span className={cx("clist-count", cn.count)}>({props.count ?? props.items.length})</span>
          </button>
        </Heading>
      )}
      {!s.collapsed && (
        <>
          {props.before}
          {rows.length > 0 &&
            (props.renderContainer ? props.renderContainer(rows) : <ul className={cx("clist-body", cn.body)}>{rows}</ul>)}
          {(w.more || w.less) && (
            <div className={cx("clist-footer", cn.footer)}>
              <button
                type="button"
                className={cx("clist-more", cn.button)}
                onClick={() => props.onStateChange({ ...s, showAll: w.more })}
              >
                {w.more ? more(w.hidden) : less}
              </button>
            </div>
          )}
          {props.after}
        </>
      )}
    </div>
  );
}
