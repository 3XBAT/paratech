// Fallback popup for builds without the frontend: start or skip the current prompt (no snooze). The React popup in
// desktop/frontend replaces it. The Wails runtime is served by the shell itself at /wails/runtime.js.
import { Call, Events, Window } from "/wails/runtime.js";

const service = "paratech/desktop/internal/input/wails.PromptService.";
const el = (id) => document.getElementById(id);
let current = null;
let busy = false;

// The title is imported text: textContent only, never HTML.
function render(view, message) {
  current = view;
  const onOffer = view !== null && (view.state === "OFFERED" || view.state === "STARTING");
  const lesson = view !== null && view.kind === "lesson";
  el("heading").textContent = view === null ? "Запись" : lesson ? "Начинается занятие" : "Запись без занятия";
  const time = lesson ? [view.start, view.end].filter(Boolean).join("–") : "";
  el("details").textContent = lesson ? [view.title, time].filter(Boolean).join(", ") : "";
  el("message").textContent = message || (onOffer && view.problem ? "Не удалось начать запись." : "");
  el("start").textContent = view !== null && view.state === "STARTING" ? "Запуск…" : "Начать запись";
  el("start").hidden = el("skip").hidden = !onOffer;
  el("close").hidden = onOffer;
  el("start").disabled = el("skip").disabled = busy || (view !== null && view.state === "STARTING");
}

function load(message) {
  Call.ByName(service + "Current")
    .then((view) => render(view ?? null, message))
    .catch(() => render(null, "Не удалось загрузить предложение записи."));
}

function act(method) {
  if (current === null || busy) {
    return;
  }
  busy = true;
  render(current, "");
  // The fallback has no source picker: Start records the whole computer ("").
  const args = method === "Start" ? [current.id, ""] : [current.id];
  Call.ByName(service + method, ...args)
    .then((view) => {
      busy = false;
      render(view, "");
    })
    .catch(() => {
      busy = false;
      load("Не удалось выполнить действие.");
    });
}

el("start").addEventListener("click", () => act("Start"));
el("skip").addEventListener("click", () => act("Skip"));
el("close").addEventListener("click", () => Window.Hide());
Events.On("recordingprompt:shown", () => load(""));
load("");
