import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { get } from "../api/client.ts";
import { begin, failed, initial, loaded, view, type Snap } from "../lib/loadstate.ts";

export interface Loaded<T> {
  data: T | undefined;
  /** With data: the last reload failed and the data may be out of date. */
  error: unknown;
  loading: boolean;
  reload: () => void;
  setData: (d: T) => void;
}

/**
 * Loads something with GET on mount and when `path` changes. A reload keeps its data while it runs; a new
 * path never shows the data of the old one (lib/loadstate.ts).
 */
export function useGet<T>(path: string | null, map?: (raw: never) => T): Loaded<T> {
  const [snap, setSnap] = useState<Snap<T>>(() => initial<T>(path));
  const [n, setN] = useState(0);
  const mapRef = useRef(map);
  mapRef.current = map;
  useEffect(() => {
    if (path === null) return;
    const ac = new AbortController();
    setSnap((s) => begin(s, path));
    get<unknown>(path, ac.signal)
      .then((raw) => {
        if (ac.signal.aborted) return;
        setSnap((s) => loaded(s, path, (mapRef.current ? mapRef.current(raw as never) : raw) as T));
      })
      .catch((error) => {
        if (ac.signal.aborted || (error instanceof DOMException && error.name === "AbortError")) return;
        setSnap((s) => failed(s, path, error));
      });
    return () => ac.abort();
  }, [path, n]);
  const reload = useCallback(() => setN((x) => x + 1), []);
  const setData = useCallback((d: T) => setSnap((s) => (path === null ? s : { ...s, path, data: d })), [path]);
  const v = view(snap, path);
  return { data: v.data, error: v.error, loading: v.loading || (path !== null && snap.path !== path), reload, setData };
}
