import { useEffect, useState } from "react";
import { startDelay } from "../subjectTabsView.js";

/** false for `ms` after `active` turned true, then true; false again once `active` is off. Rendering on the server: false. */
export function useAfterDelay(active: boolean, ms: number): boolean {
  const [late, setLate] = useState(false);
  useEffect(() => {
    if (!active) {
      setLate(false);
      return;
    }
    return startDelay(ms, () => setLate(true));
  }, [active, ms]);
  return active && late;
}
