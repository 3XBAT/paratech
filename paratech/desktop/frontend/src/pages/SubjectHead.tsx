import { useRef } from "react";
import type { LibrarySubject } from "../bindings/library.js";
import { subjectSubLine } from "../libraryView.js";
import { headTexts } from "../subjectTabsView.js";
import { removalTexts } from "../subjectRemovalView.js";
import { Button } from "../ui/Button.js";
import { RowMenu, type RowMenuItem } from "../ui/RowMenu.js";

/**
 * The head of the page of a subject (TASK-353): the name with its sub-lines, «Билеты к экзамену» and «⋯». The dangerous
 * «Удалить предмет» lives in the menu, not next to the useful buttons; the question it opens returns the focus to the
 * «⋯» button (`onRemove` gets it as the opener). Names are typed or imported text: text nodes only.
 */
export function SubjectHead(props: {
  subject: LibrarySubject;
  /** The schedule line ("" without one). */
  schedule: string;
  busy: boolean;
  onTickets: () => void;
  onRemove: (opener: HTMLElement | null) => void;
}) {
  const { subject } = props;
  const box = useRef<HTMLDivElement>(null);
  const sub = subjectSubLine(subject);
  const items: readonly RowMenuItem<"remove">[] = [
    { key: "remove", label: removalTexts.action, disabled: props.busy, hint: props.busy ? headTexts.busy : undefined },
  ];
  return (
    <div className="head">
      <div>
        <h1>{subject.name}</h1>
        {sub !== "" && <p className="small muted sub-line">{sub}</p>}
        {props.schedule !== "" && <p className="small muted sub-line">{props.schedule}</p>}
      </div>
      <div className="acts" ref={box}>
        <Button icon="doc" onClick={props.onTickets}>
          {headTexts.tickets}
        </Button>
        <RowMenu
          label={headTexts.more}
          title={headTexts.moreTitle}
          items={items}
          onPick={() => props.onRemove(box.current?.querySelector<HTMLElement>(".mmenu-btn") ?? null)}
        />
      </div>
    </div>
  );
}
