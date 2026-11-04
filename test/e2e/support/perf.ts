import type { Page } from "@playwright/test";

/**
 * Types into an input the way the browser delivers it (one input event) and measures until the page shows the
 * result: the condition holds and two frames have been painted. Milliseconds.
 */
export async function filterLatency(page: Page, input: string, text: string, done: string): Promise<number> {
  return page.evaluate(
    async ({ input, text, done }) => {
      const el = document.querySelector<HTMLInputElement>(input)!;
      el.focus();
      const cond = new Function(`return (${done});`) as () => boolean;
      const t0 = performance.now();
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, text);
      el.dispatchEvent(new Event("input", { bubbles: true }));
      await new Promise<void>((res, rej) => {
        const start = performance.now();
        const tick = () => {
          if (cond()) return res();
          if (performance.now() - start > 5000) return rej(new Error("the filter result never showed"));
          requestAnimationFrame(tick);
        };
        requestAnimationFrame(tick);
      });
      await new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r())));
      return performance.now() - t0;
    },
    { input, text, done },
  );
}

/** Scrolls a list for `ms` milliseconds at a steady speed and reports frame times and long tasks. */
export async function scrollSmoothness(page: Page, selector: string, ms: number): Promise<{ frames: number; p95: number; worst: number; longTasks: number; scrolled: number }> {
  return page.evaluate(
    async ({ selector, ms }) => {
      const box = document.querySelector<HTMLElement>(selector)!;
      let longTasks = 0;
      const po = new PerformanceObserver((l) => (longTasks += l.getEntries().length));
      po.observe({ entryTypes: ["longtask"] });
      const top0 = box.scrollTop;
      const deltas: number[] = [];
      let last = performance.now();
      const start = last;
      await new Promise<void>((res) => {
        const step = (now: number) => {
          deltas.push(now - last);
          last = now;
          box.scrollTop += 40; // about 2400 px/s at 60 Hz: a fast flick
          if (now - start < ms) requestAnimationFrame(step);
          else res();
        };
        requestAnimationFrame(step);
      });
      po.disconnect();
      deltas.shift();
      const sorted = [...deltas].sort((a, b) => a - b);
      return { frames: deltas.length, p95: sorted[Math.floor(sorted.length * 0.95)] ?? 0, worst: sorted[sorted.length - 1] ?? 0, longTasks, scrolled: box.scrollTop - top0 };
    },
    { selector, ms },
  );
}
