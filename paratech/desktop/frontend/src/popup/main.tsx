import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { followTheme, initTheme } from "../ui/theme";
import { Popup } from "./Popup";
// Only the tokens and the font of the main window (the same font files); the rest is in popup.css, so the popup stays
// light (docs/design/brief.md section 8).
import "../ui/fonts.css";
import "../ui/tokens.css";
import "./popup.css";

// Before the first render, like the main window; then a change in Settings reaches an open popup.
initTheme();
followTheme();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Popup />
  </StrictMode>,
);
