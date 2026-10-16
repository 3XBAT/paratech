// Help screen bindings: calls into the Go core (internal/input/wails/help.go). The page sends only a key of a closed
// set; the core owns the URLs and opens them in the default browser.

import type { CallByName } from "./settings";

/** Keys of HelpPageURLs in help.go (a test compares the two lists). */
export const helpPageKeys = [
  "apple-iphone-accounts",
  "apple-mac-internet-accounts",
  "apple-mac-calendar-accounts",
  "apple-mac-subscribe",
  "google-apple-sync",
  "google-ical-address",
  "google-syncselect",
] as const;

export type HelpPageKey = (typeof helpPageKeys)[number];

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  openHelpPage: "paratech/desktop/internal/input/wails.HelpService.OpenHelpPage",
} as const;

/** Error texts of OpenHelpPage (ErrHelpPageUnknown, ErrHelpLinkFailed in help.go). */
export const helpErrors = {
  unknown: "Такой страницы справки нет",
  failed: "Не удалось открыть страницу справки в браузере",
} as const;

export interface HelpApi {
  /** Opens the official page of the key in the default browser. */
  openHelpPage(key: HelpPageKey): Promise<void>;
}

/** Builds the API over a call function (the Wails runtime in the app, a fake in tests). */
export function helpApi(call: CallByName): HelpApi {
  return {
    openHelpPage: async (key) => {
      await call(methods.openHelpPage, key);
    },
  };
}
