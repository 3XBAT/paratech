import { type AriaAttributes, useEffect, useId, useRef } from "react";
import { firstControlSelector, focusMove } from "../disclosureFocus.js";

/**
 * The focus of a panel that a button opens («Импортировать запись», TASK-302): opening moves it to the first enabled control
 * of the panel, closing gives it back to the opener (see focusMove). It acts only when `open` changes, never on a re-render.
 * The opener and the panel are found by id, so the opener may be replaced by its twin (the empty state and the head of
 * «Записи» are never shown together). Put `openerProps` on the button and `panelId` on the panel.
 */
export function useDisclosureFocus(open: boolean) {
  const openerId = useId();
  const panelId = useId();
  const was = useRef(open);

  useEffect(() => {
    const at = document.activeElement;
    const move = focusMove(was.current, open, at === null ? null : { id: at.id, isBody: at === document.body }, openerId);
    was.current = open;
    if (move === "panel") {
      document.getElementById(panelId)?.querySelector<HTMLElement>(firstControlSelector)?.focus();
    } else if (move === "opener") {
      document.getElementById(openerId)?.focus();
    }
  }, [open, openerId, panelId]);

  const openerProps: { id: string } & Pick<AriaAttributes, "aria-expanded" | "aria-controls"> = {
    id: openerId,
    "aria-expanded": open,
    // The panel exists only while it is open: a reference to a missing id is not given.
    "aria-controls": open ? panelId : undefined,
  };
  return { panelId, openerProps };
}
