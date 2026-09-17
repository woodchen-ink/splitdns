"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleArrowUp, Download, ExternalLink, LoaderCircle, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { api, postWithMessage } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { UpdateStatus } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

// 状态只读后端内存, 不打 GitHub, 平时一分钟问一次足够; 下载 / 安装期间要看进度才加快
const IDLE_POLL = 60_000;
const BUSY_POLL = 1000;

// UpdateButton 是版本号 + 更新入口。顶栏和登录页各挂一个:
// 登录流程本身出 bug 时, 修复它的新版本得能在登录页上装。
//
// 检查由后端定时做 (启动后 10 秒一次, 之后每 6 小时), 这里只展示结论, 不自己触发检查。
export function UpdateButton({ className }: { className?: string }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);

  const { data } = useQuery({
    queryKey: queryKeys.update(),
    queryFn: () => api.get<UpdateStatus>("/api/update"),
    refetchInterval: (query) => {
      const state = query.state.data?.state;
      return state === "downloading" || state === "installing" || state === "checking"
        ? BUSY_POLL
        : IDLE_POLL;
    },
  });

  const check = useMutation({
    mutationFn: () => postWithMessage<UpdateStatus>("/api/update/check"),
    onSuccess: ({ data: status, msg }) => {
      qc.setQueryData(queryKeys.update(), status);
      toast.success(msg);
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const install = useMutation({
    mutationFn: () => postWithMessage<UpdateStatus>("/api/update/install"),
    onSuccess: ({ data: status }) => qc.setQueryData(queryKeys.update(), status),
    onError: (e: Error) => toast.error(e.message),
  });

  const openPage = useMutation({
    mutationFn: (url: string) => api.post("/api/open", { url }),
    onError: (e: Error) => toast.error(e.message),
  });

  if (!data) {
    return null;
  }

  const state = data.state;
  const busy = state === "downloading" || state === "installing";
  const hasUpdate = state === "available" || busy;
  const percent = data.total > 0 ? Math.min(100, Math.round((data.received / data.total) * 100)) : 0;

  let label = data.currentVersion;
  if (state === "available" && data.latest) {
    label = `新版本 ${data.latest.tag}`;
  } else if (state === "downloading") {
    label = `下载中 ${percent}%`;
  } else if (state === "installing") {
    label = "正在重启…";
  }

  return (
    <>
      <Button
        variant={hasUpdate ? "outline" : "ghost"}
        size="sm"
        className={cn(!hasUpdate && "text-muted-foreground font-normal", className)}
        onClick={() => setOpen(true)}
        title={state === "disabled" ? data.disabledReason : "软件更新"}
      >
        {busy ? (
          <LoaderCircle className="animate-spin" />
        ) : hasUpdate ? (
          <CircleArrowUp />
        ) : null}
        {label}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>软件更新</DialogTitle>
            <DialogDescription>
              当前版本 {data.currentVersion}
              {data.checkedAt && ` · 上次检查 ${new Date(data.checkedAt).toLocaleString()}`}
            </DialogDescription>
          </DialogHeader>

          {state === "disabled" && (
            <p className="text-muted-foreground">{data.disabledReason}</p>
          )}
          {state === "idle" && <p className="text-muted-foreground">已是最新版本。</p>}

          {hasUpdate && data.latest && (
            <div className="space-y-3">
              <p>
                <span className="font-medium">{data.latest.tag}</span>
                <span className="text-muted-foreground">
                  {" "}
                  · 发布于 {new Date(data.latest.publishedAt).toLocaleDateString()}
                </span>
              </p>
              {data.latest.notes && (
                <div className="bg-muted/50 max-h-64 overflow-y-auto rounded-md p-3 text-xs leading-relaxed whitespace-pre-wrap">
                  {data.latest.notes}
                </div>
              )}
              {state === "downloading" && (
                <div className="bg-muted h-1.5 overflow-hidden rounded-full">
                  <div className="bg-primary h-full transition-all" style={{ width: `${percent}%` }} />
                </div>
              )}
              {state === "installing" && (
                <p className="text-muted-foreground">校验通过, 正在安装, 程序马上会自动重启。</p>
              )}
              {!data.canAutoInstall && state === "available" && (
                <p className="text-muted-foreground">这个版本需要到下载页手动安装。</p>
              )}
            </div>
          )}

          {data.error && <p className="text-destructive">{data.error}</p>}

          <DialogFooter>
            {state !== "disabled" && (
              <Button
                variant="outline"
                onClick={() => check.mutate()}
                disabled={check.isPending || busy || state === "checking"}
              >
                <RefreshCw className={cn((check.isPending || state === "checking") && "animate-spin")} />
                检查更新
              </Button>
            )}
            {hasUpdate && data.latest && !data.canAutoInstall && (
              <Button onClick={() => openPage.mutate(data.latest!.url)}>
                <ExternalLink />
                打开下载页
              </Button>
            )}
            {hasUpdate && data.canAutoInstall && (
              <Button onClick={() => install.mutate()} disabled={install.isPending || busy}>
                {busy ? <LoaderCircle className="animate-spin" /> : <Download />}
                更新并重启
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
