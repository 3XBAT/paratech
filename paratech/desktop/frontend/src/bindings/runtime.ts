import type { CallByName } from "./settings";

// The Wails runtime is served by the app itself at /wails/runtime.js (no npm package). It is loaded at run time, so
// Vite leaves the import alone and the build does not need the file.
const runtimeUrl = "/wails/runtime.js";

interface WailsRuntime {
  Call: { ByName(method: string, ...args: unknown[]): Promise<unknown> };
}

let runtime: Promise<WailsRuntime> | undefined;

function load(): Promise<WailsRuntime> {
  runtime ??= import(/* @vite-ignore */ runtimeUrl) as Promise<WailsRuntime>;
  return runtime;
}

/** Calls a bound Go method through the Wails runtime. */
export const callByName: CallByName = async (method, ...args) => (await load()).Call.ByName(method, ...args);
