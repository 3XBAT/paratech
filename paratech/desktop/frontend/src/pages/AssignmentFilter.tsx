import { type AssignmentCounts, type AssignmentFilter, assignmentFilterOptions } from "../homeworkView.js";

export const filterEmpty = "В этой группе заданий нет.";

/** The filter of the list of assignments with the number in each part; the split waits for the emails. */
export function FilterCounters(props: { counts: AssignmentCounts; value: AssignmentFilter; onChange: (f: AssignmentFilter) => void }) {
  return (
    <div className="seg hw-filter" role="group" aria-label="Какие задания показывать">
      {assignmentFilterOptions.map((o) => {
        const n = props.counts[o.value];
        // The split is unknown (emails not loaded or every load failed): only «Все» works.
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={o.value === props.value}
            disabled={n === null}
            onClick={() => props.onChange(o.value)}
          >
            {o.label}
            {n !== null && <span className="cnt">{n}</span>}
          </button>
        );
      })}
    </div>
  );
}
