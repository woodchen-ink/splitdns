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

// CFZone 是一个可选的 CF zone 以及它归哪份凭据管 —— 选完区就能顺带把账号定下来
export interface CFZone {
  zone: string;
  credentialId: number;
  credentialName: string;
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

// OriginDNS 是回源落点在 CF 上那条解析记录的巡检结论。
// message 为空表示这个回源压根不需要 CF 里有记录 (落点是 IP, 或者不归本账号管), 不必展示
export interface OriginDNS {
  originId: number;
  // zone 为空表示落点不归本账号的 CF 管
  zone: string;
  expect: string;
  actual: string;
  level: Level;
  message: string;
  canCreate: boolean;
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

// PlanProgress 是一条流程的完成度概览, 数据取自本地库里的步骤状态, 不含平台实时状态。
// done + skipped 才是"走完的步数": skipped 是终态, 不再验证也不阻塞收尾
export interface PlanProgress {
  planId: number;
  kind: PlanKind | string;
  status: string;
  total: number;
  done: number;
  skipped: number;
  // current 当前停在哪一步的标题; 全部走完时为空
  current: string;
}

// HostnameListItem 是列表页的形态: 域名本体 + 各条流程的完成度
export interface HostnameListItem extends Hostname {
  plans: PlanProgress[] | null;
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

// OriginRecordState 是落点主机名在 CF 上那条记录的实际状态。
// checked 为 false 表示这一轮没查过 (记录不在 SaaS 区里), 不能当成"没有"
export interface OriginRecordState {
  checked: boolean;
  found: boolean;
  type: string;
  content: string;
  proxied: boolean;
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
  customOriginRecord: OriginRecordState;
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

// AuthUser 是 CZL Connect 上的账号资料。令牌不会下发到前端, 这里也没有对应字段
export interface AuthUser {
  remoteId: number;
  username: string;
  nickname: string;
  email: string;
  avatar: string;
  scope: string;
  expiresAt: string;
  loggedInAt: string;
}

// SessionStatus: guest 没登录过, active 登录有效, expired 令牌被服务端判死需重新授权
export type SessionStatus = "guest" | "active" | "expired";

export interface Session {
  status: SessionStatus | string;
  user: AuthUser | null;
  // waiting 表示一次授权正在进行中 (等浏览器回跳, 或回跳已到手正在换令牌)
  waiting: boolean;
  // exchanging 表示回调已经到手, 正在换令牌
  exchanging: boolean;
  // loginError 上一次授权回跳失败的原因 —— 回跳发生在应用外面, 只能这样带回界面
  loginError: string;
  // tokenError 最近一次刷新令牌失败的原因; 属于暂时没刷上, 不影响继续使用
  tokenError: string;
}

// UpdateState 是自动更新所处的阶段, 取值由后端 service/update.go 定义
export type UpdateState =
  | "disabled"
  | "idle"
  | "checking"
  | "available"
  | "downloading"
  | "installing";

export interface UpdateRelease {
  tag: string;
  notes: string;
  url: string;
  publishedAt: string;
}

export interface UpdateStatus {
  state: UpdateState | string;
  currentVersion: string;
  // disabledReason 仅 disabled 时有值 (本地构建 / 包里没有更新公钥)
  disabledReason?: string;
  latest: UpdateRelease | null;
  // canAutoInstall 为 false 时只能去下载页手动更新 (macOS、老版本发布没有签名文件)
  canAutoInstall: boolean;
  received: number;
  total: number;
  checkedAt: string | null;
  error: string;
}
