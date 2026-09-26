import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    // The Go binary embeds this directory, so `go run .` serves the built
    // application and the API from one process. That is what keeps the console
    // clonable and runnable in two commands.
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    // In development Vite serves the UI and proxies the API to the Go process,
    // so the front end hot-reloads without a Go rebuild.
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
