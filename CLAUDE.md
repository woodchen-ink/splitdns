# splitdns

把一个子域名从 Cloudflare 委派到 DNSPod 做分线路解析、并配合 Cloudflare for SaaS 让其中一条线路继续走 CF CDN
—— 这套流程的配置、执行与巡检工具。

## 端目录

- `server/` — Go 后端。同时承载 `/api` 与 `web/out` 静态产物; 同一个二进制也是 CLI (`splitdns check`)
- `web/` — Next.js 16 静态导出前端 (Tailwind + Shadcn UI + TanStack Query)
- `desktop/` — Wails 桌面壳。窗口里跑的就是 `server/` 那套 handler, 前端一行不用改

## 偏离默认架构的记录

- **数据库用 SQLite 而不是 PostgreSQL**: 单用户自托管工具, 数据量是几十到几百行, 一个文件挂卷即可, 备份就是拷文件。
  驱动选 `glebarez/sqlite` (纯 Go 实现), 因此镜像可以 `CGO_ENABLED=0` 编译并保持 alpine 小体积
- **`desktop/` 直接 import `server/` 的包** (`go.mod` 里 `replace ../server`): 桌面版不是另一个前端, 而是同一套后端的另一个外壳,
  复制一份业务代码只会让两边漂移。这偏离了"端目录之间禁止互相引用源码"的默认约定, 影响范围仅限 `desktop/main.go`
- **桌面版不监听端口**: Wails 把 webview 的请求直接交给 handler, 没有 TCP 端口也就没有鉴权的必要;
  前端调的还是 `/api`, 与服务端形态完全一致
- **应用不实现登录**: 认证交给 Cloudflare Access。应用侧只校验边缘下发的 `Cf-Access-Jwt-Assertion`,
  防止有人绕过 Access 直连源站。未配置 Access 时应用拒绝监听非回环地址, 除非显式设 `ALLOW_INSECURE_BIND=true`

## 数据模型

- `credential` — 平台 API 凭据 (Cloudflare / 腾讯云 DNSPod)。密钥字段 `json:"-"`, 只写不读
- `origin` — 回源目标。一个回源可被多个访问域名共用, 改一处全部跟着变
- `hostname` — 访问域名, 记录它在 CF 父区 / CF SaaS 区 / DNSPod 三处的落点
- `route` — `访问域名 × 线路 → 回源` 的绑定; 多访问域名多回源靠这张表组合
- `plan` / `step` — 配置流程实例与步骤。持久化是因为流程中多步要等 DNS 生效, 天然跨会话

`origin.kind` 与 `step.key` 都是开放式取值: 消费侧按模式识别 + 兜底处理, 新增取值不需要前端同步发版。

## 核心行为

- **巡检** (`service/inspect.go` + `check*.go`): 一次拉齐三个平台的实际状态, 再按规则判定。
  任何一处拉取失败都转成 Finding, 不让整个报告消失
- **流程** (`service/plan*.go`): 步骤分 `manual` / `auto` / `wait`。
  能自动做的直接调 API, 做完仍然走一次巡检验证 —— 平台接口返回 200 不等于配置已经生效
- **不可逆操作**: 只有"清理父区被遮蔽的记录"会删数据, 未带 `confirm` 时只返回待删清单并报 `ErrNeedConfirm`
- **验证退回**: 曾经通过的步骤在巡检发现线上被改动后会退回 `waiting`, 不会一直显示完成

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORT` | `8080` | 监听端口 |
| `BIND_HOST` | `127.0.0.1` | 监听地址 |
| `DATABASE_PATH` | `/data/splitdns.db` | SQLite 文件路径 |
| `STATIC_ROOT` | `../web/out` | 前端静态产物目录 |
| `TZ` | `Asia/Shanghai` | 业务时区, 加载失败直接退出 |
| `CF_ACCESS_TEAM_DOMAIN` | 空 | Access 团队域名, 配了才校验身份 JWT |
| `CF_ACCESS_AUD` | 空 | Access 应用 AUD, 与团队域名必须同时配 |
| `ALLOW_INSECURE_BIND` | 空 | 设 `true` 才允许"没有 Access 却监听非回环地址" |

## 常用命令

```bash
docker compose up -d --build     # 一键启动
```

```bash
cd server && go build ./... && go vet ./...
```

```bash
cd web && npm run build && npx eslint app components lib
```

桌面版 (绿色版, 产物 `desktop/build/bin/splitdns.exe`):

```bash
cd desktop && wails build -platform windows/amd64 -webview2 embed -skipbindings -s
```

数据落在 exe 同级的 `data/` 目录, 只读位置回退到用户配置目录。前端产物由 `web` 的 `build:desktop`
脚本拷进 `desktop/frontend/dist` 再嵌进二进制 —— Go 的 embed 不能引用模块目录之外的文件。
打 `v*` tag 触发 GitHub Actions 出 Release。

CLI: `splitdns check` 巡检全部启用的域名, `splitdns check <域名|ID>` 只查指定的; 存在 error 级问题时退出码非 0。
