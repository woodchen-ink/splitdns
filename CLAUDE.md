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
- **不监听端口**: Wails 把 webview 的请求直接交给 handler, 没有网络入口。
  前端调的还是 `/api`, 与普通 Web 应用形态一致
- **登录不是会话而是账号绑定**: 没有网络入口, 也就没有会话 Cookie / CSRF 这一套。
  `/api` 的门禁读的是本地库里那一行登录态 (`service.IsLoggedIn`), 拦的是"这台机器还没绑账号",
  不是"这个请求来路不明"

## 数据模型

- `account` — 当前登录的 CZL Connect 账号, 单行表 (主键恒为 `model.AccountRowID`)。
  令牌字段 `json:"-"`, 前端只拿得到昵称头像那些资料
- `credential` — 平台 API 凭据 (Cloudflare / 腾讯云 DNSPod)。密钥字段 `json:"-"`, 只写不读;
  **写入必须走 handler 里单独的入参结构** —— `json:"-"` 是双向的, 靠 model tag 会把请求体里的密钥一起丢掉
- `origin` — 回源目标。一个回源可被多个访问域名共用, 改一处全部跟着变。
  `address` 是落点主机名背后的源站 IP, 只在程序要替你建那条橙云记录时才用得上;
  双栈用逗号 / 空白分隔填多个 (IPv4 建 A、IPv6 建 AAAA), 解析用 `originAddresses()`。
  `saas_custom` 的 `sni` 恒等于落点值 (`fillCustomOriginSNI`, 回源库与内联落点两条写入路径都过一遍):
  CF 默认拿源服务器名握手, 留空虽然也能回源, 却会让"源站要给这个名字挂 router"从流程里消失
- `hostname` — 访问域名, 记录它在 CF 父区 / CF SaaS 区 / DNSPod 三处的落点
- `route` — `访问域名 × 线路 → 落点` 的绑定。落点二选一: 引用 `origin`, 或直接内联填值 (`origin_id` 为 0),
  判定统一走 `Route.Target()`
- `plan` / `step` — 流程实例与步骤。持久化是因为流程中多步要等 DNS 生效, 天然跨会话。
  `plan.kind` 分 `setup` (配置到位) 与 `teardown` (把痕迹撤掉), 同一域名两条流程可以并存;
  这一列之前的数据是空串, 一律按 `setup` 解释 (`Plan.PlanKind()`)

`origin.kind` 与 `step.key` 都是开放式取值: 消费侧按模式识别 + 兜底处理, 新增取值不需要前端同步发版。

**bool 字段一律不写 `default` 标签**: GORM 对带默认值的字段会跳过零值, `false` 会被悄悄写成 DB 默认的
`true` —— `step.verifiable` 和 `hostname.enabled` 都栽过 (前者让"需人工确认"的步骤先显示成能自动执行,
巡检一轮后又翻回来)。要 DB 默认值就用指针类型, 不要两者都要。

## 核心行为

- **登录** (`server/pkg/czlconnect` + `service/auth*.go` + `desktop/oauth.go`): CZL Connect 的
  Authorization Code + PKCE, Public Client 没有 `client_secret` (桌面程序里的密钥用户都挖得出来)。
  **强制登录**: 没绑账号时 `/api` 全线 401 (豁免 `/api/auth/`、`/api/healthz`、`/api/open`),
  前端 `AuthGate` 同步只渲染登录页。两层都要有 —— 只拦前端, 开个 devtools 就能拿未登录状态去改生产 DNS;
  只拦后端, 界面会变成一片报错。
  授权页走**系统浏览器**而不是自家 webview: 要复用浏览器里已有的 CZL Connect 登录态,
  而且第三方登录页放进 webview, 用户没有地址栏可以核对域名
- **令牌刷新** (`service/auth_token.go`): 剩余有效期不足 `refreshSkew` 就先刷, 由 `tokenMu` 串行化 ——
  并发拿同一个 `refresh_token` 换两次, 服务端一旦轮换就把自己刷废了。
  **只有 `invalid_grant` 才清本地登录态**: 网络不通 / 5xx 只是这次没刷上, 当成"要重新登录"会让人断个网就被自己的
  工具锁在门外; 那类失败进冷却期 (`refreshRetryGap`), 否则前端轮询会把它变成对授权服务器的连打。
  服务端不轮换时不回 `refresh_token`, **拿空值覆盖等于自己清了登录态**, 要保留旧值
