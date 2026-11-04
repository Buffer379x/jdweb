// scripts/e2e.sh runs this before anything starts: node support/check-guard.ts
import { assertTestInstance } from "./guard.ts";

try {
  await assertTestInstance();
  console.log("[e2e] the guard accepts the JDownloader at the test stack");
} catch (e) {
  console.error((e as Error).message);
  process.exit(1);
}
