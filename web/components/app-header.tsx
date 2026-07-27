"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";

const NAV = [
  { href: "/", label: "域名" },
  { href: "/origins/", label: "回源" },
  { href: "/credentials/", label: "凭据" },
  { href: "/data/", label: "数据" },
];

// AppHeader 是全站唯一导航。当前项按路径前缀高亮, 首页只在完全匹配时高亮,
// 否则详情页会把三个入口全点亮。
export function AppHeader() {
  const pathname = usePathname();

  return (
    <header className="border-border/60 bg-background/95 supports-[backdrop-filter]:bg-background/75 sticky top-0 z-20 shrink-0 border-b backdrop-blur">
      <div className="mx-auto flex w-full max-w-6xl items-center gap-6 px-4 py-3 sm:px-6">
        <Link href="/" className="text-base font-semibold tracking-tight">
          splitdns
        </Link>
        <nav className="flex items-center gap-1 text-sm">
          {NAV.map((item) => {
            const active =
              item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "rounded-md px-3 py-1.5 transition-colors",
                  active
                    ? "bg-accent text-accent-foreground font-medium"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent/50",
                )}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
      </div>
    </header>
  );
}
