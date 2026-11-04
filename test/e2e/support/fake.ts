import { FAKE_CTL } from "./env.ts";

/** The control server of the fake JDownloader (internal/testjd/cmd/fakejd). */
export async function ctl(path: string): Promise<string> {
  const r = await fetch(`${FAKE_CTL}${path}`);
  if (!r.ok) throw new Error(`fake JDownloader ${path}: ${r.status}`);
  return (await r.text()).trim();
}
