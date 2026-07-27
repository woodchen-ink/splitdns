"use client";

import Link from "next/link";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Hostname } from "@/lib/types";
import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";

interface HostnameList {
  list: Hostname[] | null;
  total: number;
}

// 域名列表页。健康状态需要逐个调平台接口, 慢且吃配额, 所以列表只展示配置,
// 实际状态在详情页按需巡检, 不在这里批量拉。
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
            </div>
            <p className="text-muted-foreground mt-1.5 text-sm">
              父区 {h.parentZone} · {(h.routes ?? []).length} 条线路
              {h.note ? ` · ${h.note}` : ""}
            </p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {(h.routes ?? []).map((r) => (
                <Badge key={r.id} variant="outline" className="font-normal">
                  {r.line} → {r.origin?.name ?? "未绑定回源"}
                </Badge>
              ))}
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}
