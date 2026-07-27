import type { NextConfig } from "next";

// 静态导出 + 结尾斜杠, 与 Go 侧 nextstatic 托管约定对齐。
// 业务 API 全部由 Go 承载, 这里不开 Route Handler。
const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
};

export default nextConfig;
