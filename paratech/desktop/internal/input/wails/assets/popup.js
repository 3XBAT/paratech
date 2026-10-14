// The Wails runtime is served by the shell itself at /wails/runtime.js.
import { Window } from "/wails/runtime.js";

document.getElementById("dismiss").addEventListener("click", () => Window.Hide());
