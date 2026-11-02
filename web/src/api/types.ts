// Shapes of docs/api.md. IDs are strings of digits.

export type ConnectionState = "connected" | "syncing" | "offline" | "misconfigured";
export type Controller = "IDLE" | "RUNNING" | "PAUSE" | "STOPPING";
export type ListName = "downloads" | "linkgrabber";

export type LinkState = "disabled" | "extractError" | "finished" | "skipped" | "running" | "waiting";

export type Priority = "HIGHEST" | "HIGHER" | "HIGH" | "DEFAULT" | "LOW" | "LOWER" | "LOWEST";
export const PRIORITIES: readonly Priority[] = ["HIGHEST", "HIGHER", "HIGH", "DEFAULT", "LOW", "LOWER", "LOWEST"];

export interface Status {
  connection: ConnectionState;
  connectionDetail: string;
  controller: Controller;
  speed: number;
  downloads: { packages: number; links: number; bytesTotal: number; bytesLoaded: number; running: number; eta: number | null };
  linkgrabber: { packages: number; links: number; bytesTotal: number; crawling: boolean };
  captchas: number;
  dialogs: number;
  toggles: { limit: boolean; limitBytes: number; reconnect: boolean; premium: boolean; clipboard: boolean; stopAfter: boolean };
  jd: { revision: number; docker: boolean; url: string };
}

export interface Session {
  username: string;
  totpEnabled: boolean;
  version: string;
}

export interface DlPackage {
  id: string;
  name: string;
  hosts: string[];
  childCount: number;
  bytesTotal: number;
  bytesLoaded: number;
  speed: number;
  eta: number | null;
  enabled: boolean;
  finished: boolean;
  running: boolean;
  saveTo: string;
  status: string;
  priority: string;
  comment: string;
  downloadPassword: string;
}

export interface DlLink {
  id: string;
  packageId: string;
  name: string;
  host: string;
  url: string;
  bytesTotal: number;
  bytesLoaded: number;
  speed: number;
  eta: number | null;
  enabled: boolean;
  finished: boolean;
  running: boolean;
  skipped: boolean;
  skipReason: string;
  extractionStatus: string;
  status: string;
  priority: string;
  comment: string;
  downloadPassword: string;
  addedDate: number;
  finishedDate: number;
  state: LinkState;
}

export interface LgPackage {
  id: string;
  name: string;
  hosts: string[];
  childCount: number;
  bytesTotal: number;
  saveTo: string;
  onlineCount: number;
  offlineCount: number;
  unknownCount: number;
  tempUnknownCount: number;
  enabled: boolean;
  priority: string;
  comment: string;
  downloadPassword: string;
}

export type Availability = "ONLINE" | "OFFLINE" | "UNKNOWN" | "TEMP_UNKNOWN";

export interface LinkVariant {
  id: string;
  name: string;
  iconKey: string;
}

export interface LgLink {
  id: string;
  packageId: string;
  name: string;
  host: string;
  url: string;
  bytesTotal: number;
  availability: Availability;
  enabled: boolean;
  priority: string;
  variants: boolean;
  variant: LinkVariant | null;
  comment: string;
  downloadPassword: string;
}

/** The common part of both lists, what the generic list logic needs. */
export interface PackageLike {
  id: string;
  name: string;
  hosts: string[];
  childCount: number;
  bytesTotal: number;
  enabled: boolean;
  saveTo: string;
  priority: string;
  comment: string;
  downloadPassword: string;
}
export interface LinkLike {
  id: string;
  packageId: string;
  name: string;
  host: string;
  url: string;
  bytesTotal: number;
  enabled: boolean;
  priority: string;
  comment: string;
  downloadPassword: string;
}

export interface Patch {
  list: ListName;
  links: Record<string, Record<string, unknown>>;
  packages: Record<string, Record<string, unknown>>;
}

export interface Captcha {
  id: string;
  hoster: string;
  type: string;
  kind: string;
}

export interface FolderInfo {
  path: string;
  free: number;
  size: number;
}

export type CleanupAction =
  | "DELETE_ALL"
  | "DELETE_DISABLED"
  | "DELETE_FAILED"
  | "DELETE_FINISHED"
  | "DELETE_OFFLINE"
  | "DELETE_DUPE";
export type CleanupMode = "REMOVE_LINKS_ONLY" | "REMOVE_LINKS_AND_RECYCLE_FILES" | "REMOVE_LINKS_AND_DELETE_FILES";
export type CleanupSelection = "SELECTED" | "UNSELECTED" | "ALL" | "NONE";

/** UI preferences stored with PUT /prefs. Unknown members are kept. */
export interface Prefs {
  lang?: "de" | "en";
  theme?: "dark" | "light";
  expanded?: { downloads?: string[]; linkgrabber?: string[] };
  details?: boolean;
  [key: string]: unknown;
}
