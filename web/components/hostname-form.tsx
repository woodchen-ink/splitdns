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

type Draft = Omit<Hostname, "id" | "routes"> & {
  id: number;
  routes: Array<Pick<Route, "line" | "originId">>;
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
  routes: [{ line: "默认", originId: 0 }],
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
          routes: (initial.routes ?? []).map((r) => ({ line: r.line, originId: r.originId })),
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

  const save = useMutation({
    mutationFn: () => api.post<Hostname>("/api/hostnames", draft),
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
        <Field label="访问域名" hint="对外提供服务的主机名, 如 i.czl.net">
          <Input
            value={draft.hostname}
            onChange={(e) => set("hostname", e.target.value)}
            placeholder="i.czl.net"
            required
          />
        </Field>
        <Field label="父区" hint="主域名所在的 CF zone, 委派 NS 加在这里">
          <Input
            value={draft.parentZone}
            onChange={(e) => set("parentZone", e.target.value)}
            placeholder="czl.net"
            required
          />
        </Field>
        <Field label="SaaS 区" hint="承载自定义主机名的另一个 CF zone; 不走 CF 可留空">
          <Input
            value={draft.saasZone}
            onChange={(e) => set("saasZone", e.target.value)}
            placeholder="20200511.xyz"
          />
        </Field>
        <Field label="DNSPod 域名" hint="留空则与访问域名相同">
          <Input
            value={draft.dnspodDomain}
            onChange={(e) => set("dnspodDomain", e.target.value)}
            placeholder="i.czl.net"
          />
        </Field>
        <Field label="父区凭据">
          <CredSelect
            value={draft.cfCredentialId}
            options={cfCreds}
            onChange={(v) => set("cfCredentialId", v)}
          />
        </Field>
        <Field label="SaaS 区凭据" hint="留空表示与父区同账号">
          <CredSelect
            value={draft.saasCredentialId}
            options={cfCreds}
            onChange={(v) => set("saasCredentialId", v)}
            allowEmpty
          />
        </Field>
        <Field label="DNSPod 凭据">
          <CredSelect
            value={draft.dnspodCredentialId}
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
            onClick={() => setDraft((d) => ({ ...d, routes: [...d.routes, { line: "", originId: 0 }] }))}
          >
            加一条
          </Button>
        </div>

        {draft.routes.map((route, i) => (
          <div key={i} className="flex flex-wrap items-end gap-2">
            <div className="w-32">
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
              <Label className="text-xs">回源</Label>
              <Select
                value={route.originId ? String(route.originId) : ""}
                onValueChange={(v) => setRoute(i, { originId: Number(v ?? 0) })}
              >
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue placeholder="选择回源" />
                </SelectTrigger>
                <SelectContent>
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
        ))}
        <datalist id="dnspod-lines">
          {COMMON_LINES.map((l) => (
            <option key={l} value={l} />
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
        <SelectValue placeholder="选择凭据" />
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
