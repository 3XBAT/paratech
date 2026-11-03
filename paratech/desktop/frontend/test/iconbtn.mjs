// TASK-368: helpers for tests of icon buttons. An icon button has no visible words: it is found by its aria-label.
// (Not a test file: node --test runs test/*.test.mjs only.)

/** The opening tag of the first <button> whose aria-label is `label`; null when there is none. */
export function buttonTag(html, label) {
  const tags = html.match(/<button\b[^>]*>/g) ?? [];
  return tags.find((t) => t.includes(`aria-label="${label}"`)) ?? null;
}

/** Off: natively disabled, or aria-disabled (a disabled button with a reason stays focusable so the reason can be read). */
function off(tag) {
  return /\sdisabled=""/.test(tag) || /\saria-disabled="true"/.test(tag);
}

/** True when the button named `label` exists and is not disabled. */
export function isOn(html, label) {
  const tag = buttonTag(html, label);
  return tag !== null && !off(tag);
}

/** True when the button named `label` exists and is disabled (natively or aria-disabled). */
export function isOff(html, label) {
  const tag = buttonTag(html, label);
  return tag !== null && off(tag);
}

/** The aria-labels of all buttons of a markup, in order. */
export function ariaLabels(html) {
  return [...html.matchAll(/<button\b[^>]*\saria-label="([^"]*)"/g)].map((m) => m[1]);
}

/** The visible text of the markup: tags, the aria-hidden tooltips and the visually hidden descriptions removed. */
export function visibleText(html) {
  return html
    .replace(/<span class="ia-tip"[^>]*>[^<]*<\/span>/g, "")
    .replace(/<span (?:id="[^"]*" )?class="ia-vh">[^<]*<\/span>/g, "")
    .replace(/<[^>]+>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}
