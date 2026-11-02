import { useState } from "preact/hooks";

/** The icon of a hoster through the proxy of JDWeb. Loaded lazily; without it the name beside it still says everything. */
export function HosterIcon({ host }: { host: string }) {
  const [failed, setFailed] = useState(false);
  if (failed || !host) return <span class="host-icon none" aria-hidden="true" />;
  return (
    <img
      class="host-icon"
      src={`/api/v1/icons/hoster/${encodeURIComponent(host)}`}
      alt=""
      width={16}
      height={16}
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
    />
  );
}
