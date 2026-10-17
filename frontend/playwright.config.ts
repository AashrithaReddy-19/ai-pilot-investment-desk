import { defineConfig, devices } from '@playwright/test'

/**
 * Browser end-to-end checks for the operating screens. The suite needs the full stack:
 * Postgres + Redis + the rules engine (FastAPI :8000) + the Go gateway (:8080).
 * BASE_URL points the specs at the dev server (or a preview of the build); it defaults
 * to the Vite dev port so `npm run dev` + `npx playwright test` is a valid local flow.
 * The specs are read-only apart from one observation they record and delete via the API.
 */
export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  use: {
    baseURL: process.env.BASE_URL ?? 'http://localhost:5173',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
