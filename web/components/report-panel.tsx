"use client";

import type { HostnameReport } from "@/lib/types";
import { LevelBadge } from "@/components/level-badge";
import { Badge } from "@/components/ui/badge";

// ReportPanel 展示巡检结果。检查项的 code 由后端定义且会增长,
// 这里一律走通用渲染, 不为每个 code 写专属分支。
export function ReportPanel({ report }: { report: HostnameReport }) {
  const findings = report.findings ?? [];
  const snap = report.snapshot;

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-base font-semibold">巡检结果</h2>
        <LevelBadge level={report.level} />
        <span className="text-muted-foreground text-sm">{report.summary}</span>
      </div>

      {findings.length === 0 && (
        <p className="text-muted-foreground text-sm">没有发现问题。</p>
      )}

      <ul className="space-y-2">
        {findings.map((f, i) => (
          <li key={`${f.code}-${i}`} className="border-border/60 rounded-lg border p-3">
            <div className="flex flex-wrap items-center gap-2">
              <LevelBadge level={f.level} />
              <span className="text-sm font-medium">{f.title}</span>
              <code className="text-muted-foreground text-xs">{f.code}</code>
            </div>
            {f.detail && (
              <p className="text-muted-foreground mt-1.5 font-mono text-xs break-all">{f.detail}</p>
            )}
            {f.fix && <p className="mt-1.5 text-sm">→ {f.fix}</p>}
          </li>
        ))}
      </ul>

      <div className="border-border/60 space-y-2 rounded-xl border p-4 text-sm">
        <h3 className="font-medium">实际状态</h3>
        <Row label="父区委派 NS" value={(snap.delegation ?? []).join(", ")} />
        <Row label="DNSPod 分配 NS" value={(snap.dnspodNameservers ?? []).join(", ")} />
        <Row
          label="回退源"
          value={snap.fallbackOrigin ? `${snap.fallbackOrigin} (${snap.fallbackOriginStatus})` : ""}
        />
        {snap.customHostname.exists && (
          <>
            <Row
              label="自定义主机名"
              value={`主机名 ${snap.customHostname.status} · 证书 ${snap.customHostname.sslStatus} · ${snap.customHostname.certAuthority || "未知 CA"}`}
            />
            {snap.customHostname.customOrigin && (
              <Row
                label="自定义源"
                value={`${snap.customHostname.customOrigin} (SNI ${snap.customHostname.customOriginSni || "同名"})`}
              />
            )}
          </>
        )}
      </div>

      {(snap.records ?? []).length > 0 && (
        <div className="border-border/60 rounded-xl border p-4">
          <h3 className="mb-2 text-sm font-medium">DNSPod 解析记录</h3>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-muted-foreground text-left text-xs">
                <tr>
                  <th className="py-1 pr-4 font-medium">主机记录</th>
                  <th className="py-1 pr-4 font-medium">类型</th>
                  <th className="py-1 pr-4 font-medium">线路</th>
                  <th className="py-1 pr-4 font-medium">值</th>
                  <th className="py-1 font-medium">TTL</th>
                </tr>
              </thead>
              <tbody className="font-mono text-xs">
                {(snap.records ?? []).map((r, i) => (
                  <tr key={i} className="border-border/40 border-t">
                    <td className="py-1.5 pr-4">{r.name}</td>
                    <td className="py-1.5 pr-4">{r.type}</td>
                    <td className="py-1.5 pr-4">{r.line}</td>
                    <td className="py-1.5 pr-4 break-all">
                      {r.value}
                      {!r.enabled && (
                        <Badge variant="outline" className="ml-2 font-sans font-normal">
                          已停用
                        </Badge>
                      )}
                    </td>
                    <td className="py-1.5">{r.ttl}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-wrap gap-x-3">
      <span className="text-muted-foreground w-32 shrink-0">{label}</span>
      <span className="font-mono text-xs break-all">{value || "—"}</span>
    </div>
  );
}
