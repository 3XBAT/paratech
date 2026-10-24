import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { initAccent } from "./ui/accent";
import { initTheme } from "./ui/theme";
import "./ui/fonts.css";
import "./ui/tokens.css";
import "./ui/base.css";
import "./ui/components.css";
import "./ui/accent.css";
import "./sourcePicker/sourcePicker.css";
import "./ui/soundLevel.css";
import "./styles.css";
import "./agent/agent.css";
import "./agent/confirm.css";
import "./pages/today.css";
import "./pages/tickets.css";

// Before the first render: the CSP forbids an inline script in index.html.
initTheme();
initAccent();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
