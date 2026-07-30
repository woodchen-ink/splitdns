"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LogOut, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { api, postWithMessage } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Session } from "@/lib/types";
import { Button } from "@/components/ui/button";

// GUEST_SESSION 是退出后就地写进缓存的未登录态, 形状与后端返回的一致
const GUEST_SESSION: Session = {
  status: "guest",
  user: null,
  waiting: false,
  exchanging: false,
  loginError: "",
  tokenError: "",
};

// UserMenu 是顶部导航右侧的账号区。
// 只读缓存里的登录态, 不自己发请求 —— AuthGate 已经拉过一次, 再拉一次纯属重复。
export function UserMenu() {
  const qc = useQueryClient();
  const { data } = useQuery({
    queryKey: queryKeys.session(),
    queryFn: () => api.get<Session>("/api/auth/session"),
    staleTime: 30_000,
  });

  const refresh = useMutation({
    mutationFn: () => postWithMessage<Session>("/api/auth/refresh"),
    onSuccess: ({ msg }) => {
      toast.success(msg);
      void qc.invalidateQueries({ queryKey: queryKeys.session() });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const logout = useMutation({
    mutationFn: () => postWithMessage("/api/auth/logout"),
    onSuccess: ({ msg }) => {
      toast.success(msg);
      // 先把登录态就地改成未登录, 闸门立刻切回登录页, 不等这一次重新拉取回来
      qc.setQueryData(queryKeys.session(), GUEST_SESSION);
      // 换个人登录时不该看到上一个人的域名列表, 业务缓存整个丢掉。
      // 用 predicate 挑掉 auth 那一支而不是 qc.clear(): 后者会把登录态这条查询本身也从缓存里摘掉,
      // 挂在它上面的闸门就永远等不到下一次结果, 界面卡在一片 401 上
      qc.removeQueries({ predicate: (query) => query.queryKey[0] !== "auth" });
      void qc.invalidateQueries({ queryKey: queryKeys.session() });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const user = data?.user;
  if (!user) {
    return null;
  }
  const name = user.nickname || user.username || user.email;

  return (
    <div className="ml-auto flex items-center gap-2">
      {data?.tokenError && (
        // 刷新失败不影响继续用 (令牌到期前都还有效), 所以只是提示, 不打断操作
        <span className="text-muted-foreground hidden text-xs sm:inline" title={data.tokenError}>
          登录状态未能刷新
        </span>
      )}
      {user.avatar ? (
        // eslint-disable-next-line @next/next/no-img-element -- 头像是 CZL Connect 给的任意外部地址, 走不了 next/image 的域名白名单
        <img src={user.avatar} alt="" className="size-7 rounded-full object-cover" />
      ) : (
        <span className="bg-accent text-accent-foreground flex size-7 items-center justify-center rounded-full text-xs font-medium">
          {name.slice(0, 1).toUpperCase()}
        </span>
      )}
      <span className="max-w-32 truncate text-sm" title={user.email || name}>
        {name}
      </span>
      <Button
        variant="ghost"
        size="icon"
        className="size-8"
        title="刷新登录状态"
        onClick={() => refresh.mutate()}
        disabled={refresh.isPending}
      >
        <RefreshCw className={refresh.isPending ? "animate-spin" : undefined} />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="size-8"
        title="退出登录"
        onClick={() => logout.mutate()}
        disabled={logout.isPending}
      >
        <LogOut />
      </Button>
    </div>
  );
}
