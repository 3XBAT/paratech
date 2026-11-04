// Environment access for the helpers. A problem of the machine is thrown with EnvironmentMarker, so the report says
// «не проверено» and not «сломано». Pure apart from the env argument.
import { EnvironmentMarker } from "../contract.js";

/** An Error whose message starts with EnvironmentMarker. */
export function envError(message: string): Error {
  return new Error(`${EnvironmentMarker} ${message}`);
}

/** The value of a variable the global setup exports; missing means the helper runs outside a prepared run. */
export function requireEnv(name: string, env: NodeJS.ProcessEnv = process.env): string {
  const v = env[name];
  if (!v) {
    throw envError(`нет переменной ${name}: тест запущен не через npm run e2e`);
  }
  return v;
}
