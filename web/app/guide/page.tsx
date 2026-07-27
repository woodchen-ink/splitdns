import type { Metadata } from "next";
import type { ReactNode } from "react";
import { Separator } from "@/components/ui/separator";

export const metadata: Metadata = {
  title: "教程",
};

// 教程页是纯静态内容, 不需要客户端交互, 保持服务端组件即可。
// 内容与 README 讲的是同一套东西, 但这里面向"正在用这个工具"的人:
// 只说在界面上怎么走, 以及每一步背后的原因。
export default function GuidePage() {
  return (
    <article className="max-w-3xl space-y-8">
      <header>
        <h1 className="text-xl font-semibold tracking-tight">教程</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          从零把一个子域名配成「海外走 Cloudflare、国内走别的 CDN」。
        </p>
      </header>

      <Section title="它在解决什么">
        <p>
          Cloudflare 的权威 DNS <Em>不支持普通记录按地区返回不同答案</Em>；而记录一旦是橙云，
          返回的又是全球一致的 anycast IP。所以「同一个域名，不同地区解析到不同地方」这件事，
          CF 自己做不到。
        </p>
        <p>
          唯一的出路是把<Em>这一个子域名</Em>的权威交给支持线路的 DNS 服务商（这里用 DNSPod），
          主域名和其它记录仍然留在 CF。但这样一来 CF 就不再管这个名字了，橙云、缓存、WAF 全部失效——
          于是再用 <Em>Cloudflare for SaaS</Em> 把其中一条线路带回 CF：它的特点正是
          「DNS 不在 CF 也能让 CF 代理这个主机名」。
        </p>
        <Pre>{`example.com (Cloudflare)
    ├── www / mail / 其它所有记录 ──> 照旧, 完全不受影响
    └── img  NS ──> DNSPod            ← 只有这一条被委派出去
                      │
                      ├── 境内线 ──> CNAME 国内 CDN
                      └── 默认线 ──> CNAME SaaS 区的橙云记录 ──> CF 边缘 ──> 源站`}</Pre>
        <p className="text-muted-foreground text-sm">
          注意被委派的必须是<Em>子域名</Em>。域名本身（zone apex）的 NS 由注册商控制，
          不是 CF 区里能加的记录，那种情况只能把整个域名的 NS 换掉。
        </p>
      </Section>

      <Separator />

      <Section title="第一步 · 凭据">
        <p>去「凭据」页各加一条。密钥保存后不再回显，只能覆盖。</p>
        <Table
          head={["平台", "要什么", "权限"]}
          rows={[
            [
              "Cloudflare",
              "API Token",
              "Zone:Read、DNS:Edit、SSL and Certificates:Edit；作用范围要同时覆盖父区和 SaaS 区",
            ],
            ["腾讯云 DNSPod", "SecretId / SecretKey", "DNSPod 读写"],
          ]}
        />
        <p>
          填完点<Em>「检测」</Em>。Cloudflare 会把这份 Token 能看到的 zone 全部列出来——
          Token 有效和权限范围够用是两回事，列表里没有你要用的 zone 就是白搭。
        </p>
      </Section>

      <Separator />

      <Section title="第二步 · 回源（可跳过）">
        <p>
          「回源」页登记那些<Em>会被多个域名共用</Em>的落点，以后换机器只改这一处。
          只给一个域名用的落点（比如 EdgeOne 那种一域名一 CNAME），在配域名时直接填就行，不必先来这里建。
        </p>
        <Table
          head={["类型", "什么时候用", "落点值填什么"]}
          rows={[
            ["CF SaaS 落点", "这条线要走 Cloudflare", "SaaS 区里任意一条橙云记录的主机名"],
            ["CF SaaS 自定义源", "走 CF，但要回源到另一台机器", "那台机器在 SaaS 区里的主机名"],
            ["第三方 CDN CNAME", "走别家 CDN", "对方给的加速 CNAME"],
            ["直连源站 IP", "不过任何 CDN", "源站 IP"],
          ]}
        />
        <Note>
          「CF SaaS 落点」不必非得是被设为「回退源」的那一条。流量到了 CF 边缘是按 Host 头
          找自定义主机名的，CNAME 目标只负责把流量带进这个区，同区任意橙云记录都行。
        </Note>
      </Section>

      <Separator />

      <Section title="第三步 · 域名">
        <p>「域名 → 新增域名」，只有四件事要填：</p>
        <ul className="list-disc space-y-1.5 pl-5">
          <li>
            <Em>访问域名</Em>——填完父区会自动推导出来（拿它去凭据可见的 zone 里找最长后缀匹配），
            输入框下面会显示推导结果
          </li>
          <li>
            <Em>Cloudflare 凭据</Em>和 <Em>DNSPod 凭据</Em>
          </li>
          <li>
            <Em>SaaS 区</Em>——从下拉里选；这条线不走 CF 就选「不使用 CF for SaaS」
          </li>
          <li>
            <Em>线路落点</Em>——每条线路指一个落点，可以引用回源库，也可以直接填
          </li>
        </ul>
        <Note tone="warn">
          「默认」线是兜底，务必配上。只配境内 + 境外的话，识别不出归属的解析器会<Em>一条记录都拿不到</Em>。
          建议把覆盖面最广的那条挂在「默认」上。
        </Note>
        <p className="text-muted-foreground text-sm">
          DNSPod 免费版只有 默认 / 境内 / 境外 三条线路，要精确到某个国家得升级套餐。
        </p>
      </Section>

      <Separator />

      <Section title="第四步 · 跟着流程走">
        <p>保存后自动进入配置流程。每一步下面的按钮：</p>
        <Table
          head={["按钮", "含义"]}
          rows={[
            ["自动执行", "程序调 API 替你做"],
            ["我自己做完了", "你在 CF / DNSPod 面板里做完后点，等待计时从这一刻开始"],
            ["确认完成", "只出现在程序验不了的步骤（比如源站的 SNI 路由）"],
          ]}
        />
        <p>
          不管哪种方式，做完都会重新去三个平台核对一遍。
          <Em>平台接口返回成功不算数，巡检查到真的生效才算完成</Em>。所以：
        </p>
        <ul className="list-disc space-y-1.5 pl-5">
          <li>关掉程序隔天回来，进度还在，而且会按当前真实状态重新判定</li>
          <li>已经通过的步骤如果线上被人改了，会自动退回「等待生效」并说明差在哪</li>
          <li>清理父区旧记录那步会先列出每一条，点确认才动手</li>
        </ul>
        <Note>
          清理那一步默认给的是<Em>「先迁移到 DNSPod 再删」</Em>——被遮蔽不等于该扔，那些记录本来在正常服务，
          只是委派之后待错了地方。它会逐条判断：接入域名本身只删不删搬（落点归线路配置管），
          其余原样搬走。橙云记录会单独提示，因为搬到 DNSPod 之后就不再经过 CF 了。
        </Note>
      </Section>

      <Separator />

      <Section title="几个不看文档就会踩的坑">
        <Pitfall title="委派后留在父区的记录会变成 shadowed records">
          它们在 CF 面板里看得见，但一条都不生效。排查时最容易被它们带偏——以为配了，其实没生效。
        </Pitfall>
        <Pitfall title="CF 的验证 TXT 可能是同名多条">
          证书带通配符 SAN 时，基础域名和通配符各要一条 DCV，主机记录同名、值不同。
          少写一条证书就签不出来，而且报错不会告诉你少了哪条。
        </Pitfall>
        <Pitfall title="DNSPod 新加的域名默认是暂停状态">
          这时候委派、记录可以全对，解析就是不出结果。工具会自动开启，也会在巡检里单独报出来。
        </Pitfall>
        <Pitfall title="用了自定义源服务器，源站必须认得那个 SNI">
          CF 回源时 Host 头是访问域名，但 TLS 握手用的 SNI 是源服务器名字。
          源站上的 Traefik / Nginx 匹配不到对应 router 就直接 403——跟证书、跟后端服务都没关系。
          流程里那一步会给出具体到机器、名字和自检命令的指令。
        </Pitfall>
        <Pitfall title="自定义源服务器不是解析目标">
          解析要指向 SaaS 区里的橙云记录，不是指向源服务器。前者把流量带进 CF，
          后者是 CF 决定往哪转——写反了 CF 根本不认识这个名字。
        </Pitfall>
      </Section>

      <Separator />

      <Section title="换机器 / 备份">
        <p>
          「数据」页可以整库导出和导入。导出的是一致性快照，导入前会自动把现有数据另存一份备份。
        </p>
        <Note tone="warn">
          导出文件里有<Em>明文的平台密钥</Em>，当作密钥文件对待，别随手丢共享盘。
        </Note>
      </Section>
    </article>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-base font-semibold">{title}</h2>
      <div className="space-y-3 text-sm leading-relaxed">{children}</div>
    </section>
  );
}

