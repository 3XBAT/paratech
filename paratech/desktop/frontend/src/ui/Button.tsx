import { type ButtonHTMLAttributes, type ReactNode, useId } from "react";
import { Icon, type IconName } from "./Icon.js";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";

export interface ButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "className"> {
  variant?: ButtonVariant;
  /** Small (24px) or large (36px); the default is 30px. */
  size?: "sm" | "lg";
  icon?: IconName;
  /** Shows busyLabel instead of the label; the width stays the same. Disabling stays with the caller. */
  busy?: boolean;
  busyLabel?: string;
  /** Shown next to a disabled button: a disabled control always says why. */
  disabledReason?: string;
  className?: string;
  children: ReactNode;
}

export function Button({
  variant = "secondary",
  size,
  icon,
  busy = false,
  busyLabel,
  disabledReason,
  className,
  children,
  type = "button",
  ...rest
}: ButtonProps) {
  const reasonId = useId();
  const reason = rest.disabled && disabledReason ? disabledReason : "";
  const cls = ["btn", variant === "secondary" ? "" : variant, size ?? "", className ?? ""].filter((c) => c).join(" ");
  const button = (
    <button type={type} className={cls} aria-describedby={reason ? reasonId : undefined} {...rest}>
      {icon && <Icon name={icon} />}
      {busyLabel === undefined ? (
        children
      ) : (
        // Both labels share one grid cell, so the button is as wide as the longer one.
        <span className="btn-labels">
          <span className={busy ? "btn-off" : undefined}>{children}</span>
          <span className={busy ? undefined : "btn-off"}>{busyLabel}</span>
        </span>
      )}
    </button>
  );
  if (!reason) {
    return button;
  }
  return (
    <span className="btn-with-reason">
      {button}
      <span id={reasonId} className="small muted">
        {reason}
      </span>
    </span>
  );
}

/**
 * A compact round button with only an icon; its label is the tooltip and the accessible name. While busy it is
 * disabled and aria-busy, so a second click cannot start the action twice.
 */
export function IconButton(props: { icon: IconName; label: string; busy?: boolean; disabled?: boolean; onClick: () => void }) {
  const busy = props.busy ?? false;
  return (
    <button
      type="button"
      className="btn icon-btn"
      aria-label={props.label}
      title={props.label}
      aria-busy={busy || undefined}
      disabled={busy || props.disabled}
      onClick={props.onClick}
    >
      <Icon name={props.icon} />
    </button>
  );
}
