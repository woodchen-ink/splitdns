"use client";

import Link from "next/link";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { HostnameListItem, PlanProgress, Route } from "@/lib/types";
import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// 落点有两种写法: 引用回源库, 或直接内联填值。列表要把两种都显示出来,
// 只认前者会让内联落点显示成"未绑定"。
function routeTarget(r: Route): string {
  if (r.origin?.name) {
    return r.origin.name;
  }
  return r.value || "未设置落点";
}

// 流程类型是开放式取值, 没登记的原样展示成它自己, 不静默丢掉整条进度
const PLAN_LABEL: Record<string, string> = { setup: "配置", teardown: "拆除" };

// PlanChip 用一小条进度表示流程走到哪了, 挂在卡片标题行右侧, 不单占一行。
// 只反映"步骤走完了几步", 不代表线上真的生效 —— 那是详情页巡检的事, 这里的数据全来自本地库。
// 停在哪一步、跳过了几步写进 title, 需要时悬停就能看到, 不占版面。
function PlanChip({ plan }: { plan: PlanProgress }) {
  const walked = plan.done + plan.skipped;
  const pct = plan.total > 0 ? Math.round((walked / plan.total) * 100) : 0;
  const finished = plan.total > 0 && walked >= plan.total;

  const hint = [
    finished ? "已走完全部步骤" : plan.current && `当前: ${plan.current}`,
    plan.skipped > 0 && `已跳过 ${plan.skipped} 步`,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <span className="flex items-center gap-1.5 text-xs" title={hint}>
      <span className="text-muted-foreground">{PLAN_LABEL[plan.kind] ?? plan.kind}</span>
      <span className="bg-muted h-1 w-14 overflow-hidden rounded-full">
        <span
          className={cn("block h-full rounded-full", finished ? "bg-emerald-500" : "bg-primary")}
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className="text-muted-foreground tabular-nums">
        {walked}/{plan.total}
      </span>
    </span>
  );
}

interface HostnameList {
  list: HostnameListItem[] | null;
  total: number;
}

// 域名列表页。展示配置本身 + 流程走到第几步 (后者是本地库里的步骤状态, 后端算好直接给)。
// 健康状态要逐个调平台接口, 慢且吃配额, 所以实际状态在详情页按需巡检, 不在这里批量拉。
export default function HostnamesPage() {
  const [keyword, setKeyword] = useState("");
  const { data, isLoading, isError, error } = useQuery({
    queryKey: queryKeys.hostnames(keyword),
    queryFn: () => api.get<HostnameList>(`/api/hostnames?keyword=${encodeURIComponent(keyword)}`),
  });

  const list = data?.list ?? [];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">访问域名</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            委派到 DNSPod 做分线路解析的域名, 共 {data?.total ?? 0} 个
          </p>
        </div>
        <Link href="/hostnames/new/" className={buttonVariants()}>
          新增域名
        </Link>
      </div>

      <Input
        value={keyword}
        onChange={(e) => setKeyword(e.target.value)}
        placeholder="搜索域名、父区或备注"
        className="max-w-sm"
      />

      {isLoading && (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-24 w-full rounded-xl" />
          ))}
        </div>
      )}

      {isError && <p className="text-destructive text-sm">加载失败: {(error as Error).message}</p>}

      {!isLoading && !isError && list.length === 0 && (
        <div className="border-border/60 rounded-xl border border-dashed p-10 text-center">
          <p className="text-muted-foreground text-sm">
            还没有域名。先去「凭据」配好 Cloudflare 和 DNSPod 的密钥, 再回来新增。
          </p>
        </div>
      )}

      <div className="space-y-2">
        {list.map((h) => (
          <Link
            key={h.id}
            href={`/hostnames/${h.id}/`}
            className="border-border/60 hover:bg-accent/40 block rounded-xl border p-4 transition-colors"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{h.hostname}</span>
              {!h.enabled && <Badge variant="outline">已停用</Badge>}
              {h.saasZone && <Badge variant="secondary">SaaS {h.saasZone}</Badge>}
              <span className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-1">
                {(h.plans ?? []).length === 0 ? (
                  <span className="text-muted-foreground text-xs">还没开始配置流程</span>
                ) : (
                  (h.plans ?? []).map((p) => <PlanChip key={p.planId} plan={p} />)
                )}
              </span>
            </div>
            <p className="text-muted-foreground mt-1.5 text-sm">
              {h.parentZone ? `父区 ${h.parentZone}` : "DNSPod 直托"} · {(h.routes ?? []).length}{" "}
              条线路
              {h.note ? ` · ${h.note}` : ""}
            </p>
            <div className="mt-2.5 flex flex-wrap gap-1.5">
              {(h.routes ?? []).map((r) => (
                <Badge key={r.id} variant="outline" className="font-normal">
                  {r.line} → {routeTarget(r)}
                </Badge>
              ))}
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}
