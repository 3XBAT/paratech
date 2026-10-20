// The .js extensions let the Node test runner load the compiled component.
import type { RecordingSound } from "../bindings/activity.js";
import { soundLevelModel, soundMarkPercent } from "../soundLevelView.js";

// The sound level capsule of a running recording (TASK-099): 56×4 (88×5 when large) with the «слышно» mark and a word.
// Styles are in soundLevel.css, loaded by the entry points (the Node tests render this without CSS).
export function SoundLevel(props: { sound: RecordingSound | null; large?: boolean; hint?: boolean }) {
  const m = soundLevelModel(props.sound);
  if (m === null) {
    return null;
  }
  const cls = ["lvl", m.state === "heard" ? "on" : "quiet", props.large ? "lg" : ""].filter((c) => c).join(" ");
  return (
    <>
      <span className={cls} role="img" aria-label={m.label}>
        <span className="lvl-bar" aria-hidden="true">
          {/* One element across renders, so the transition smooths the steps between reads. */}
          <b style={m.state === "heard" ? { transform: `scaleX(${m.percent / 100})` } : undefined} />
          <i style={{ left: `${soundMarkPercent}%` }} />
        </span>
        <span className="num" aria-hidden="true">
          {m.word}
        </span>
      </span>
      {props.hint && m.hint !== "" && <span className="lvl-hint">{m.hint}</span>}
    </>
  );
}
