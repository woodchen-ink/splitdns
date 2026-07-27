# splitdns

把一个子域名从 Cloudflare 委派到 DNSPod 做分线路解析、并配合 Cloudflare for SaaS 让其中一条线路继续走 CF CDN
—— 这套流程的配置、执行与巡检工具。只有桌面版一种形态。

## 端目录

- `server/` — 业务本体: `/api` 处理器、巡检规则、平台客户端、数据库。**不是独立进程**, 没有 main
- `web/` — Next.js 16 静态导出前端 (Tailwind + Shadcn UI + TanStack Query)
- `desktop/` — Wails 桌面壳。窗口里跑的就是 `server/` 那套 handler, 前端一行不用改

## 偏离默认架构的记录

- **数据库用 SQLite 而不是 PostgreSQL**: 单用户桌面工具, 数据量是几十到几百行, 一个文件即可, 备份就是拷文件。
  驱动选 `glebarez/sqlite` (纯 Go 实现), 因此不需要 CGO
- **`server/` 是库不是端**: 它保留端目录的位置与命名, 但没有 `main.go`, 由 `desktop/` 通过 `go.mod` 的
  `replace ../server` 引用。桌面版不是另一个前端, 而是同一套后端的外壳, 复制一份业务代码只会让两边漂移
- **不监听端口、没有鉴权**: Wails 把 webview 的请求直接交给 handler, 没有网络入口也就没有鉴权的必要;
  前端调的还是 `/api`, 与普通 Web 应用形态一致

## 数据模型

- `credential` — 平台 API 凭据 (Cloudflare / 腾讯云 DNSPod)。密钥字段 `json:"-"`, 只写不读;
  **写入必须走 handler 里单独的入参结构** —— `json:"-"` 是双向的, 靠 model tag 会把请求体里的密钥一起丢掉
- `origin` — 回源目标。一个回源可被多个访问域名共用, 改一处全部跟着变
- `hostname` — 访问域名, 记录它在 CF 父区 / CF SaaS 区 / DNSPod 三处的落点
- `route` — `访问域名 × 线路 → 落点` 的绑定。落点二选一: 引用 `origin`, 或直接内联填值 (`origin_id` 为 0),
  判定统一走 `Route.Target()`
- `plan` / `step` — 流程实例与步骤。持久化是因为流程中多步要等 DNS 生效, 天然跨会话。
  `plan.kind` 分 `setup` (配置到位) 与 `teardown` (把痕迹撤掉), 同一域名两条流程可以并存;
  这一列之前的数据是空串, 一律按 `setup` 解释 (`Plan.PlanKind()`)

`origin.kind` 与 `step.key` 都是开放式取值: 消费侧按模式识别 + 兜底处理, 新增取值不需要前端同步发版。

## 核心行为

- **巡检** (`service/inspect.go` + `check*.go`): 一次拉齐三个平台的实际状态, 再按规则判定。
  任何一处拉取失败都转成 Finding, 不让整个报告消失; 同时记进 `Snapshot.FetchErrors`,
  **判定层必须区分"确实没有"和"根本没读到"** —— 两者在快照里长得一模一样, 混淆会让拆除流程谎报清干净了
- **流程** (`service/plan*.go`): 步骤分 `manual` / `auto` / `wait`。
  能自动做的直接调 API, 做完仍然走一次巡检验证 —— 平台接口返回 200 不等于配置已经生效
- **拆除流程** (`service/plan_teardown*.go`, `step.key` 前缀 `teardown.`): 按 撤委派 → 清 DNSPod 记录 →
  删 DNSPod 域名 → 删自定义主机名 → 清回退源 → 清父区验证记录 的顺序逐步撤。
  **第一步必须是撤委派**: 反过来先删 DNSPod 域名, 委派还指着不再托管它的 NS, 解析器拿到 SERVFAIL 且会一直重试。
  删本地记录不在流程里 (删完流程自己也没了), 走 `DELETE /api/hostnames/{id}`
- **不可逆操作**: 清理父区被遮蔽的记录, 以及全部拆除步骤。未带 `confirm` 时只返回待删清单并报 `ErrNeedConfirm`,
  清单必须逐条列出来, 只报"有 N 条"等于让人闭眼点确认
- **跳过**: `step.status = skipped` 是终态, 不再验证也不阻塞流程收尾。给拆除用 ——
  想留着 DNSPod 域名、回退源还有别人在用, 都是合理的"这步不做"
