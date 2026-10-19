import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { initTheme } from "./ui/theme";
import "./ui/fonts.css";
import "./ui/tokens.css";
import "./ui/base.css";
import "./ui/components.css";
import "./sourcePicker/sourcePicker.css";
import "./styles.css";

// Before the first render: the CSP forbids an inline script in index.html.
initTheme();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
