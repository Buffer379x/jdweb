// The queue of extraction jobs. JDownloader has no event for it, so it is asked every few seconds while the page is visible.

import { signal } from "@preact/signals";
import { get } from "../api/client.ts";
import type { QueueEntry } from "../lib/extraction.ts";
import { live } from "./app.ts";

export const queue = signal<QueueEntry[]>([]);
export const queueFailed = signal(false);
export const queueLoaded = signal(false);

export async function refreshQueue(): Promise<void> {
  try {
    const q = await get<QueueEntry[]>("/extraction/queue");
    queue.value = Array.isArray(q) ? q : [];
    queueFailed.value = false;
    queueLoaded.value = true;
  } catch {
    queueFailed.value = true;
  }
}

/** Polls while connected and visible; fast while the panel is open or something runs. Returns the stop function. */
export function startQueuePolling(isOpen: () => boolean): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false;
  const tick = async () => {
    if (stopped) return;
    if (live.peek() && !document.hidden) await refreshQueue();
    const busy = queue.peek().length > 0 || isOpen();
    timer = setTimeout(tick, busy ? 2000 : 6000);
  };
  void tick();
  return () => {
    stopped = true;
    clearTimeout(timer);
  };
}
