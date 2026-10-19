// Opening the summary viewer from any page (TASK-087): `const open = useOpenSummary(); open(recording.id)`. App
// provides the function; the viewer opens over the current section and «Назад» returns to it.
import { createContext, useContext } from "react";

/** Where the summary page opens: "lecture" scrolls to the lecture text (TASK-090); undefined: the top. */
export type SummarySection = "lecture";

export type OpenSummary = (recordingId: string, section?: SummarySection) => void;

export const OpenSummaryContext = createContext<OpenSummary>(() => undefined);

/** The function that opens the summary of a recording by its id. */
export function useOpenSummary(): OpenSummary {
  return useContext(OpenSummaryContext);
}
