// query key 统一在这里定义, 组件内不写字面量, 失效时才不会漏。

export const queryKeys = {
  // 登录态独立成一支: 退出登录要清掉 config / discover / plan 下的全部缓存, 唯独它自己得留着
  session: () => ["auth", "session"] as const,
  hostnames: (keyword: string) => ["config", "hostnames", keyword] as const,
  // 前缀 key: 域名增删后让所有关键字下的列表一起失效
  hostnamesAll: () => ["config", "hostnames"] as const,
  hostname: (id: number) => ["config", "hostname", id] as const,
  origins: () => ["config", "origins"] as const,
  // 落在 origins 前缀下: 回源一改就跟着重新检测, 不用手动失效两把 key
  originDns: () => ["config", "origins", "dns"] as const,
  credentials: () => ["config", "credentials"] as const,
  // credentialId 传 0 表示不限定凭据, 后端聚合全部 CF 账号可见的 zone
  cfZones: (credentialId: number) => ["discover", "cf-zones", credentialId] as const,
  plan: (id: number) => ["plan", "detail", id] as const,
  report: (hostnameId: number) => ["plan", "report", hostnameId] as const,
};
