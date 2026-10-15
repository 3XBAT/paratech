import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const page = (name: string) => fileURLToPath(new URL(name, import.meta.url));

// The build goes into the Go package that embeds it (internal/input/wails/assets.go explains the layout).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/input/wails/assets/app",
    // outDir must stay the dedicated app/ subfolder: emptyOutDir wipes it, and the committed fallback pages live next to it.
    emptyOutDir: true,
    // Two pages: the main window and the recording popup (a separate window, PRD section 6.1).
    rollupOptions: {
      input: { main: page("./index.html"), popup: page("./popup.html") },
    },
  },
});
