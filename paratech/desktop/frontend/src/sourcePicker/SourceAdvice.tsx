// The silent-source advice in the row of the running recording (TASK-082): a warning note with «Переключиться» and
// «Оставить». No dialog and no focus: the note is a polite live region. Names are untrusted: React renders them as
// text. The .js extensions let the Node test runner load the compiled modules.
import type { AdviceView } from "../bindings/activity.js";
import { Button } from "../ui/Button.js";
import { Icon } from "../ui/Icon.js";
import { adviceHints, adviceLabels, adviceModel, type AdvicePending } from "./advice.js";

export interface SourceAdviceProps {
  advice: AdviceView;
  pending: AdvicePending;
  /** Why the last click did not help; "" for none. */
  problem: string;
  onSwitch: () => void;
  onKeep: () => void;
}

export function SourceAdvice(props: SourceAdviceProps) {
  const m = adviceModel(props.advice);
  const busy = props.pending !== null;
  return (
    <div className="note s-wait source-advice" role="status">
      <Icon name="warn" />
      <div>
        <p className="source-advice-text" title={m.hint}>
          {m.text}
        </p>
        {props.problem !== "" && <p className="small source-advice-problem">{props.problem}</p>}
        <div className="acts start">
          <Button
            variant="primary"
            busy={props.pending === "switch"}
            busyLabel={adviceLabels.switching}
            disabled={busy}
            title={adviceHints.switch}
            onClick={props.onSwitch}
          >
            {adviceLabels.switch}
          </Button>
          <Button variant="ghost" disabled={busy} title={adviceHints.keep} onClick={props.onKeep}>
            {adviceLabels.keep}
          </Button>
        </div>
      </div>
    </div>
  );
}
