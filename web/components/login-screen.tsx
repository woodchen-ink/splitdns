"use client";

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, postWithMessage } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Session } from "@/lib/types";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface LoginStart {
  authorizeUrl: string;
  opened: boolean;
}

// LoginScreen 是未登录时的整个界面。
//
// 授权在系统浏览器里进行, 完成后由 splitdns:// 回跳唤起本程序, 所以这一页要同时承担三件事:
// 发起授权、把"正在等回跳"这个状态说清楚、以及协议没通时给出一条能自己走完的退路。
export function LoginScreen({ session }: { session: Session }) {
  const qc = useQueryClient();
  const [authorizeUrl, setAuthorizeUrl] = useState("");
  const [pastedUrl, setPastedUrl] = useState("");
  const [manual, setManual] = useState(false);

  const refreshSession = () => qc.invalidateQueries({ queryKey: queryKeys.session() });

  const start = useMutation({
    mutationFn: () => postWithMessage<LoginStart>("/api/auth/login"),
    onSuccess: ({ data, msg }) => {
      setAuthorizeUrl(data.authorizeUrl);
      // 浏览器没打开时把手动区直接展开, 不让用户自己去找那条退路
      if (!data.opened) {
        setManual(true);
        toast.warning(msg);
      } else {
        toast.success(msg);
      }
      void refreshSession();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  // 再打开一次用的是同一个地址, 而不是重新发起授权:
  // 重发会换掉 state 与 PKCE 参数, 用户手里那个已经打开的授权页就作废了
  const reopen = useMutation({
    mutationFn: () => api.post("/api/open", { url: authorizeUrl }),
    onError: (e: Error) => toast.error(e.message),
  });

  const submitCallback = useMutation({
    mutationFn: () => postWithMessage<Session>("/api/auth/callback", { url: pastedUrl }),
    onSuccess: ({ msg }) => {
      toast.success(msg);
      setPastedUrl("");
      void refreshSession();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const waiting = session.waiting;

  return (
    <div className="flex h-full items-center justify-center overflow-y-auto px-4 py-10">
      <div className="w-full max-w-md space-y-6">
        <div className="space-y-1.5 text-center">
          <h1 className="text-2xl font-semibold tracking-tight">splitdns</h1>
          <p className="text-muted-foreground text-sm">
            用 CZL Connect 账号登录后才能操作解析配置
          </p>
        </div>

        {session.status === "expired" && (
          <Alert>
            <AlertTitle>登录已过期</AlertTitle>
            <AlertDescription>
              {session.user
                ? `${session.user.nickname || session.user.username} 的授权已被服务端收回, 需要重新授权一次。`
                : "本机的授权已经失效, 需要重新授权一次。"}
            </AlertDescription>
          </Alert>
        )}

        {session.loginError && (
          <Alert variant="destructive">
            <AlertTitle>上一次授权没有完成</AlertTitle>
            <AlertDescription>{session.loginError}</AlertDescription>
          </Alert>
        )}

        <div className="border-border/60 space-y-4 rounded-xl border p-5">
          <Button className="w-full" onClick={() => start.mutate()} disabled={start.isPending}>
            {start.isPending ? "正在打开浏览器…" : waiting ? "重新发起授权" : "使用 CZL Connect 登录"}
          </Button>

          {waiting && (
            <p className="text-muted-foreground text-center text-sm leading-relaxed">
              已在浏览器中打开授权页, 点完同意就会自动回到这里。
              <br />
              浏览器那个标签页会停在原地不动, 属正常现象, 直接关掉即可。
            </p>
          )}

          {authorizeUrl && (
            <div className="space-y-2">
              <Label className="text-muted-foreground text-xs">授权地址</Label>
              <Input readOnly value={authorizeUrl} className="font-mono text-xs" />
              <Button
                variant="outline"
                size="sm"
                className="w-full"
                onClick={() => reopen.mutate()}
                disabled={reopen.isPending}
              >
                再打开一次这个地址
              </Button>
            </div>
          )}

          <div className="border-border/60 border-t pt-3">
            <Button
              variant="ghost"
              size="sm"
              className="w-full"
              onClick={() => setManual((v) => !v)}
            >
              {manual ? "收起" : "浏览器授权完了却没回到应用?"}
            </Button>

            {manual && (
              <form
                className="mt-3 space-y-2"
                onSubmit={(e) => {
                  e.preventDefault();
                  submitCallback.mutate();
                }}
              >
                <p className="text-muted-foreground text-xs leading-relaxed">
                  说明 splitdns:// 协议没有注册成功 (换过安装目录、或者被安全软件拦下)。
                  把浏览器地址栏里那条 splitdns://callback 开头的地址整条复制过来即可完成登录。
                </p>
                <Input
                  value={pastedUrl}
                  onChange={(e) => setPastedUrl(e.target.value)}
                  placeholder="splitdns://callback?code=…&state=…"
                  className="font-mono text-xs"
                />
                <Button
                  type="submit"
                  variant="outline"
                  size="sm"
                  className="w-full"
                  disabled={!pastedUrl || submitCallback.isPending}
                >
                  用这条地址完成登录
                </Button>
              </form>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
