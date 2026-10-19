// The source picker of the popup, «Диагностика» and «Записи и задачи» (TASK-081): one row like a select that opens the
// list of the open apps over the rest. It holds no data: the owner passes the model and gets the choice back. Styles
// are in sourcePicker.css, loaded by both windows. The .js extensions let the Node test runner load the compiled
// modules; TypeScript and Vite map them to the .ts and .tsx files.
import { useId } from "react";
import { Icon } from "../ui/Icon.js";
import { sourceMarks, type PickerModel, type SourceOption } from "./view.js";

export interface SourcePickerProps {
  picker: PickerModel;
  disabled: boolean;
  open: boolean;
  onToggle: () => void;
  onClose: () => void;
  onChoose: (app: string) => void;
  /** Id of the visible label of the picker, if it has one. */
  labelId?: string;
  /** Why the picker is disabled; shown on hover and read as its description. */
  disabledReason?: string;
}

export function SourcePicker(props: SourcePickerProps) {
  const { picker } = props;
  const reasonId = useId();
  const listShown = props.open && !props.disabled;
  const reason = props.disabled && props.disabledReason ? props.disabledReason : "";
  return (
    <div className="picker">
      <button
        type="button"
        className="source chosen"
        disabled={props.disabled}
        aria-haspopup="listbox"
        aria-expanded={listShown}
        aria-describedby={reason ? reasonId : undefined}
        title={reason || picker.chosen.hint}
        onClick={props.onToggle}
      >
        <SourceRow option={picker.chosen} />
        <Icon name="chev" className="caret" />
      </button>
      {reason && (
        <span id={reasonId} className="vh">
          {reason}
        </span>
      )}
      {listShown && (
        <>
          <div className="backdrop" onClick={props.onClose} />
          <ul className="list" role="listbox" aria-labelledby={props.labelId}>
            {picker.options.map((o) => (
              <li key={o.app} role="option" aria-selected={o.app === picker.chosen.app}>
                <button
                  type="button"
                  className={o.app === picker.chosen.app ? "source current" : "source"}
                  title={o.hint}
                  onClick={() => props.onChoose(o.app)}
                >
                  <SourceRow option={o} />
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

// SourceRow renders one source as text: names and titles are untrusted, and the icon is only a validated PNG data URL.
function SourceRow({ option }: { option: SourceOption }) {
  return (
    <>
      {option.icon !== "" ? (
        <img className="icon" src={option.icon} alt="" draggable={false} />
      ) : (
        <span className={option.app === "" ? "icon letter whole" : "icon letter"} aria-hidden="true">
          {option.letter}
        </span>
      )}
      <span className="name">{option.name}</span>
      {option.gone && <span className="mark gone">{sourceMarks.gone}</span>}
      {option.muted && <span className="mark muted">{sourceMarks.muted}</span>}
      {option.playing && <span className="mark playing">{sourceMarks.playing}</span>}
    </>
  );
}

/** The warnings about the chosen source and, if asked, the general advice: small plain text under the picker. */
export function SourceNotes(props: { picker: PickerModel; advice?: boolean }) {
  return (
    <>
      {props.picker.warnings.map((w) => (
        <p key={w} className="small picker-warning">
          {w}
        </p>
      ))}
      {props.advice && <p className="small muted">{props.picker.advice}</p>}
    </>
  );
}
