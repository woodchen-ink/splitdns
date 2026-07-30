"use client";

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Session } from "@/lib/types";
import { LoginScreen } from "@/components/login-screen";
import { Button } from "@/components/ui/button";

// AuthGate 是全站闸门: 拿到登录态之前不渲染任何业务界面。
//
// 后端那边同样拦着 /api (router/auth.go), 这里挡的是"人看得见的部分" ——
// 两层都要有: 只有前端拦, 开一下 devtools 就绕过去了; 只有后端拦, 界面会变成一片报错。
export function AuthGate({ children }: { children: ReactNode }) {
  const { data, isPending, error, refetch } = useQuery({
    queryKey: queryKeys.session(),
    queryFn: () => api.get<Session>("/api/auth/session"),
    // 授权在系统浏览器里完成, 回跳进的是 Go 那一侧, 前端没有任何回调可接 —— 只能轮询。
    //
    // **判据是"还没登录"而不是"正在等回跳"**: 后者要求后端的流程状态一刻不差地跟着变,
    // 中间但凡有一瞬间显示成"没在等待", 轮询就停了, 之后回调成功也没人来看一眼 (踩过)。
    // 登录进去就完全停掉; 没登录时这个请求只读本地一行, 一秒一次也不心疼
    refetchInterval: (query) => {
      const session = query.state.data;
      if (!session || session.status === "active") {
        return false;
      }
      // 回跳前后那几秒用户正盯着看, 拖两拍就像卡住了; 单纯停在登录页则不必这么勤
      return session.waiting ? 1000 : 3000;
    },
    // 桌面壳收到回调会把窗口调到前台, 焦点回来时顺手再确认一次
    refetchOnWindowFocus: true,
    staleTime: 0,
    retry: false,
  });

  if (isPending) {
    return (
      <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
        正在读取登录状态…
      </div>
    );
  }

  // 接口本身都调不通: 桌面版里这意味着后端没起来, 和"没登录"是两码事, 不能引到登录页去
  if (error || !data) {
    return (
      <div className="flex h-full items-center justify-center px-6">
        <div className="max-w-md space-y-3 text-center">
          <p className="text-sm font-medium">读取登录状态失败</p>
          <p className="text-muted-foreground text-sm">{error?.message ?? "接口没有返回数据"}</p>
          <Button variant="outline" size="sm" onClick={() => void refetch()}>
            重试
          </Button>
        </div>
      </div>
    );
  }

  if (data.status !== "active") {
    return <LoginScreen session={data} />;
  }
  return <>{children}</>;
}
