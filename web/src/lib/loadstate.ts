// The state behind useGet, as plain functions. The data belongs to the path it was loaded from: a new path
// never shows the data of the old one (a page reused for another extension would show A's settings under
// B's title). A reload of the same path keeps its data while it runs, and when it fails the data stays with
// the error next to it, so the page can say the values may be out of date.

export interface Snap<T> {
  /** The path the data and the error belong to. */
  path: string | null;
  data?: T;
  error?: unknown;
  loading: boolean;
}

export function initial<T>(path: string | null): Snap<T> {
  return { path, loading: path !== null };
}

/** A load starts (a new path, or a reload of the same one). */
export function begin<T>(s: Snap<T>, path: string): Snap<T> {
  return s.path === path ? { ...s, loading: true, error: undefined } : { path, loading: true };
}

export function loaded<T>(_s: Snap<T>, path: string, data: T): Snap<T> {
  return { path, data, loading: false };
}

export function failed<T>(s: Snap<T>, path: string, error: unknown): Snap<T> {
  return s.path === path ? { path, data: s.data, error, loading: false } : { path, error, loading: false };
}

/** What a render may use for `path`: nothing from another path. */
export function view<T>(s: Snap<T>, path: string | null): Snap<T> {
  return s.path === path ? s : initial<T>(path);
}
