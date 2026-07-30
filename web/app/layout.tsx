import type { Metadata } from "next";
// 字体走 geist 包而不是 next/font/google: 后者在构建时要去 fonts.googleapis.com 现拉,
// 那个域名在国内连不上, 本地一构建就直接失败。这个包把 woff2 装在 node_modules 里,
// 变量名 (--font-geist-sans / --font-geist-mono) 与之前完全一致
import { GeistMono } from "geist/font/mono";
import { GeistSans } from "geist/font/sans";
import "./globals.css";
import { Providers } from "./providers";
import { AppHeader } from "@/components/app-header";
import { AuthGate } from "@/components/auth-gate";
import { Toaster } from "@/components/ui/sonner";

export const metadata: Metadata = {
  title: {
    default: "splitdns",
    template: "%s - splitdns",
  },
  description: "分线路解析的配置与巡检",
};

// RootLayout 是唯一的应用外壳: 顶部导航在这里挂一次, 路由切换不重挂载。
// 主壳锁视口高度, 内容区自己滚动。
//
// 导航连同页面一起放在 AuthGate 里面: 未登录时整页只有登录界面, 不给一排点不动的入口。
export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="zh-CN"
      className={`${GeistSans.variable} ${GeistMono.variable} h-full antialiased`}
    >
      <body className="bg-background text-foreground h-full">
        <Providers>
          <AuthGate>
            <div className="flex h-full flex-col">
              <AppHeader />
              <main className="min-h-0 flex-1 overflow-y-auto">
                <div className="mx-auto w-full max-w-6xl px-4 py-6 sm:px-6">{children}</div>
              </main>
            </div>
          </AuthGate>
          <Toaster position="top-center" richColors />
        </Providers>
      </body>
    </html>
  );
}
