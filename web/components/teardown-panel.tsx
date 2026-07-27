"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Hostname } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { PlanWizard } from "@/components/plan-wizard";

// TeardownPanel 是拆除入口: 上半截按步骤把各平台上的痕迹撤掉, 下半截才是删本地记录。
// 两件事分开是因为它们的后果完全不同 —— 本地记录删了只是这个工具里没有了,
// 平台上的解析、证书、域名不会跟着消失, 反过来也一样。
export function TeardownPanel({ hostname }: { hostname: Hostname }) {
  return (
    <div className="space-y-5">
      <div className="rounded-xl border border-amber-500/30 bg-amber-500/5 p-4">
        <p className="text-sm font-medium">按顺序把 {hostname.hostname} 的痕迹一处处撤掉</p>
        <p className="text-muted-foreground mt-1.5 text-sm">
          第一步撤掉委派之后这个域名就不再解析了, 后面几步只是清残留。每一步都会先列出待删清单,
          确认后才动手; 不想做的步骤可以跳过。
        </p>
      </div>

      <PlanWizard hostnameId={hostname.id} kind="teardown" />

      <Separator />
      <LocalRecordCard hostname={hostname} />
    </div>
  );
}

// LocalRecordCard 删掉本工具数据库里的这个域名。
// 不放进流程步骤里: 删完之后流程本身也没了, 再刷新只会报错。
function LocalRecordCard({ hostname }: { hostname: Hostname }) {
  const qc = useQueryClient();
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);

  const remove = useMutation({
    mutationFn: () => api.del(`/api/hostnames/${hostname.id}`),
    onSuccess: () => {
      toast.success("已删除本地记录");
      qc.removeQueries({ queryKey: queryKeys.hostname(hostname.id) });
      qc.invalidateQueries({ queryKey: queryKeys.hostnamesAll() });
      router.push("/");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <div className="border-border/60 rounded-xl border p-4">
      <p className="font-medium">删除本工具里的这个域名</p>
      <p className="text-muted-foreground mt-1.5 text-sm">
        只删本地数据 (域名配置、线路、流程记录), 不碰任何平台上的解析。平台上还没清干净的部分,
        删完之后就只能自己去各家面板处理了。
      </p>

      {confirming ? (
        <div className="mt-3 flex flex-wrap gap-2">
          <Button size="sm" variant="destructive" onClick={() => remove.mutate()} disabled={remove.isPending}>
            {remove.isPending ? "删除中…" : `确认删除 ${hostname.hostname}`}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
            取消
          </Button>
        </div>
      ) : (
        <Button size="sm" variant="outline" className="mt-3" onClick={() => setConfirming(true)}>
          删除本地记录
        </Button>
      )}
    </div>
  );
}
