import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    include: ["src/**/*.test.{ts,tsx}"]
  },
  server: {
    port: 5173,
    host: "127.0.0.1",
    proxy: {
      "/api": {
        target: process.env.SUPERSCIENCE_SERVER ?? "http://127.0.0.1:4096",
        changeOrigin: true,
        ws: true
      }
    }
  }
});
