// Handing a text to the input of the agent panel (the hint chips of «Сегодня»): a window event, so the panel and the
// pages stay unaware of each other. The text is only put into the field; sending stays the user's action.

export const draftEvent = "paratech:agent-draft";

/** Puts text into the panel input and focuses it. */
export function requestDraft(text: string, target: EventTarget = window): void {
  target.dispatchEvent(new CustomEvent<string>(draftEvent, { detail: text }));
}

/** The text of a draft event, "" for a foreign or malformed one. */
export function draftText(e: Event): string {
  const d = (e as CustomEvent<unknown>).detail;
  return typeof d === "string" ? d : "";
}
