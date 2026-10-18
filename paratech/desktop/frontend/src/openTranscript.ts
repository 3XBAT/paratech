// Opening the transcript viewer from any page (TASK-079): `const open = useOpenTranscript(); open(recording.id)`.
// App provides the function; the viewer opens over the current section and «Назад» returns to it.
import { createContext, useContext } from "react";

export type OpenTranscript = (recordingId: string) => void;

export const OpenTranscriptContext = createContext<OpenTranscript>(() => undefined);

/** The function that opens the transcript of a recording by its id. */
export function useOpenTranscript(): OpenTranscript {
  return useContext(OpenTranscriptContext);
}
