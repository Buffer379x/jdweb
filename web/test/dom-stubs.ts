// What the stores need from a browser, as stubs, so that they run under node --test. Import this first.

const g = globalThis as unknown as Record<string, unknown>;
g.window = globalThis;
g.addEventListener ??= () => {};
g.removeEventListener ??= () => {};
g.location = { pathname: "/downloads" };
g.matchMedia = () => ({ matches: false, addEventListener() {} });

const listeners = new Map<string, Set<() => void>>();
export const fakeDocument = {
  hidden: false,
  documentElement: { lang: "", setAttribute() {}, removeAttribute() {} },
  addEventListener(type: string, fn: () => void) {
    if (!listeners.has(type)) listeners.set(type, new Set());
    listeners.get(type)!.add(fn);
  },
  removeEventListener(type: string, fn: () => void) {
    listeners.get(type)?.delete(fn);
  },
  fire(type: string) {
    for (const fn of [...(listeners.get(type) ?? [])]) fn();
  },
  listenerCount(type: string) {
    return listeners.get(type)?.size ?? 0;
  },
};
g.document = fakeDocument;

export class FakeEventSource {
  static all: FakeEventSource[] = [];
  url: string;
  closed = false;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private handlers = new Map<string, ((ev: { data: string }) => void)[]>();
  constructor(url: string) {
    this.url = url;
    FakeEventSource.all.push(this);
  }
  addEventListener(type: string, fn: (ev: { data: string }) => void) {
    this.handlers.set(type, [...(this.handlers.get(type) ?? []), fn]);
  }
  close() {
    this.closed = true;
  }
  // test helpers
  open() {
    this.onopen?.();
  }
  emit(type: string, data: unknown) {
    for (const fn of this.handlers.get(type) ?? []) fn({ data: JSON.stringify(data) });
  }
  fail() {
    this.onerror?.();
  }
}
g.EventSource = FakeEventSource;

/** Lets pending promises run. */
export async function flush(n = 10): Promise<void> {
  for (let i = 0; i < n; i++) await Promise.resolve();
}

export type Route = (url: string, init?: RequestInit) => Response | Promise<Response> | undefined;

/** Replaces fetch. `routes` answers by path; the log keeps every call. */
export function stubFetch(routes: Record<string, () => Response | Promise<Response>>): string[] {
  const log: string[] = [];
  globalThis.fetch = (async (url: string, init?: RequestInit) => {
    const path = String(url).replace("/api/v1", "");
    log.push(`${init?.method ?? "GET"} ${path}`);
    const r = routes[path];
    return r ? r() : new Response("{}", { status: 200 });
  }) as typeof fetch;
  return log;
}

export const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status });
