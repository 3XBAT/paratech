// View logic of the «Google Календарь» block of "Подключения": labels and button states. The rules of the client id,
// the secret and the sign-in live in the Go core (ARCH-44); the page shows the statuses and the fixed texts from Go.

import type { GoogleAccountStatus, GoogleClientStatus, GoogleState } from "./bindings/google";

/** The action of the block in flight; cancelling a sign-in runs next to "signIn". */
export type GoogleAction = "save" | "remove" | "signIn" | "signOut" | null;

export const googleTexts = {
  title: "Google Календарь",
  hint:
    "Идентификатор и секрет берутся из своего клиента типа «Desktop app» в Google Cloud Console " +
    "(как его создать — в инструкции к альфа-сборке, раздел «Google Календарь: вход»).",
  clientTitle: "Клиент Google",
  accountTitle: "Вход",
  clientId: "Идентификатор клиента (client ID)",
  clientSecret: "Секрет клиента (client secret)",
  replaceHint: "Новые идентификатор и секрет заменят сохранённые.",
  save: "Сохранить",
  saving: "Сохраняем…",
  remove: "Удалить клиент",
  confirmRemove: "Удалить сохранённые идентификатор и секрет клиента Google?",
  confirmRemoveNote: "Чтобы снова войти через Google, клиент придётся сохранить заново.",
  confirmYes: "Удалить клиент",
  confirmNo: "Отмена",
  signIn: "Войти через Google",
  signInNeedsClient: "Сначала сохраните идентификатор и секрет клиента.",
  waiting: "Ожидание входа в браузере…",
  cancel: "Отменить",
  signOut: "Выйти",
  signingOut: "Выходим…",
  loading: "Загрузка…",
  loadFailed: "Не удалось узнать состояние входа в Google.",
  actionFailed: "Не удалось выполнить действие с Google.",
} as const;

const clientTexts: Readonly<Record<GoogleClientStatus, string>> = {
  set: "Клиент Google задан",
  unset: "Клиент Google не задан",
  unsupported: "Диспетчер учётных данных Windows недоступен в этой системе",
  unavailable: "Диспетчер учётных данных не ответил",
};

const accountTexts: Readonly<Record<GoogleAccountStatus, string>> = {
  signedIn: "Вход в Google выполнен",
  signedOut: "Вход в Google не выполнен",
  unsupported: "Вход в Google недоступен в этой системе",
  unavailable: "Не удалось узнать, выполнен ли вход в Google",
};

/** The label of a client status; an unknown one is shown as "не ответил" rather than as raw text. */
export function clientStatusText(status: string): string {
  return Object.hasOwn(clientTexts, status) ? clientTexts[status as GoogleClientStatus] : clientTexts.unavailable;
}

export function accountStatusText(status: string): string {
  return Object.hasOwn(accountTexts, status) ? accountTexts[status as GoogleAccountStatus] : accountTexts.unavailable;
}

// The chips under the «Клиент Google» and «Вход» headings: one to three words. A status that is not a normal one also
// shows its full text next to the chip (clientStatusNote, accountStatusNote).
const clientChips: Readonly<Record<GoogleClientStatus, string>> = {
  set: "Задан",
  unset: "Не задан",
  unsupported: "Недоступно",
  unavailable: "Не ответил",
};

const accountChips: Readonly<Record<GoogleAccountStatus, string>> = {
  signedIn: "Выполнен",
  signedOut: "Не выполнен",
  unsupported: "Недоступен",
  unavailable: "Не удалось проверить",
};

export function clientChipText(status: string): string {
  return Object.hasOwn(clientChips, status) ? clientChips[status as GoogleClientStatus] : clientChips.unavailable;
}

export function accountChipText(status: string): string {
  return Object.hasOwn(accountChips, status) ? accountChips[status as GoogleAccountStatus] : accountChips.unavailable;
}

/** The full text next to the chip of a client status that is not "set/unset"; "" otherwise. */
export function clientStatusNote(status: string): string {
  return status === "set" || status === "unset" ? "" : clientStatusText(status);
}

/** The full text next to the chip of an account status that is not "signedIn/signedOut"; "" otherwise. */
export function accountStatusNote(status: string): string {
  return status === "signedIn" || status === "signedOut" ? "" : accountStatusText(status);
}

/** A status that is not a normal "set/unset" is shown as a warning. */
export function statusWarns(status: string): boolean {
  return status === "unsupported" || status === "unavailable";
}

export interface GoogleControls {
  inputsDisabled: boolean;
  /** Saving needs both values typed. */
  saveDisabled: boolean;
  /** «Удалить клиент» is offered only for a stored client. */
  removeShown: boolean;
  removeDisabled: boolean;
  /** A sign-in waits for the browser: the waiting text and «Отменить» replace «Войти». */
  signingIn: boolean;
  signInShown: boolean;
  /** «Войти» waits for a stored client. */
  signInDisabled: boolean;
  signOutShown: boolean;
  signOutDisabled: boolean;
}

/** What the block allows: one action at a time, nothing without a secret store. */
export function googleControls(input: {
  state: GoogleState;
  action: GoogleAction;
  typedId: boolean;
  typedSecret: boolean;
}): GoogleControls {
  const { state, action } = input;
  const signingIn = action === "signIn" || state.signingIn;
  const busy = action !== null || signingIn;
  const unsupported = state.client === "unsupported";
  return {
    inputsDisabled: unsupported || busy,
    saveDisabled: unsupported || busy || !input.typedId || !input.typedSecret,
    removeShown: state.client === "set",
    removeDisabled: busy,
    signingIn,
    signInShown: !signingIn && state.account !== "signedIn",
    signInDisabled: busy || state.client !== "set",
    signOutShown: !signingIn && state.account === "signedIn",
    signOutDisabled: busy,
  };
}

/** How often the status is read while a sign-in started elsewhere (before the page was opened) waits. */
export const statusPollMs = 2000;

/** The page's own sign-in call answers by itself; one started before the page opened is followed by polling. */
export function pollStatus(state: GoogleState | null, action: GoogleAction): boolean {
  return state !== null && state.signingIn && action !== "signIn";
}
