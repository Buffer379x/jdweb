import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { get } from "../api/client.ts";

export interface Loaded<T> {
  data: T | undefined;
  error: unknown;
  loading: boolean;
  reload: () => void;
  setData: (d: T) => void;
}

/** Loads something with GET on mount and when `path` changes. Keeps the old data while it reloads. */
export function useGet<T>(path: string | null, map?: (raw: never) => T): Loaded<T> {
  const [state, setState] = useState<{ data?: T; error?: unknown; loading: boolean }>({ loading: path !== null });
  const [n, setN] = useState(0);
  const mapRef = useRef(map);
  mapRef.current = map;
  useEffect(() => {
    if (path === null) return;
    const ac = new AbortController();
    setState((s) => ({ ...s, loading: true, error: undefined }));
    get<unknown>(path, ac.signal)
      .then((raw) => {
        if (ac.signal.aborted) return;
        const data = (mapRef.current ? mapRef.current(raw as never) : raw) as T;
        setState({ data, loading: false });
      })
      .catch((error) => {
        if (ac.signal.aborted || (error instanceof DOMException && error.name === "AbortError")) return;
        setState((s) => ({ data: s.data, error, loading: false }));
      });
    return () => ac.abort();
  }, [path, n]);
  const reload = useCallback(() => setN((x) => x + 1), []);
  const setData = useCallback((d: T) => setState((s) => ({ ...s, data: d })), []);
  return { data: state.data, error: state.error, loading: state.loading, reload, setData };
}