- **巡检** (`service/inspect.go` + `check*.go`): 一次拉齐三个平台的实际状态, 再按规则判定。
  任何一处拉取失败都转成 Finding, 不让整个报告消失; 同时记进 `Snapshot.FetchErrors`,
  **判定层必须区分"确实没有"和"根本没读到"** —— 两者在快照里长得一模一样, 混淆会让拆除流程谎报清干净了
- **流程** (`service/plan*.go`): 步骤分 `manual` / `auto` / `wait`。
  能自动做的直接调 API, 做完仍然走一次巡检验证 —— 平台接口返回 200 不等于配置已经生效。
  **同一域名同一类型只留一条流程, 进页面时复用并把步骤对齐当前配置** (`syncSteps`: 缺的补、
  不适用的删、模板信息覆盖、状态与时间戳保留)。**不能只复用 `running` 的** —— 走完最后一步流程就变 `done`,
  再按 `running` 找必然落空而重建一条, 手动确认过的步骤全部回到未开始; 能自动验证的会被下一轮巡检立刻翻回完成,
  于是看起来只有"程序验不了的那一步"在反复退回
- **列表页的进度** (`service/plan_progress.go`): `GET /api/hostnames` 每个域名带上各条流程的完成度,
  数据全来自本地库的步骤状态 (`done + skipped` 算走完), **列表页一个平台接口都不调** —— 逐个域名巡检慢且吃配额,
  实际状态进详情页按需巡检。同一类型有多条流程时只留一条: 优先在跑的, 否则取最新的
- **账号自动匹配** (`service/discover.go`): `/api/discover/cf-zones` 返回 `zone + 归属凭据`, 不带 `credentialId`
  就聚合全部 CF 账号; `/api/discover/parent-zone` 同样带出凭据。存域名时 `cfCredentialId` 可以留空, 由访问域名反查填上 ——
  域名落在哪个账号下是客观事实, 不该让人先选账号再选区。SaaS 区落在别的账号时才写 `saasCredentialId`
- **回源落点解析** (`service/origin_dns.go`): 落点是主机名时, CF 里必须有一条指向源站的橙云记录, 流量才转得出去。
  回源是多个域名共用的公共资料、身上没有凭据与 zone, 所以按"哪份 CF 凭据看得见管辖这个名字的 zone"反查
  (最长后缀匹配, 判据同 `DeriveParentZone`)。**读不到 zone 列表时必须报"无法判定"而不是"不归我们管"** ——
  后者会让整条检查静默消失。建记录只补缺失, 已存在的一律不覆盖: 那是线上正在生效的解析。
  **同名多条记录是正常的** (A + AAAA 双栈), 只有同一类型重复才算说不清; 登记的源站 IP 只跟同类型的那条比。
  回退源那条橙云记录走的是同一个 `ensureOriginRecord`
- **拆除流程** (`service/plan_teardown*.go`, `step.key` 前缀 `teardown.`): 按 撤委派 → 清 DNSPod 记录 →
  删 DNSPod 域名 → 删自定义主机名 → 清回退源 → 清父区验证记录 的顺序逐步撤。
  **第一步必须是撤委派**: 反过来先删 DNSPod 域名, 委派还指着不再托管它的 NS, 解析器拿到 SERVFAIL 且会一直重试。
  删自定义主机名时一并清掉本工具为它建的落点记录, 但区里还有别的自定义主机名指着同一个源服务器、
  或它本身就是回退源时不动 —— 少删一条只是残留, 多删一条是别人的线上流量。
  删本地记录不在流程里 (删完流程自己也没了), 走 `DELETE /api/hostnames/{id}`
- **不可逆操作**: 清理父区被遮蔽的记录, 以及全部拆除步骤。未带 `confirm` 时只返回待删清单并报 `ErrNeedConfirm`,
  清单必须逐条列出来, 只报"有 N 条"等于让人闭眼点确认
- **跳过**: `step.status = skipped` 是终态, 不再验证也不阻塞流程收尾。给拆除用 ——
  想留着 DNSPod 域名、回退源还有别人在用, 都是合理的"这步不做"
- **验证退回**: 曾经通过的步骤在巡检发现线上被改动后会退回 `waiting`, 不会一直显示完成
- **数据搬家** (`service/backup.go`): 导出走 `VACUUM INTO` 取一致性快照; 导入前校验文件头与必备表,
  替换前另存带时间戳的备份, 写入失败自动回滚。
  **导出副本里的 `account` 表会被清掉** (`stripAccount`): 备份是业务数据不是身份, 那份 `refresh_token`
  拿到手就能以本人身份调 CZL Connect。平台密钥保留 (换机器就是要它们), 登录换台机器重登一次。
  同理导入别人的库之后本机会变成未登录 —— `importTables` 不要求有 `account`, 老备份照样导得进来

