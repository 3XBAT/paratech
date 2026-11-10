import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { installScrollActivity } from "../ui/scrollActivity";
import { followTheme, initTheme } from "../ui/theme";
import { Popup } from "./Popup";
// Only the tokens, the font, the source picker and the shared icon button of the main window (the same files); the rest
// is in popup.css, so the popup stays light (docs/design/brief.md section 8).
import "../ui/fonts.css";
import "../ui/tokens.css";
import "../ui/scrollbar.css";
import "../ui/iconAction.css";
import "../sourcePicker/sourcePicker.css";
import "../ui/soundLevel.css";
import "./popup.css";

// Before the first render, like the main window; then a change in Settings reaches an open popup.
initTheme();
followTheme();
installScrollActivity(document);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Popup />
  </StrictMode>,
);
