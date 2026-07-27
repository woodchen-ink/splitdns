"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Credential, Hostname, Origin, Route } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// 免费版 DNSPod 只有这三条线路; 用户用付费套餐时可以直接输入其它线路名。
const COMMON_LINES = ["默认", "境内", "境外"];

// 回源类型, 与后端 model/origin.go 的常量对齐。内联落点用得上。
const ORIGIN_KINDS = [
  { value: "cname", label: "第三方 CDN CNAME" },
  { value: "ip", label: "直连源站 IP" },
  { value: "saas_fallback", label: "CF SaaS 落点 (解析指向它, 把流量带进 CF)" },
  { value: "saas_custom", label: "CF SaaS 自定义源 (CF 收到后转给它, 不是解析目标)" },
];

type DraftRoute = Pick<Route, "line" | "originId" | "kind" | "value" | "address" | "sni">;

type Draft = Omit<Hostname, "id" | "routes"> & {
  id: number;
  routes: DraftRoute[];
};

const EMPTY_ROUTE: DraftRoute = {
  line: "默认",
  originId: 0,
  kind: "cname",
  value: "",
  address: "",
  sni: "",
};

const EMPTY: Draft = {
  id: 0,
  hostname: "",
  parentZone: "",
  saasZone: "",
  dnspodDomain: "",
  cfCredentialId: 0,
  saasCredentialId: 0,
  dnspodCredentialId: 0,
  enabled: true,
  note: "",
  routes: [{ ...EMPTY_ROUTE }],
};

