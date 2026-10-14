import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The build goes into the Go package that embeds it (internal/input/wails/assets.go explains the layout).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/input/wails/assets/app",
    // outDir must stay the dedicated app/ subfolder: emptyOutDir wipes it, and the committed popup lives next to it.
    emptyOutDir: true,
  },
});
