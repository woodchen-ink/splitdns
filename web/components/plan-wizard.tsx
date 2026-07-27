"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError, CODE_NEED_CONFIRM, api, postWithMessage } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { PlanView, Step } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { ReportPanel } from "@/components/report-panel";
import { cn } from "@/lib/utils";

const STEP_STATUS: Record<string, { label: string; className: string }> = {
  done: { label: "已完成", className: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-400" },
  waiting: { label: "等待生效", className: "bg-amber-500/15 text-amber-700 dark:text-amber-400" },
  pending: { label: "待处理", className: "bg-muted text-muted-foreground" },
  failed: { label: "失败", className: "bg-red-500/12 text-red-700 dark:text-red-400" },
  skipped: { label: "已跳过", className: "bg-muted text-muted-foreground" },
};

const MODE_LABEL: Record<string, string> = {
  manual: "可自动执行, 也能自己去面板做",
  auto: "程序执行",
  wait: "只需等待",
};

// PlanWizard 是配置向导。每一步给出精确指令, 能自动做的直接调 API 做,
// 做完统一靠巡检验证 —— 平台接口返回成功不等于配置已经生效。
export function PlanWizard({ hostnameId }: { hostnameId: number }) {
  const qc = useQueryClient();
  const [planId, setPlanId] = useState<number | null>(null);

  // 进页面即建流程 (已有未完成的会被复用), 顺带巡检一次, 手动做过的步骤直接显示为完成
  const create = useMutation({
    mutationFn: () => api.post<PlanView>(`/api/hostnames/${hostnameId}/plan`),
    onSuccess: (view) => {
      setPlanId(view.plan.id);
      qc.setQueryData(queryKeys.plan(view.plan.id), view);
    },
    onError: (e: Error) => toast.error(e.message),
  });

  useEffect(() => {
    create.mutate();
    // 只在域名变化时重新建流程
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hostnameId]);

  const { data, isFetching, refetch } = useQuery({
    queryKey: queryKeys.plan(planId ?? 0),
    queryFn: () => api.get<PlanView>(`/api/plans/${planId}`),
    enabled: planId !== null,
  });

  if (!data) {
    return <Skeleton className="h-72 w-full rounded-xl" />;
  }

  const steps = data.plan.steps ?? [];
  const doneCount = steps.filter((s) => s.status === "done").length;

  return (
    <div className="space-y-5">
      <div className="border-border/60 flex flex-wrap items-center justify-between gap-3 rounded-xl border p-4">
        <div>
          <p className="text-sm font-medium">
            进度 {doneCount} / {steps.length}
            {data.plan.status === "done" && (
              <Badge variant="secondary" className="ml-2 border-0 bg-emerald-500/12 text-emerald-700 dark:text-emerald-400">
                全部完成
              </Badge>
            )}
          </p>
          <p className="text-muted-foreground mt-1 text-sm">
            每次刷新都会重新去三个平台核对一遍实际状态
          </p>
        </div>
        <Button variant="outline" onClick={() => refetch()} disabled={isFetching}>
          {isFetching ? "核对中…" : "重新核对"}
        </Button>
      </div>

      <ol className="space-y-3">
        {steps.map((step) => (
          <StepCard key={step.id} step={step} planId={data.plan.id} />
        ))}
      </ol>

      <Separator />
      <ReportPanel report={data.report} />
    </div>
  );
}

// StepCard 渲染单个步骤。指令用等宽字体原样展示 —— 里面是要照抄进面板的记录值,
// 排版一乱就容易抄错。
function StepCard({ step, planId }: { step: Step; planId: number }) {
  const qc = useQueryClient();
  const [pendingConfirm, setPendingConfirm] = useState<string | null>(null);
  const status = STEP_STATUS[step.status] ?? {
    label: step.status,
    className: "bg-muted text-muted-foreground",
  };

  const refreshPlan = (view: PlanView) => qc.setQueryData(queryKeys.plan(planId), view);

  // 记住待确认的是哪种做法, 确认时要原样再发一次
  const [pendingAction, setPendingAction] = useState("");
  const apply = useMutation({
    mutationFn: ({ confirm, action }: { confirm: boolean; action?: string }) =>
      postWithMessage<PlanView>(`/api/plans/${planId}/apply`, {
        stepId: step.id,
        confirm,
        action: action ?? "",
      }),
    onSuccess: ({ data, msg }) => {
      setPendingConfirm(null);
      setPendingAction("");
      refreshPlan(data);
      toast.success(msg || "已执行");
    },
    onError: (e: Error) => {
      // 破坏性操作要二次确认, 后端把待删清单放在错误信息里回来
      if (e instanceof ApiError && e.code === CODE_NEED_CONFIRM) {
        setPendingConfirm(e.message);
        return;
      }
      toast.error(e.message);
    },
  });

  const mark = useMutation({
    mutationFn: (action: "started" | "done") =>
      api.post<PlanView>(`/api/plans/${planId}/mark`, { stepId: step.id, action }),
    onSuccess: refreshPlan,
    onError: (e: Error) => toast.error(e.message),
  });

  const done = step.status === "done";

  return (
    <li
      className={cn(
        "border-border/60 rounded-xl border p-4",
        done && "bg-muted/30",
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="bg-muted text-muted-foreground flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-medium">
          {step.seq}
        </span>
        <span className="font-medium">{step.title}</span>
        <Badge variant="secondary" className={cn("border-0", status.className)}>
          {status.label}
        </Badge>
        {!step.verifiable && (
          <Badge variant="outline" className="font-normal">
            需人工确认
          </Badge>
        )}
      </div>

      {!done && step.instruction && (
        <pre className="bg-muted/60 text-foreground/90 mt-3 overflow-x-auto rounded-lg p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
          {step.instruction}
        </pre>
      )}

      {!done && step.lastError && (
        <p className="text-muted-foreground mt-2 text-sm">还没通过: {step.lastError}</p>
      )}

      {!done && (
        <p className="text-muted-foreground mt-2 text-xs">
          {MODE_LABEL[step.mode] ?? step.mode}
          {step.etaSeconds > 0 && ` · 操作后约 ${Math.round(step.etaSeconds / 60)} 分钟生效`}
          {step.startedAt && ` · 已等待 ${elapsed(step.startedAt)}`}
        </p>
      )}

      {pendingConfirm && (
        <div className="mt-3 rounded-lg border border-red-500/30 bg-red-500/5 p-3">
          <p className="text-sm font-medium text-red-700 dark:text-red-400">
            {pendingAction === "migrate"
              ? "确认后会先把记录搬到 DNSPod, 再从父区删除"
              : "这一步会删除记录, 确认后不可撤销"}
          </p>
          <pre className="mt-2 overflow-x-auto font-mono text-xs whitespace-pre-wrap">
            {pendingConfirm}
          </pre>
          <div className="mt-3 flex gap-2">
            <Button
              size="sm"
              variant="destructive"
              onClick={() => apply.mutate({ confirm: true, action: pendingAction })}
              disabled={apply.isPending}
            >
              {pendingAction === "migrate" ? "确认迁移并删除" : "确认删除"}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setPendingConfirm(null);
                setPendingAction("");
              }}
            >
              取消
            </Button>
          </div>
        </div>
      )}

      {!done && !pendingConfirm && (
        <div className="mt-3 flex flex-wrap gap-2">
          {/* 清理那一步的记录可能还在服务, 默认给"先搬走"这条更安全的路 */}
          {step.key === "cf.cleanup" && (
            <Button
              size="sm"
              onClick={() => {
                setPendingAction("migrate");
                apply.mutate({ confirm: false, action: "migrate" });
              }}
              disabled={apply.isPending}
            >
              {apply.isPending ? "处理中…" : "先迁移到 DNSPod 再删"}
            </Button>
          )}
          {step.mode !== "wait" && step.verifiable && (
            <Button
              size="sm"
              variant={step.key === "cf.cleanup" ? "outline" : "default"}
              onClick={() => {
                setPendingAction("");
                apply.mutate({ confirm: false });
              }}
              disabled={apply.isPending}
            >
              {apply.isPending ? "执行中…" : step.key === "cf.cleanup" ? "直接删除" : "自动执行"}
            </Button>
          )}
          {step.mode === "manual" && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => mark.mutate("started")}
              disabled={mark.isPending}
            >
              我自己做完了
            </Button>
          )}
          {!step.verifiable && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => mark.mutate("done")}
              disabled={mark.isPending}
            >
              确认完成
            </Button>
          )}
        </div>
      )}
    </li>
  );
}

// elapsed 把起始时刻转成"已等待多久"。只在客户端计算, 不参与首屏渲染, 避免水合不一致。
function elapsed(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime();
  if (ms < 60_000) return `${Math.max(0, Math.round(ms / 1000))} 秒`;
  if (ms < 3_600_000) return `${Math.round(ms / 60_000)} 分钟`;
  return `${Math.round(ms / 3_600_000)} 小时`;
}