- **验证退回**: 曾经通过的步骤在巡检发现线上被改动后会退回 `waiting`, 不会一直显示完成
- **数据搬家** (`service/backup.go`): 导出走 `VACUUM INTO` 取一致性快照; 导入前校验文件头与必备表,
  替换前另存带时间戳的备份, 写入失败自动回滚

## 平台上踩过的坑 (改动相关代码前先看这里)

- **CF for SaaS 的自定义源服务器不是解析目标**: 解析指向 SaaS 区里任意一条橙云记录即可, 边缘按 Host 头找自定义主机名
- **`custom_origin_sni` 是企业版字段** (错误码 1456): 与源服务器同名时根本不用发, CF 默认就拿它当 SNI
- **DNSPod 免费版 TTL 最低 600**, 更小的值接口直接拒; 线路只有 默认 / 境内 / 境外
- **DNSPod 新加的域名默认暂停**, 不启用解析则记录全对也不生效; 状态判定用黑名单 (只有 PAUSE/SPAM 算停),
  白名单只认 `ENABLE` 会把 `LOCK` 和小写误判
- **腾讯云同一语义的错误码挂在不同前缀下** (`InvalidParameter.` / `FailedOperation.`), 按后缀匹配;
  "域名不存在"因此收敛成 `dnspod.ErrDomainNotFound`, 不要把它和网络 / 权限错误混在一起判
- **CF for SaaS 的回退源是整个区共享的**: 区里还有别的自定义主机名时删掉它, 那些主机名会一起失效。
  拆除前必须先数一遍 (`ListCustomHostnames`), 有别人就拒绝执行而不是让用户确认一下就删
- **本工具写进 CF 的记录都带 `splitdns` 开头的备注**: 拆除时靠 `comment.startswith` 认回自己的痕迹,
  名字和类型都不稳定, 只有备注是。新增写记录的地方备注也必须以 `cfCommentPrefix` 开头
- **WebView2 里 multipart 上传的文件部分是空的**: 上传走裸请求体, 别用 `FormData`

## 前端要点

- 这版 shadcn 底层是 **Base UI 不是 Radix**: `Select.Value` 默认渲染 value 本身, 要传函数才显示选项文字;
  `Button` 没有 `asChild`, 用 `buttonVariants()` 给 `Link` 加 class
- **静态导出的动态路由不能用 `useParams`**: 真实 ID 由 Go 映射到 `_` 占位符模板, 那份模板构建期的参数字面量就是 `_`;
  要从 `usePathname()` 解析, 且等挂载后再判定

## 命令

```bash
cd desktop && wails dev
```

```bash
cd server && go build ./... && go vet ./...
```

```bash
cd web && npm run build && npx eslint app components lib
```

出包 (产物在 `desktop/build/bin/`):

```bash
cd desktop && wails build -platform windows/amd64 -webview2 embed -skipbindings -nsis
```

Windows 本地出包也可以直接双击仓库根的 `build-installer.bat` (查依赖 → 构建 → 打开产物目录)。
**这个文件必须保持 GBK 编码**: cmd.exe 按活动代码页逐行读批处理, 存成 UTF-8 会把中文行拦腰截断当命令执行,
加 `chcp 65001` 只会更糟 (改代码页会打乱 cmd 对文件的字节定位)。

```bash
cd desktop && wails build -platform darwin/universal -skipbindings
```

**macOS 端只能在 macOS 上构建** (要链 WebKit, 交叉编译不可行), CI 里是独立的 job。
macOS 上数据一律走 `~/Library/Application Support/splitdns` —— 二进制在 .app 包里,
往旁边写会污染包并破坏签名。发出去的 .app 没签名没公证, Gatekeeper 会拦, 说明写在 Release 里。

前端产物由 `web` 的 `build:desktop` 脚本拷进 `desktop/frontend/dist` 再嵌进二进制 —— Go 的 embed 不能引用
模块目录之外的文件。**构建时别带 `-s`**: 该目录不进仓库, 跳过前端构建的话 Wails 会塞一个占位 `index.html`,
编译照样通过, 装出来却是个空壳。启动时有一道校验会拦住这种包。运行时按内容哈希决定是否重新摊到数据目录, **别改回按大小之类的近似判断**:
只改子页面时首页大小不变, 会让新二进制配着旧前端跑。

打 `v*` tag 触发 GitHub Actions 出 Release。