## 平台上踩过的坑 (改动相关代码前先看这里)

- **CF for SaaS 的自定义源服务器不是解析目标**: 解析指向 SaaS 区里任意一条橙云记录即可, 边缘按 Host 头找自定义主机名
- **但自定义源服务器自己必须是本账号 DNS 里的一条橙云记录** (CF 明文要求, 不能填 IP): 没建或者是灰云,
  回源就直接失败, 而自定义主机名页面上主机名状态、证书状态照样显示有效, 从那边一点异常都看不出来
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
- **CZL Connect 只支持 PKCE 的 `S256`**, `plain` 发过去会被拒
- **授权回跳走 `splitdns://callback` 自定义协议**, 落点是桌面壳而不是某个 HTTP 接口。
  正常通路是 Wails 的单实例锁: 系统拉起第二个进程, 把回调地址转交给已经开着的那个 ——
  **PKCE 的 `verifier` 只在发起授权那个进程的内存里**, 转交不到就换不出令牌 (冷启动收到回调必然如此,
  提示用户重新发起即可, 不要为此把 verifier 落库)。
  协议注册有三处: NSIS 装机时 (`wails.json` 的 `info.protocols`)、macOS 的 `CFBundleURLTypes` (同一份配置生成)、
  以及每次启动时写 HKCU (`desktop/protocol_windows.go`) —— 最后这处是给绿色版和"换过目录"准备的。
  三处写的描述都得是 Windows 惯例的 `URL:splitdns Protocol`, 且**只能用 ASCII**:
  它会被塞进 NSIS 脚本, makensis 不按 UTF-8 读的话中文就成乱码。
  **`build/windows/installer/*.nsh` 生成一次之后 Wails 就不再覆盖了**, 改 `wails.json` 的 `protocols` 必须同步手改那份脚本。
  注意 `wails dev` 也会写 HKCU, 会把协议指到临时构建产物上, 跑一次正式程序即可覆盖回来
- **任何一处协议都可能不通** (安全软件拦、Linux 没有发行形态): 登录页始终留着"手动粘贴回调地址"那条路
  (`POST /api/auth/callback`), 别把它当成调试功能删掉

## 前端要点

- **主机名一律用 `components/host-picker.tsx` 输入**: 打前缀 + 选 CF 域名后缀, 后缀选不到时才退回手打整串。
  `manual` 状态由调用方持有, 不能靠"当前值匹不匹配得上"反推 —— 那样用户刚清空前缀就会被弹回选择模式

- 这版 shadcn 底层是 **Base UI 不是 Radix**: `Select.Value` 默认渲染 value 本身, 要传函数才显示选项文字;
  `Button` 没有 `asChild`, 用 `buttonVariants()` 给 `Link` 加 class
- **静态导出的动态路由不能用 `useParams`**: 真实 ID 由 Go 映射到 `_` 占位符模板, 那份模板构建期的参数字面量就是 `_`;
  要从 `usePathname()` 解析, 且等挂载后再判定
- **`AuthGate` 包住导航与全部页面**: 未登录时整页只有登录界面, 不给一排点不动的入口。
  等待授权回跳期间才开轮询 (`waiting` 为真时 1s 一次) —— 授权在应用外面完成, 前端没有任何回调可接;
  平时一次都不多问。任何接口返回 401 都在 `providers.tsx` 的 cache `onError` 里统一失效登录态,
  不要在各个调用点各判一次
- **退出登录不能 `qc.clear()`**: 它连登录态那条查询一起从缓存里摘掉, 挂在上面的闸门就再也等不到下一次结果,
  界面卡在业务页的一片 401 上。要清就用 `predicate` 挑掉 `auth` 之外的那些, 再把登录态就地写成未登录

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

图标是用代码画的 (SDF 光栅化, 每个尺寸按自身分辨率单独渲染, 不是缩放大图),
改完 `desktop/tools/icongen/main.go` 里的坐标 / 配色重跑即可, 一次覆盖三处产物
(`desktop/build/appicon.png`、`desktop/build/windows/icon.ico`、`web/app/favicon.ico`):

```bash
cd desktop && go run ./tools/icongen
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
