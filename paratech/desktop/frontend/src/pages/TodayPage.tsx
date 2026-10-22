import type { PageId } from "../nav.js";
import { RecordingsPage } from "./RecordingsPage.js";

// «Сегодня»: a placeholder until the main screen of TASK-125 replaces this file. Until then the section holds what the
// former home screen did, the recordings and tasks, so nothing is lost.
export function TodayPage(props: { onOpen: (id: PageId) => void; onOpenAssignment: (id: string) => void }) {
  return <RecordingsPage onOpen={props.onOpen} onOpenAssignment={props.onOpenAssignment} />;
}
