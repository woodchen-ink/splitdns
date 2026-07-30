"use client";

import {
  MutationCache,
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { ApiError, CODE_NEED_LOGIN } from "@/lib/api";
import { queryKeys } from "@/lib/queries";

// Providers 挂 TanStack Query 单例。
// 这是后台管理型工具, 关掉窗口聚焦重取, 避免切回标签页就把一堆平台 API 再打一遍。
export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(() => {
    // 缓存要在 QueryClient 之前建好, 而处理函数又需要这个 client, 所以用一个盒子先占位
    const box: { client?: QueryClient } = {};
    // 后端说未登录就以后端为准: 重新拉一次登录态, 闸门会把界面切回登录页。
    // 集中在这里而不是每个调用点各判一次 —— 漏掉一处, 那个页面就会一直对着 401 弹 toast
    const onError = (error: unknown) => {
      if (error instanceof ApiError && error.code === CODE_NEED_LOGIN) {
        void box.client?.invalidateQueries({ queryKey: queryKeys.session() });
      }
    };

    box.client = new QueryClient({
      queryCache: new QueryCache({ onError }),
      mutationCache: new MutationCache({ onError }),
      defaultOptions: {
        queries: {
          staleTime: 30_000,
          gcTime: 5 * 60_000,
          retry: 1,
          refetchOnWindowFocus: false,
        },
      },
    });
    return box.client;
  });

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
