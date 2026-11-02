// A user agent string as "Browser on System", for the list of sessions. Only text, nothing is looked up.

export function describeAgent(ua: string): string {
  if (!ua) return "";
  const browsers: [RegExp, string][] = [
    [/Edg\//, "Edge"], [/OPR\/|Opera/, "Opera"], [/Firefox\//, "Firefox"], [/Chrome\/|CriOS\//, "Chrome"], [/Safari\//, "Safari"], [/curl\//, "curl"],
  ];
  const systems: [RegExp, string][] = [
    [/Windows/, "Windows"], [/Android/, "Android"], [/iPhone|iPad|iOS/, "iOS"], [/Mac OS X|Macintosh/, "macOS"], [/CrOS/, "ChromeOS"], [/Linux|X11/, "Linux"],
  ];
  const b = browsers.find(([re]) => re.test(ua))?.[1];
  const s = systems.find(([re]) => re.test(ua))?.[1];
  if (b && s) return `${b} / ${s}`;
  return b ?? s ?? (ua.length > 60 ? `${ua.slice(0, 60)}…` : ua);
}