function Em({ children }: { children: ReactNode }) {
  return <strong className="font-medium">{children}</strong>;
}

function Pre({ children }: { children: string }) {
  return (
    <pre className="bg-muted/60 overflow-x-auto rounded-lg p-3 font-mono text-xs leading-relaxed">
      {children}
    </pre>
  );
}

function Note({ children, tone = "info" }: { children: ReactNode; tone?: "info" | "warn" }) {
  const className =
    tone === "warn"
      ? "border-amber-500/30 bg-amber-500/5"
      : "border-border/60 bg-muted/40";
  return <div className={`rounded-lg border p-3 text-sm ${className}`}>{children}</div>;
}

function Pitfall({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="border-border/60 rounded-lg border p-3">
      <p className="font-medium">{title}</p>
      <p className="text-muted-foreground mt-1.5">{children}</p>
    </div>
  );
}

// Table 是教程里的轻量表格, 宽内容自己横向滚动, 页面本身不会被撑出横向滚动条。
function Table({ head, rows }: { head: string[]; rows: string[][] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-muted-foreground text-left text-xs">
          <tr>
            {head.map((h) => (
              <th key={h} className="py-1.5 pr-4 font-medium">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i} className="border-border/40 border-t align-top">
              {row.map((cell, j) => (
                <td key={j} className="py-2 pr-4">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
