// query key 统一在这里定义, 组件内不写字面量, 失效时才不会漏。

export const queryKeys = {
  hostnames: (keyword: string) => ["config", "hostnames", keyword] as const,
  hostname: (id: number) => ["config", "hostname", id] as const,
  origins: () => ["config", "origins"] as const,
  credentials: () => ["config", "credentials"] as const,
  plan: (id: number) => ["plan", "detail", id] as const,
  report: (hostnameId: number) => ["plan", "report", hostnameId] as const,
};
