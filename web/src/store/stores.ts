import type { DlLink, DlPackage, LgLink, LgPackage } from "../api/types.ts";
import { percent } from "../lib/format.ts";
import { ListStore } from "./list.ts";
import { dlLinkFilterText, lgLinkStatus, lgPackageStatus, stateRank } from "./linkinfo.ts";

// What the filter reads: names, hosts and the state as a word. Progress, speed and ETA are not in it, so
// the patches that arrive every 500 ms for running links do not make the rows be worked out again.
const DL_FILTER = new Set(["name", "host", "hosts", "state", "skipReason", "finalState", "enabled"]);
const LG_FILTER = new Set(["name", "host", "hosts", "saveTo", "availability", "enabled", "onlineCount", "offlineCount", "unknownCount", "tempUnknownCount"]);

// What each sorted column reads.
const DL_SORT: Record<string, string[]> = {
  name: ["name"],
  size: ["bytesTotal"],
  host: ["host", "hosts"],
  status: ["state", "running", "finished"],
  progress: ["bytesLoaded", "bytesTotal", "finished"],
};
const LG_SORT: Record<string, string[]> = {
  name: ["name"],
  path: ["saveTo"],
  size: ["bytesTotal"],
  host: ["host", "hosts"],
  status: ["availability", "offlineCount", "unknownCount", "tempUnknownCount"],
};

export const downloads = new ListStore<DlPackage, DlLink>({
  name: "downloads",
  textOfPackage: (p) => `${p.name} ${p.hosts.join(" ")}`,
  textOfLink: (l) => `${l.name} ${l.host} ${dlLinkFilterText(l)}`,
  sortValuePackage: (p, c) => {
    switch (c) {
      case "size": return p.bytesTotal;
      case "host": return p.hosts[0] ?? "";
      case "status": return p.running ? 1 : p.finished ? 4 : 3;
      case "progress": return percent(p.bytesLoaded, p.bytesTotal, p.finished);
      default: return p.name;
    }
  },
  sortValueLink: (l, c) => {
    switch (c) {
      case "size": return l.bytesTotal;
      case "host": return l.host;
      case "status": return stateRank(l.state);
      case "progress": return percent(l.bytesLoaded, l.bytesTotal, l.finished);
      default: return l.name;
    }
  },
  filterFields: DL_FILTER,
  sortFields: (c) => new Set(DL_SORT[c] ?? ["name"]),
});

export const linkgrabber = new ListStore<LgPackage, LgLink>({
  name: "linkgrabber",
  textOfPackage: (p) => `${p.name} ${p.hosts.join(" ")} ${p.saveTo} ${lgPackageStatus(p).text}`,
  textOfLink: (l) => `${l.name} ${l.host} ${lgLinkStatus(l).text}`,
  sortValuePackage: (p, c) => {
    switch (c) {
      case "size": return p.bytesTotal;
      case "host": return p.hosts[0] ?? "";
      case "path": return p.saveTo;
      case "status": return p.offlineCount * 1000 + p.unknownCount;
      default: return p.name;
    }
  },
  sortValueLink: (l, c) => {
    switch (c) {
      case "size": return l.bytesTotal;
      case "host": return l.host;
      case "status": return l.availability;
      default: return l.name;
    }
  },
  filterFields: LG_FILTER,
  sortFields: (c) => new Set(LG_SORT[c] ?? ["name"]),
});

export const stores = { downloads, linkgrabber } as const;
