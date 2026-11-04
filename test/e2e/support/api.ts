import { expect, type APIRequestContext, type Page } from "@playwright/test";
import { FIXTURE, PREFIX } from "./env.ts";

export interface Pkg { id: string; name: string }
export interface Link { id: string; packageId: string; name: string; state?: string; availability?: string }
export interface Lists { packages: Pkg[]; links: Link[] }

/** The JDWeb API as the signed-in test browser sees it. Used to set up and to look, never instead of the UI test. */
export class Api {
  constructor(private readonly r: APIRequestContext, private readonly base: string) {}

  private headers() {
    return { "X-JDWeb-Request": "1", Origin: this.base, "Content-Type": "application/json" };
  }

  async get<T = any>(path: string): Promise<T> {
    const res = await this.r.get(`/api/v1${path}`);
    expect(res.status(), `GET ${path}`).toBe(200);
    return (await res.json()) as T;
  }

  async post<T = any>(path: string, body: unknown = {}, want = [200, 204]): Promise<T | null> {
    const res = await this.r.post(`/api/v1${path}`, { data: body, headers: this.headers() });
    expect(want, `POST ${path}: ${res.status()} ${(await res.text()).slice(0, 200)}`).toContain(res.status());
    return res.status() === 204 ? null : ((await res.json()) as T);
  }

  async put(path: string, body: unknown, want = 204): Promise<void> {
    const res = await this.r.put(`/api/v1${path}`, { data: body, headers: this.headers() });
    expect(res.status(), `PUT ${path}: ${(await res.text()).slice(0, 200)}`).toBe(want);
  }

  downloads(): Promise<Lists> { return this.get("/downloads"); }
  grabber(): Promise<Lists> { return this.get("/linkgrabber"); }
  status(): Promise<any> { return this.get("/status"); }

  /** Adds links as a package of its own. Not started unless asked. */
  async add(pkg: string, urls: string[], opts: { autostart?: boolean; dir?: string; raw?: boolean } = {}): Promise<void> {
    const body: Record<string, unknown> = { links: urls, packageName: opts.raw ? pkg : PREFIX + pkg, autostart: !!opts.autostart };
    if (opts.dir) body.destinationFolder = opts.dir;
    await this.post("/linkgrabber/add", body);
  }

  /**
   * Add, wait until JDownloader has them with a known availability, move to the downloads. Returns the name
   * JDownloader gave the package (it replaces some characters), found by what is new, not by the name asked for.
   */
  async stage(pkg: string, urls: string[], opts: { dir?: string; raw?: boolean } = {}): Promise<string> {
    const known = new Set((await this.grabber()).packages.map((p) => p.id));
    await this.add(pkg, urls, opts);
    let id = "";
    let name = "";
    await expect.poll(async () => {
      const g = await this.grabber();
      const p = g.packages.find((x) => !known.has(x.id));
      const links = g.links.filter((l) => l.packageId === p?.id);
      id = p?.id ?? "";
      name = p?.name ?? "";
      return links.length === urls.length && links.every((l) => l.availability !== "UNKNOWN" && l.availability !== "TEMP_UNKNOWN");
    }, { timeout: 60_000, message: `package ${pkg} in the linkgrabber` }).toBe(true);
    await this.post("/linkgrabber/actions/to-downloads", { packageIds: [id], linkIds: [], start: false });
    await expect.poll(async () => (await this.downloads()).packages.some((p) => p.name === name), { timeout: 30_000, message: `package ${name} in the downloads` }).toBe(true);
    return name;
  }

  async waitGrabber(count: number): Promise<Lists> {
    let l: Lists = { packages: [], links: [] };
    await expect.poll(async () => (l = await this.grabber()).links.length, { timeout: 60_000, message: "links in the linkgrabber" }).toBe(count);
    return l;
  }

  /** Moves everything of the linkgrabber to the downloads, without starting. */
  async toDownloads(): Promise<void> {
    const g = await this.grabber();
    await this.post("/linkgrabber/actions/to-downloads", { packageIds: g.packages.map((p) => p.id), linkIds: [], start: false });
  }

  /** The speed limit of JDownloader, so that a download lasts long enough to be watched. */
  async limit(bytesPerSecond: number | null): Promise<void> {
    await this.post("/speedlimit", { enabled: bytesPerSecond !== null, bytesPerSecond: bytesPerSecond ?? 51200 });
  }

  /**
   * Stops and removes everything, files included. Only after the guard passed (00-setup writes .auth/guard-passed,
   * 99-teardown checks it and the identity of the instance again): what is in JDownloader now was put there by
   * these tests, also what the UI named itself.
   */
  async wipe(): Promise<void> {
    await this.limit(null);
    // A failed stop is not hidden: the poll for IDLE below would only show it as a timeout much later.
    await this.post("/controller/stop", {}, [200, 204]);
    await this.post("/linkgrabber/actions/abort", {});
    const cleanup = { action: "DELETE_ALL", mode: "REMOVE_LINKS_AND_DELETE_FILES", selection: "ALL", linkIds: [], packageIds: [] };
    await this.post("/downloads/actions/cleanup", cleanup);
    await this.post("/linkgrabber/actions/cleanup", { ...cleanup, mode: "REMOVE_LINKS_ONLY" });
    await expect.poll(async () => (await this.downloads()).links.length + (await this.grabber()).links.length, { timeout: 30_000 }).toBe(0);
    await expect.poll(async () => (await this.status()).controller, { timeout: 30_000 }).toBe("IDLE");
  }
}

export const fx = (path: string) => FIXTURE + path;

/** Waits until the app shows the connected state. */
export async function connected(page: Page): Promise<void> {
  await expect(page.getByText("JDownloader connected")).toBeVisible({ timeout: 60_000 });
}

/** The row of an entry by its name. */
export const row = (page: Page, name: string | RegExp) => page.getByRole("row").filter({ has: page.locator(".name", { hasText: name }) });
