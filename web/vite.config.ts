import { defineConfig } from "vite";

export default defineConfig({
  server: {
    // The Go API serves on 8080; proxying /api keeps the frontend's fetch
    // calls same-origin in dev, so they need no base URL and no CORS dance.
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
  },
});
