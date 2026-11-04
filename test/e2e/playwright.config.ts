import { defineConfig, devices } from "@playwright/test";

// Two targets, one project each. "real": the built image against a real JDownloader (the throwaway
// test stack). "fake": the same image against the fake JDownloader of internal/testjd, for what a
// real JDownloader cannot be made to do here (captchas, 10,000 links, 2,000 settings).
// scripts/e2e.sh starts everything and sets the addresses.
const real = process.env.E2E_URL ?? "http://127.0.0.1:17468";
const fake = process.env.E2E_FAKE_URL ?? "http://127.0.0.1:17469";

export default defineConfig({
  testDir: "tests",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 10_000 },
  reporter: [["list"]],
  outputDir: "test-results",
  use: {
    locale: "en-US",
    trace: "off",
    actionTimeout: 10_000,
    screenshot: "only-on-failure",
    ...devices["Desktop Chrome"],
    viewport: { width: 1280, height: 800 },
  },
  projects: [
    { name: "setup", testMatch: "real/00-setup.spec.ts", teardown: "real-teardown", use: { baseURL: real } },
    { name: "real", testMatch: "real/[1-9][0-8]*.spec.ts", dependencies: ["setup"], use: { baseURL: real, storageState: ".auth/real.json" } },
    { name: "real-teardown", testMatch: "real/99-teardown.spec.ts", use: { baseURL: real, storageState: ".auth/real.json" } },
    { name: "fake-setup", testMatch: "fake/00-setup.spec.ts", use: { baseURL: fake } },
    { name: "fake", testMatch: "fake/[1-9]*.spec.ts", dependencies: ["fake-setup"], use: { baseURL: fake, storageState: ".auth/fake.json" } },
  ],
});
