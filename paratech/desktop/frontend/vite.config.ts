import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The build goes into the Go package that embeds it (internal/input/wails/assets.go explains the layout).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/input/wails/assets/app",
    emptyOutDir: true,
  },
});
