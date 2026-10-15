import type { CallByName } from "./settings";

// The Wails runtime is served by the app itself at /wails/runtime.js (no npm package). It is loaded at run time, so
// Vite leaves the import alone and the build does not need the file.
const runtimeUrl = "/wails/runtime.js";

interface WailsRuntime {
  Call: { ByName(method: string, ...args: unknown[]): Promise<unknown> };
  Events: { On(name: string, callback: (event: unknown) => void): () => void };
  Window: { Hide(): Promise<void> };
}

let runtime: Promise<WailsRuntime> | undefined;

function load(): Promise<WailsRuntime> {
  runtime ??= import(/* @vite-ignore */ runtimeUrl) as Promise<WailsRuntime>;
  return runtime;
}

/** Calls a bound Go method through the Wails runtime. */
export const callByName: CallByName = async (method, ...args) => (await load()).Call.ByName(method, ...args);

/** Listens to a Wails event emitted by the Go core; resolves to the function that stops listening. */
export async function onEvent(name: string, handler: () => void): Promise<() => void> {
  return (await load()).Events.On(name, () => handler());
}

/** Hides the window the page runs in. */
export async function hideWindow(): Promise<void> {
  await (await load()).Window.Hide();
}
