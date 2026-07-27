"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

// Providers 挂 TanStack Query 单例。
// 这是后台管理型工具, 关掉窗口聚焦重取, 避免切回标签页就把一堆平台 API 再打一遍。
export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            gcTime: 5 * 60_000,
            retry: 1,
            refetchOnWindowFocus: false,
          },
        },
      }),
  );

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
