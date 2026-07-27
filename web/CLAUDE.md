# web —— splitdns 前端

Next.js 16 App Router + `output: 'export'` 静态导出, Tailwind + Shadcn UI + TanStack Query。
构建产物 `web/out` 由 `server/` 的 Go 服务托管, 生产不跑 Node。

## 目录

- `app/` — 路由页面。`app/hostnames/[hostnameId]/` 静态导出两份产物: `_` 是占位符模板 (Go 侧把真实 ID 映射到它), `new` 是新增页
- `components/` — 业务组件; `components/ui/` 是 shadcn CLI 的领地, 不手写
- `lib/` — `api.ts` 统一客户端、`queries.ts` query key、`types.ts` 与后端 JSON 契约对应

## 约定

- 所有数据走同源 `/api`, 由 Go 承载; 不开 Next.js Route Handler
- 后端统一返回 `{ code, data, msg }`, `lib/api.ts` 把非 200 业务码转成 `ApiError`, 组件只处理 `data`
- 业务码 `4090` 表示破坏性操作需要二次确认, 待删清单在 `msg` 里, 由调用方渲染确认区
- 数组字段可能是 `null` (Go 空切片的序列化结果), 消费前一律 `?? []`
- 检查项 `code`、步骤 `key`、回源 `kind` 都是开放式取值, 一律通用渲染 + 兜底, 不为每个值写分支
- 顶部导航在 `app/layout.tsx` 挂一次, 路由切换不重挂载; 主壳锁视口高, 只有内容区滚动
- 当前 shadcn `Button` 不支持 `asChild`, 需要按钮样式的链接用 `buttonVariants()` 给 `Link` 加 class

## 命令

```bash
npm run dev
```

```bash
npm run build && npx eslint app components lib
```
