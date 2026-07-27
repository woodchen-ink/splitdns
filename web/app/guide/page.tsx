"use client";

import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";

// 教程正文放在论坛帖子里, 这里只做跳转。
// 内容跟着实际用法一直在变, 放在能随时改的地方比编进二进制里合适 ——
// 否则每改一句都得重新发版, 用旧版本的人还看不到。
const GUIDE_URL = "https://www.sunai.net/t/topic/1456";

const TOPICS = [
  "凭据要什么权限, 怎么确认范围够用",
  "「CF SaaS 落点」和「CF SaaS 自定义源」的区别 —— 最容易配反的一对",
  "四步走完整流程, 每步按钮的含义",
  "被遮蔽的记录、同名多条验证 TXT、DNSPod 默认暂停等几个必踩的坑",
];

export default function GuidePage() {
  const open = useMutation({
    // webview 里 target="_blank" 不可靠, 交给 Go 调系统浏览器
    mutationFn: () => api.post("/api/open", { url: GUIDE_URL }),
    onError: (e: Error) => toast.error(`${e.message}，可以手动复制: ${GUIDE_URL}`),
  });

  return (
    <div className="max-w-2xl space-y-5">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">教程</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          从零把一个子域名配成「海外走 Cloudflare、国内走别的 CDN」。
        </p>
      </div>

      <div className="border-border/60 space-y-4 rounded-xl border p-4">
        <ul className="space-y-1.5 text-sm">
          {TOPICS.map((t) => (
            <li key={t} className="text-muted-foreground flex gap-2">
              <span aria-hidden>·</span>
              <span>{t}</span>
            </li>
          ))}
        </ul>

        <div className="flex flex-wrap items-center gap-3">
          <Button onClick={() => open.mutate()} disabled={open.isPending}>
            {open.isPending ? "正在打开…" : "在浏览器中打开教程"}
          </Button>
          <code className="text-muted-foreground text-xs break-all">{GUIDE_URL}</code>
        </div>
      </div>
    </div>
  );
}
