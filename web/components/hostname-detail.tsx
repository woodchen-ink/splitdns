"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Hostname } from "@/lib/types";
import { HostnameForm } from "@/components/hostname-form";
import { PlanWizard } from "@/components/plan-wizard";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

// HostnameDetail 同时承载新增与详情两种形态。
// 不能用 useParams 取 id: 静态导出下真实 ID 的请求由 Go 映射到 `_` 占位符模板,
// 而那份模板在构建时的参数字面量就是 "_", useParams 拿到的永远是它。只能从真实 URL 解析。
export function HostnameDetail() {
  const pathname = usePathname();
  const router = useRouter();

  // 构建期 pathname 与浏览器端不同, 挂载后再判定, 避免水合不一致
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const raw = pathname.split("/").filter(Boolean).at(-1) ?? "";
  const isNew = raw === "new";
  const id = Number(raw);
  const valid = !isNew && Number.isFinite(id) && id > 0;

  const { data, isLoading, isError, error } = useQuery({
    queryKey: queryKeys.hostname(id),
    queryFn: () => api.get<Hostname>(`/api/hostnames/${id}`),
    enabled: mounted && valid,
  });

  if (!mounted) {
    return <Skeleton className="h-64 w-full rounded-xl" />;
  }

  if (isNew) {
    return (
      <div className="space-y-5">
        <h1 className="text-xl font-semibold tracking-tight">新增访问域名</h1>
        <p className="text-muted-foreground text-sm">
          先把域名和线路配好保存, 保存后会生成一条配置流程, 一步步带你把它真正配到位。
        </p>
        <HostnameForm onSaved={(saved) => router.push(`/hostnames/${saved.id}/`)} />
      </div>
    );
  }

  if (!valid) {
    return <p className="text-muted-foreground text-sm">无效的域名地址</p>;
  }
  if (isLoading) {
    return <Skeleton className="h-64 w-full rounded-xl" />;
  }
  if (isError || !data) {
    return <p className="text-destructive text-sm">加载失败: {(error as Error)?.message}</p>;
  }

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">{data.hostname}</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          父区 {data.parentZone}
          {data.saasZone ? ` · SaaS 区 ${data.saasZone}` : ""} · DNSPod{" "}
          {data.dnspodDomain || data.hostname}
        </p>
      </div>

      <Tabs defaultValue="plan">
        <TabsList>
          <TabsTrigger value="plan">配置流程</TabsTrigger>
          <TabsTrigger value="config">域名配置</TabsTrigger>
        </TabsList>
        <TabsContent value="plan" className="mt-4">
          <PlanWizard hostnameId={data.id} />
        </TabsContent>
        <TabsContent value="config" className="mt-4">
          <HostnameForm initial={data} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
