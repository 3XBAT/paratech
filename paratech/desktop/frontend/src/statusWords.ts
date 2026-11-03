// The one dictionary of the waiting states (glossary, ADR-0034 §6): every screen that says "waits for the network /
// the minutes / the charger" takes the words from here, so a status never reads two ways. Display only.

/** Why a job or a generation waits instead of running. */
export type WaitingReason = "network" | "quota" | "power" | "retry";

export const waitingWords: Readonly<Record<WaitingReason, string>> = {
  network: "Ждёт интернет",
  quota: "Ждёт пополнения минут",
  power: "Ждёт зарядки",
  retry: "Ждёт повтора",
};

/** The same words inside a longer chip: «В очереди · ждёт интернет». */
export function waitingInline(reason: WaitingReason): string {
  const w = waitingWords[reason];
  return w.charAt(0).toLowerCase() + w.slice(1);
}
