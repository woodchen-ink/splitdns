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
- 业务码 `4090` 表示破坏性操作需要二次确认, 待删 / 待覆盖清单在 `msg` 里, 由调用方渲染确认区
- `PlanWizard` 一套渲染跑两种流程 (`kind` = `setup` / `teardown`): 拆除时允许跳过步骤, 并且不渲染巡检报告 ——
  记录没了本来就是目的, 报告整片变红只会误导
- 数组字段可能是 `null` (Go 空切片的序列化结果), 消费前一律 `?? []`
- 检查项 `code`、步骤 `key`、回源 `kind` 都是开放式取值, 一律通用渲染 + 兜底, 不为每个值写分支
- 顶部导航在 `app/layout.tsx` 挂一次, 路由切换不重挂载; 主壳锁视口高, 只有内容区滚动
- `components/auth-gate.tsx` 是全站闸门, 连导航一起包在里面: 登录态不是 `active` 就整页换成
  `login-screen.tsx`。业务码 `401` 表示未登录, 由 `providers.tsx` 的 cache `onError` 统一失效
  `queryKeys.session()`, 组件里不各判一次
- `components/update-button.tsx` 在顶栏和登录页各挂一个: 只轮询 `GET /api/update` (读后端内存, 下载 / 安装期间 1s, 平时 60s),
  不自己触发 GitHub 检查; 能否自动安装以后端 `canAutoInstall` 为准, 前端不按平台判断
- 当前 shadcn `Button` 不支持 `asChild`, 需要按钮样式的链接用 `buttonVariants()` 给 `Link` 加 class
- **字体走 `geist` 包, 不要用 `next/font/google`**: 后者构建时要现拉 fonts.googleapis.com,
  那个域名在国内连不上, 本地一构建就直接失败 (CI 有网所以只坏本地)。CSS 变量名两者一致, 换回去不会报错、只会构建不了
- `next.config.ts` 显式钉了 `turbopack.root`: 不写的话 Turbopack 会往上找 lockfile,
  用户主目录里随便一个 `package-lock.json` 就能把工作区根推到 `C:\Users\xxx`

## 命令

```bash
npm run dev
```

```bash
npm run build && npx eslint app components lib
```