// HostnameForm 编辑域名与它的线路落点。线路整体替换保存, 不做逐条 diff。
export function HostnameForm({
  initial,
  onSaved,
}: {
  initial?: Hostname;
  onSaved?: (saved: Hostname) => void;
}) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<Draft>(() =>
    initial
      ? {
          ...initial,
          routes: (initial.routes ?? []).map((r) => ({
            line: r.line,
            originId: r.originId,
            kind: r.kind || "cname",
            value: r.value ?? "",
            address: r.address ?? "",
            sni: r.sni ?? "",
          })),
        }
      : EMPTY,
  );

  const { data: origins } = useQuery({
    queryKey: queryKeys.origins(),
    queryFn: () => api.get<Origin[] | null>("/api/origins"),
  });
  const { data: credentials } = useQuery({
    queryKey: queryKeys.credentials(),
    queryFn: () => api.get<Credential[] | null>("/api/credentials"),
  });

  const cfCreds = (credentials ?? []).filter((c) => c.kind === "cloudflare");
  const dpCreds = (credentials ?? []).filter((c) => c.kind === "dnspod");

  // 只有一份凭据时没什么可挑的, 直接当成已选。
  // 用派生值而不是在 effect 里回写 state: 后者要么和 lint 规则打架, 要么会覆盖用户的手动选择。
  const cfCredentialId = draft.cfCredentialId || (cfCreds.length === 1 ? cfCreds[0].id : 0);
  const dnspodCredentialId =
    draft.dnspodCredentialId || (dpCreds.length === 1 ? dpCreds[0].id : 0);

  // 平台上已有的 zone 与域名直接读出来供选, 不用手打
  const { data: cfZones } = useQuery({
    queryKey: ["discover", "cf-zones", cfCredentialId],
    queryFn: () => api.get<string[] | null>(`/api/discover/cf-zones?credentialId=${cfCredentialId}`),
    enabled: cfCredentialId > 0,
    staleTime: 5 * 60_000,
  });
  const { data: dpDomains } = useQuery({
    queryKey: ["discover", "dnspod-domains", dnspodCredentialId],
    queryFn: () =>
      api.get<string[] | null>(`/api/discover/dnspod-domains?credentialId=${dnspodCredentialId}`),
    enabled: dnspodCredentialId > 0,
    staleTime: 5 * 60_000,
  });

  // 父区由后端从可见 zone 里推导, 前端只拿来做预览 —— 推导逻辑只保留一份
  const { data: derivedParent } = useQuery({
    queryKey: ["discover", "parent-zone", cfCredentialId, draft.hostname],
    queryFn: () =>
      api.get<{ parentZone: string }>(
        `/api/discover/parent-zone?credentialId=${cfCredentialId}` +
          `&hostname=${encodeURIComponent(draft.hostname)}`,
      ),
    enabled: cfCredentialId > 0 && draft.hostname.includes("."),
    retry: false,
    staleTime: 60_000,
  });
  const parentZone = derivedParent?.parentZone ?? "";

  const save = useMutation({
    mutationFn: () =>
      api.post<Hostname>("/api/hostnames", { ...draft, cfCredentialId, dnspodCredentialId }),
    onSuccess: (saved) => {
      toast.success("已保存");
      qc.invalidateQueries({ queryKey: ["config"] });
      onSaved?.(saved);
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));

  const setRoute = (index: number, patch: Partial<Draft["routes"][number]>) =>
    setDraft((d) => ({
      ...d,
      routes: d.routes.map((r, i) => (i === index ? { ...r, ...patch } : r)),
    }));

  return (
    <form
      className="space-y-5"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="访问域名"
          hint={
            parentZone
              ? `父区自动推导为 ${parentZone}`
              : "对外提供服务的主机名, 如 img.example.com; 父区由它自动推导"
          }
        >
          <Input
            value={draft.hostname}
            onChange={(e) => set("hostname", e.target.value)}
            placeholder="img.example.com"
            required
          />
        </Field>
        <Field label="Cloudflare 凭据" hint="父区和 SaaS 区都用它">
          <CredSelect
            value={cfCredentialId}
            options={cfCreds}
            onChange={(v) => set("cfCredentialId", v)}
          />
        </Field>
        <Field label="SaaS 区" hint="承载自定义主机名的另一个 CF zone; 不走 CF 就选「不使用」">
          <Select
            value={draft.saasZone || "__none__"}
            onValueChange={(v) => set("saasZone", v === "__none__" ? "" : (v ?? ""))}
          >
            <SelectTrigger className="w-full">
              <SelectValue>
                {(v) => (!v || String(v) === "__none__" ? "不使用 CF for SaaS" : String(v))}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">不使用 CF for SaaS</SelectItem>
              {/* 父区排除掉: 自定义主机名不能是 SaaS 区自己的子域 */}
              {(cfZones ?? [])
                .filter((z) => z !== parentZone)
                .map((z) => (
                  <SelectItem key={z} value={z}>
                    {z}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="DNSPod 凭据">
          <CredSelect
            value={dnspodCredentialId}
            options={dpCreds}
            onChange={(v) => set("dnspodCredentialId", v)}
          />
        </Field>
        <Field label="备注">
          <Input value={draft.note} onChange={(e) => set("note", e.target.value)} />
        </Field>
      </div>

      <div className="flex items-center gap-3">
        <Switch
          id="enabled"
          checked={draft.enabled}
          onCheckedChange={(v) => set("enabled", v)}
        />
        <Label htmlFor="enabled">参与巡检</Label>
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-sm font-medium">线路落点</h3>
            <p className="text-muted-foreground text-xs">
              「默认」线是兜底, 建议把覆盖面最广的那条挂在这里
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              setDraft((d) => ({ ...d, routes: [...d.routes, { ...EMPTY_ROUTE, line: "" }] }))
            }
          >
            加一条
          </Button>
        </div>

        {draft.routes.map((route, i) => (
          <div key={i} className="border-border/60 space-y-3 rounded-lg border p-3">
            <div className="flex flex-wrap items-end gap-2">
              <div className="w-28">
                <Label className="text-xs">线路</Label>
                <Input
                  list="dnspod-lines"
                  value={route.line}
                  onChange={(e) => setRoute(i, { line: e.target.value })}
                  placeholder="默认"
                  className="mt-1"
                />
              </div>
              <div className="min-w-48 flex-1">
                <Label className="text-xs">落点</Label>
                <Select
                  value={String(route.originId)}
                  onValueChange={(v) => setRoute(i, { originId: Number(v ?? 0) })}
                >
                  <SelectTrigger className="mt-1 w-full">
                    {/* Base UI 的 Value 默认渲染 value 本身, 要自己把它映射成看得懂的名字 */}
                    <SelectValue>
                      {(v) => {
                        if (!v || String(v) === "0") return "直接填写 (不进回源库)";
                        const o = (origins ?? []).find((x) => String(x.id) === String(v));
                        return o ? `${o.name} · ${o.value}` : "选择回源";
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">直接填写 (不进回源库)</SelectItem>
                    {(origins ?? []).map((o) => (
                      <SelectItem key={o.id} value={String(o.id)}>
                        {o.name} · {o.value}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() =>
                  setDraft((d) => ({ ...d, routes: d.routes.filter((_, idx) => idx !== i) }))
                }
              >
                删除
              </Button>
            </div>

            {/* 内联落点: EdgeOne 的 CNAME、一次性的直连 IP 这类不会复用的目标, 不必先去回源库建条目 */}
            {route.originId === 0 && (
              <div className="grid gap-2 sm:grid-cols-2">
                <div>
                  <Label className="text-xs">类型</Label>
                  <Select
                    value={route.kind}
                    onValueChange={(v) => setRoute(i, { kind: v ?? route.kind })}
                  >
                    <SelectTrigger className="mt-1 w-full">
                      <SelectValue>
                        {(v) =>
                          ORIGIN_KINDS.find((k) => k.value === String(v))?.label ?? String(v ?? "")
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {ORIGIN_KINDS.map((k) => (
                        <SelectItem key={k.value} value={k.value}>
                          {k.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div>
                  <Label className="text-xs">落点值</Label>
                  <Input
                    value={route.value}
                    onChange={(e) => setRoute(i, { value: e.target.value })}
                    placeholder={
                      route.kind === "ip" ? "203.0.113.10" : "img.example.com.eo.dnse2.com"
                    }
                    className="mt-1"
                  />
                </div>
                {route.kind === "saas_fallback" && (
                  <div>
                    <Label className="text-xs">源站 IP</Label>
                    <Input
                      value={route.address}
                      onChange={(e) => setRoute(i, { address: e.target.value })}
                      placeholder="仅当要程序代建那条橙云记录时才填"
                      className="mt-1"
                    />
                  </div>
                )}
                {route.kind === "saas_custom" && (
                  <div>
                    <Label className="text-xs">SNI</Label>
                    <Input
                      value={route.sni}
                      onChange={(e) => setRoute(i, { sni: e.target.value })}
                      placeholder="留空则与落点值相同"
                      className="mt-1"
                    />
                  </div>
                )}
              </div>
            )}
          </div>
        ))}

        <datalist id="dnspod-lines">
          {COMMON_LINES.map((l) => (
            <option key={l} value={l} />
          ))}
        </datalist>
        <datalist id="cf-zones">
          {(cfZones ?? []).map((z) => (
            <option key={z} value={z} />
          ))}
        </datalist>
        <datalist id="dnspod-domains">
          {(dpDomains ?? []).map((d) => (
            <option key={d} value={d} />
          ))}
        </datalist>
      </div>

      <Button type="submit" disabled={save.isPending}>
        {save.isPending ? "保存中…" : "保存"}
      </Button>
    </form>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      {children}
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  );
}

function CredSelect({
  value,
  options,
  onChange,
  allowEmpty,
}: {
  value: number;
  options: Credential[];
  onChange: (v: number) => void;
  allowEmpty?: boolean;
}) {
  return (
    <Select
      value={value ? String(value) : allowEmpty ? "0" : ""}
      onValueChange={(v) => onChange(Number(v ?? 0))}
    >
      <SelectTrigger className="w-full">
        <SelectValue placeholder="选择凭据">
          {(v) => {
            if (allowEmpty && (!v || String(v) === "0")) return "与父区相同";
            const c = options.find((o) => String(o.id) === String(v));
            return c ? c.name : "选择凭据";
          }}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {allowEmpty && <SelectItem value="0">与父区相同</SelectItem>}
        {options.map((c) => (
          <SelectItem key={c.id} value={String(c.id)}>
            {c.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
