import type { DlLink, DlPackage, LgLink, LgPackage } from "../api/types.ts";
import { percent } from "../lib/format.ts";
import { ListStore } from "./list.ts";
import { dlLinkStatus, lgLinkStatus, lgPackageStatus, stateRank } from "./linkinfo.ts";

const DL_VIEW = new Set(["name", "host", "hosts", "state", "status", "bytesTotal", "bytesLoaded", "finished", "running", "enabled", "extractionStatus", "skipped"]);
const LG_VIEW = new Set(["name", "host", "hosts", "bytesTotal", "availability", "enabled", "onlineCount", "offlineCount", "unknownCount", "tempUnknownCount", "saveTo"]);

export const downloads = new ListStore<DlPackage, DlLink>({
  name: "downloads",
  textOfPackage: (p) => `${p.name} ${p.hosts.join(" ")} ${p.status}`,
  textOfLink: (l) => `${l.name} ${l.host} ${dlLinkStatus(l, true).text} ${l.status}`,
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
  viewFields: DL_VIEW,
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
  viewFields: LG_VIEW,
});

export const stores = { downloads, linkgrabber } as const;
