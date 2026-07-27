import { HostnameDetail } from "@/components/hostname-detail";

// 静态导出下动态段只导出两份产物:
// "_" 是占位符模板, Go 侧把真实 ID 的请求映射到它; "new" 是新增页, 有自己的真实路径。
export function generateStaticParams() {
  return [{ hostnameId: "_" }, { hostnameId: "new" }];
}

export default function HostnameDetailPage() {
  return <HostnameDetail />;
}
