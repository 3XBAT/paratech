// «Сделать под ключ» on the assignment page (TASK-186): what the block shows and the guarded start. Pure, so the tests
// run it without a browser. The click is the consent for the paid triage; nothing here marks it as paid.

import { notFoundText, type TurnkeyApi, type TurnkeyWork, turnkeyErrorText } from "./bindings/turnkey.js";

export const turnkeyTexts = {
  title: "Сделать под ключ",
  caption: "Разбор задания и вопросы, затем работа или презентация файлом",
  button: "Сделать под ключ",
  busy: "Разбор задания…",
  loading: "Загрузка…",
} as const;

/** loading: the work of the assignment is being asked for; none: no work yet (error: why the last call failed); work: the card. */
export type TurnkeyBlockState =
  | { phase: "loading" }
  | { phase: "none"; error: string }
  | { phase: "work"; workId: string; work?: TurnkeyWork };

/** The block on page open: an existing work shows its card, a missing one the button; any other failure shows its text. */
export async function loadTurnkeyBlock(api: TurnkeyApi, assignmentId: string): Promise<TurnkeyBlockState> {
  try {
    const w = await api.workOfAssignment(assignmentId);
    return { phase: "work", workId: w.id };
  } catch (err) {
    const text = turnkeyErrorText(err);
    return { phase: "none", error: text === notFoundText ? "" : text };
  }
}

/**
 * The click on «Сделать под ключ». `flight` is the single-flight guard (workView.singleFlight): a second click while the
 * call runs makes no call and answers null. A failure comes back as the state with the text of the core's error.
 */
export async function startTurnkeyOnce(
  flight: (call: () => Promise<void>) => Promise<boolean>,
  api: TurnkeyApi,
  assignmentId: string,
): Promise<TurnkeyBlockState | null> {
  let next: TurnkeyBlockState | null = null;
  await flight(async () => {
    try {
      const w = await api.startForAssignment(assignmentId);
      // The reply carries the live problem of this call; the core also stores it (lastProblem/lastMessage), but the card is seeded with the reply.
      next = { phase: "work", workId: w.id, work: w };
    } catch (err) {
      next = { phase: "none", error: turnkeyErrorText(err) };
    }
  });
  return next;
}
