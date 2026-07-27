"use client";

import { useParams, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Hostname } from "@/lib/types";
import { HostnameForm } from "@/components/hostname-form";
import { PlanWizard } from "@/components/plan-wizard";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

// HostnameDetail 同时承载新增与详情两种形态。
// 静态导出下这两个 URL 落在同一份模板上, 用路由参数区分, 不额外拆页面。
export function HostnameDetail() {
  const params = useParams<{ hostnameId: string }>();
  const router = useRouter();
  const raw = params?.hostnameId ?? "";
  const isNew = raw === "new";
  const id = Number(raw);
  const valid = !isNew && Number.isFinite(id) && id > 0;

  const { data, isLoading, isError, error } = useQuery({
    queryKey: queryKeys.hostname(id),
    queryFn: () => api.get<Hostname>(`/api/hostnames/${id}`),
    enabled: valid,
  });

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
