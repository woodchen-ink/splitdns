// 与 server/model 的 JSON 契约一一对应。字段含义以后端注释为准, 这里只做类型约束。

export type Level = "ok" | "warn" | "error";

export interface Credential {
  id: number;
  kind: "cloudflare" | "dnspod" | string;
  name: string;
  hasSecret: boolean;
  apiToken?: string;
  secretId?: string;
  secretKey?: string;
}

export interface CredentialCheck {
  ok: boolean;
  message: string;
  scope: string[] | null;
}

export interface Origin {
  id: number;
  name: string;
  kind: string;
  value: string;
  address: string;
  sni: string;
  note: string;
}

export interface Route {
  id: number;
  hostnameId: number;
  line: string;
  // originId 为 0 表示不引用回源库, 用下面的内联字段直接填落点
  originId: number;
  kind: string;
  value: string;
  address: string;
  sni: string;
  origin: Origin | null;
}

export interface Hostname {
  id: number;
  hostname: string;
  parentZone: string;
  saasZone: string;
  dnspodDomain: string;
  cfCredentialId: number;
  saasCredentialId: number;
  dnspodCredentialId: number;
  enabled: boolean;
  note: string;
  routes: Route[];
}

export interface Finding {
  level: Level;
  code: string;
  title: string;
  detail: string;
  fix: string;
}

export interface TXTRequirement {
  name: string;
  value: string;
}

export interface DNSRecord {
  name: string;
  type: string;
  line: string;
  value: string;
  ttl: number;
  enabled: boolean;
}

export interface CustomHostnameState {
  exists: boolean;
  status: string;
  sslStatus: string;
  validationMethod: string;
  certExpiresAt: string;
  certAuthority: string;
  customOrigin: string;
  customOriginSni: string;
  ownershipTxt: TXTRequirement;
  dcvTxt: TXTRequirement[] | null;
  minTlsVersion: string;
}

export interface Snapshot {
  // fetchErrors 非空表示对应平台这轮没读到, 状态是未知而不是"空"
  fetchErrors: Record<string, string> | null;
  delegation: string[] | null;
  dnspodNameservers: string[] | null;
  dnspodMissing: boolean;
  shadowedRecords: string[] | null;
  parentLeftovers: string[] | null;
  fallbackOrigin: string;
  fallbackOriginStatus: string;
  customHostname: CustomHostnameState;
  records: DNSRecord[] | null;
}

export interface HostnameReport {
  hostnameId: number;
  hostname: string;
  snapshot: Snapshot;
  findings: Finding[] | null;
  level: Level;
  summary: string;
  checkedAt: string;
}

export interface Step {
  id: number;
  planId: number;
  seq: number;
  key: string;
  title: string;
  instruction: string;
  mode: "manual" | "auto" | "wait" | string;
  status: "pending" | "waiting" | "done" | "failed" | "skipped" | string;
  etaSeconds: number;
  startedAt: string | null;
  doneAt: string | null;
  lastCheckedAt: string | null;
  lastError: string;
  verifiable: boolean;
}

// setup 把域名配到位, teardown 反过来把各平台上的痕迹一处处撤掉
export type PlanKind = "setup" | "teardown";

export interface Plan {
  id: number;
  hostnameId: number;
  kind: PlanKind | string;
  status: string;
  steps: Step[];
}

export interface PlanView {
  plan: Plan;
  hostname: Hostname;
  report: HostnameReport;
}
