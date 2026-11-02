import { announcement } from "../store/announce.ts";

/** The one polite live region of the app. Rendered once, next to the toasts. */
export function Announcer() {
  return <div class="visually-hidden" role="status" aria-live="polite" aria-atomic="true">{announcement.value}</div>;
}
