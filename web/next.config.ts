import path from "node:path";
import type { NextConfig } from "next";

// 静态导出 + 结尾斜杠, 与 Go 侧 nextstatic 托管约定对齐。
// 业务 API 全部由 Go 承载, 这里不开 Route Handler。
const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
  // 显式钉住工作区根。不写的话 Turbopack 会往上找 lockfile, 用户主目录里随便一个
  // package-lock.json 就能把根推到 C:\Users\xxx, 相对路径与产物位置全跟着漂
  turbopack: { root: path.resolve(import.meta.dirname) },
};

export default nextConfig;
