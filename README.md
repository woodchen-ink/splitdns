# splitdns

主域名留在 Cloudflare，只把一个子域名委派给 DNSPod 做国内外分流 —— 这套流程的配置、执行与巡检工具。

## 它解决什么

Cloudflare 的权威 DNS 不支持普通记录按地区返回不同答案，而记录一旦是橙云，返回的又是全球一致的 anycast IP。
想让一个子域名「海外走 CF CDN、国内走别的 CDN」，只能把这一个名字委派给支持线路的 DNS 服务商，
再用 Cloudflare for SaaS 让 CF 继续代理这个已经不在它手里的主机名。

流程本身不难，但环节多、每步都要等生效、还有几个不看文档就会踩的坑：

- 委派后留在父区的记录会变成 shadowed records，看得见但一条都不生效
- CF 的 DCV TXT 在证书带通配符 SAN 时是多条同名不同值，少一条证书就签不出来
- CF 那条解析必须挂在「默认」线兜底，只配境内 + 境外会让部分解析器拿不到记录
- 用了自定义源服务器，源站必须能路由那个 SNI，否则回源直接 403

这个工具把流程拆成带验证的步骤，能自动做的直接调 API，做不了的给出精确指令并轮询确认。

## 功能

- **域名列表**：所有委派出去的访问域名及其线路落点
- **配置向导**：一步步把域名配到位。每步标明是程序执行还是手动操作、预计多久生效、已经等了多久；关掉页面回来能续上
- **巡检**：随时核对三个平台的实际状态，报告委派、遮蔽记录、验证 TXT、线路落点、证书等问题
- **CLI**：`splitdns check` 可以塞进 cron 做定期巡检

## 快速开始

```bash
docker compose up -d --build
```

打开面板后先在「凭据」里填 Cloudflare API Token 和腾讯云 SecretId/SecretKey，
再到「回源」建好回源目标，最后在「域名」里新增访问域名并跟着向导走。

### 访问控制

应用自己不做登录，认证交给 Cloudflare Access。配上这两个环境变量后，应用会校验边缘下发的身份 JWT：

```bash
CF_ACCESS_TEAM_DOMAIN=yourteam.cloudflareaccess.com
CF_ACCESS_AUD=<Access 应用的 AUD tag>
```

没配 Access 时应用只允许监听回环地址。确实跑在受控网络的反代后面，用 `ALLOW_INSECURE_BIND=true` 显式放行。

## 开发

```bash
cd server && go run .
```

```bash
cd web && npm run dev
```

前端静态导出后由 Go 一并托管，生产不跑 Node。
