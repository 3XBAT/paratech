import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon.js";

export type ChipKind = "rec" | "run" | "wait" | "net" | "ok" | "fail" | "neutral";

/** A status label: colour, icon and word together, never colour alone (docs/design/brief.md section 4). */
export function StatusChip(props: { kind: ChipKind; icon?: IconName | null; children: ReactNode; title?: string }) {
  return (
    <span className={`chip s-${props.kind}`} title={props.title}>
      {props.kind === "rec" ? <span className="dot" aria-hidden="true" /> : props.icon && <Icon name={props.icon} />}
      <span className="chip-text">{props.children}</span>
    </span>
  );
}
