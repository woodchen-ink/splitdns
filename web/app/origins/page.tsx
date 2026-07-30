"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, postWithMessage } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { CFZone, Origin, OriginDNS } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { LevelBadge } from "@/components/level-badge";
import { HostPicker, splitHost } from "@/components/host-picker";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// 回源类型与它们的说明。后端按"有没有独立 SNI"判定是否需要源站补路由,
// 这里的取值只影响表单提示, 新增类型不会让页面失灵。
const KINDS = [
  {
    value: "saas_fallback",
    label: "CF SaaS 落点",
    hint: "DNS 那条 CNAME 的值, 决定流量怎么进 CF。填 SaaS 区里任意一条橙云记录的主机名; 留空则用该区当前的回退源",
  },
  {
    value: "saas_custom",
    label: "CF SaaS 自定义源",
    hint: "不是解析目标! 它配在自定义主机名上, 决定流量进了 CF 之后往哪台机器转。只有该域名要回源到另一台机器时才需要, 且源站必须能路由这个 SNI, 否则 403",
  },
  { value: "cname", label: "第三方 CDN CNAME", hint: "如 EdgeOne 给的加速 CNAME" },
  { value: "ip", label: "直连源站 IP", hint: "解析直接落到这个 IP, 不过任何 CDN" },
];

const EMPTY: Origin = { id: 0, name: "", kind: "cname", value: "", address: "", sni: "", note: "" };

// 回源管理页。一个回源可被多个访问域名共用, 改一处全部跟着变。
export default function OriginsPage() {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<Origin>(EMPTY);
  // 落点后缀选不到时退回手打整串, 由这个开关记住, 不靠"当前值匹不匹配得上"反推 ——
  // 那样用户刚清空前缀就会被弹回选择模式
  const [manualHost, setManualHost] = useState(false);

  // CF 上可见的 zone, 不限定凭据 —— 回源不绑账号, 后缀可能落在任意一个 CF 账号下
  const zones = useQuery({
    queryKey: queryKeys.cfZones(0),
    queryFn: () => api.get<CFZone[] | null>("/api/discover/cf-zones"),
    retry: false,
  });
  const zoneList = (zones.data ?? []).map((z) => z.zone);

  const { data } = useQuery({
    queryKey: queryKeys.origins(),
    queryFn: () => api.get<Origin[] | null>("/api/origins"),
  });

  // 落点在 CF 上有没有真的建成橙云记录, 每次进页面 / 每次改完回源都自动查一遍。
  // 走的是三个平台的实时接口, 比列表慢, 所以单独一条 query, 不拖着列表一起转圈
  const dns = useQuery({
    queryKey: queryKeys.originDns(),
    queryFn: () => api.get<OriginDNS[] | null>("/api/origins/dns"),
    retry: false,
  });
  const dnsOf = (id: number) => (dns.data ?? []).find((d) => d.originId === id);

  const save = useMutation({
    mutationFn: () => api.post<Origin>("/api/origins", draft),
    onSuccess: () => {
      toast.success("已保存");
      resetDraft();
      qc.invalidateQueries({ queryKey: queryKeys.origins() });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const createRecord = useMutation({
    mutationFn: (id: number) => postWithMessage(`/api/origins/${id}/dns`),
    onSuccess: ({ msg }) => {
      toast.success(msg);
      qc.invalidateQueries({ queryKey: queryKeys.originDns() });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/api/origins/${id}`),
    onSuccess: () => {
      toast.success("已删除");
      qc.invalidateQueries({ queryKey: queryKeys.origins() });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const kind = KINDS.find((k) => k.value === draft.kind);
  const isCustomOrigin = draft.kind === "saas_custom";

  // 自定义源的回源 SNI 就是源服务器主机名本身, 跟着落点值走, 不让两处各写各的
  const edit = (next: Origin) =>
    setDraft(next.kind === "saas_custom" ? { ...next, sni: next.value } : next);

  // 编辑已有回源时, 后缀在 CF 里找不到就直接进手打模式
  const startEdit = (o: Origin) => {
    setDraft(o);
    setManualHost(splitHost(o.value, zoneList).zone === "");
  };

  const resetDraft = () => {
    setDraft(EMPTY);
    setManualHost(false);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">回源</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          线路最终指向的目标。同一个回源可以被多个访问域名引用。
        </p>
      </div>

      <form
        className="border-border/60 space-y-4 rounded-xl border p-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <h2 className="text-sm font-medium">{draft.id ? "编辑回源" : "新增回源"}</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label>名称</Label>
            <Input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              placeholder="海外源站"
              required
            />
          </div>
          <div className="space-y-1.5">
            <Label>类型</Label>
            <Select value={draft.kind} onValueChange={(v) => edit({ ...draft, kind: v ?? draft.kind })}>
              <SelectTrigger className="w-full">
                <SelectValue>
                  {(v) => KINDS.find((k) => k.value === String(v))?.label ?? String(v ?? "")}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {KINDS.map((k) => (
                  <SelectItem key={k.value} value={k.value}>
                    {k.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {kind && <p className="text-muted-foreground text-xs">{kind.hint}</p>}
          </div>
          <div className="space-y-1.5">
            <Label>落点值</Label>
            <HostPicker
              value={draft.value}
              onChange={(value) => edit({ ...draft, value })}
              zones={zoneList}
              manual={manualHost}
              onManualChange={setManualHost}
              prefixPlaceholder="rn-22dc3"
              fullPlaceholder="origin.mycdn.net"
              required
            />
            {draft.value && (
              <p className="text-muted-foreground font-mono text-xs break-all">{draft.value}</p>
            )}
            {zones.isError && (
              <p className="text-muted-foreground text-xs">
                没能读到 CF 上的域名列表, 只能手打: {(zones.error as Error).message}
              </p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label>源站 IP</Label>
            <Input
              value={draft.address}
              onChange={(e) => setDraft({ ...draft, address: e.target.value })}
              placeholder="192.0.2.1, 2001:db8::1"
            />
            <p className="text-muted-foreground text-xs">
              落点是主机名时, CF 里必须有一条指向它的橙云记录, 流量才转得出去。填上源站
              IP, 保存后就能在下面一键建出来。双栈用逗号分隔填两个, IPv4 建 A、IPv6 建 AAAA
            </p>
          </div>
          <div className="space-y-1.5">
            <Label>SNI</Label>
            <Input
              value={isCustomOrigin ? draft.value : draft.sni}
              onChange={(e) => setDraft({ ...draft, sni: e.target.value })}
              placeholder="自定义源服务器才需要"
              readOnly={isCustomOrigin}
              className={isCustomOrigin ? "text-muted-foreground" : undefined}
            />
            {isCustomOrigin && (
              <p className="text-muted-foreground text-xs">
                自定义源的回源 SNI 就是落点值本身, CF 默认拿它握手; 单独指定别的名字是企业版才有的能力
              </p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label>备注</Label>
            <Input
              value={draft.note}
              onChange={(e) => setDraft({ ...draft, note: e.target.value })}
            />
          </div>
        </div>
        <div className="flex gap-2">
          <Button type="submit" disabled={save.isPending}>
            保存
          </Button>
          {draft.id !== 0 && (
            <Button type="button" variant="ghost" onClick={resetDraft}>
              取消编辑
            </Button>
          )}
        </div>
      </form>

      <div className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-sm font-medium">已有回源</h2>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => dns.refetch()}
            disabled={dns.isFetching}
          >
            {dns.isFetching ? "检测中…" : "重新检测 CF 解析"}
          </Button>
        </div>
        {dns.error && (
          <p className="text-muted-foreground text-xs">
            没能检测 CF 上的解析: {(dns.error as Error).message}
          </p>
        )}

        {(data ?? []).map((o) => {
          const st = dnsOf(o.id);
          return (
            <div
              key={o.id}
              className="border-border/60 flex items-start justify-between gap-3 rounded-xl border p-4"
            >
              {/* flex-1 + min-w-0: 基准宽度归零, 否则那行很长的记录值会把整行占满, 把按钮挤到第二行 */}
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{o.name}</span>
                  <Badge variant="outline" className="font-normal">
                    {KINDS.find((k) => k.value === o.kind)?.label ?? o.kind}
                  </Badge>
                  {st?.message && <LevelBadge level={st.level}>CF 解析</LevelBadge>}
                </div>
                <p className="text-muted-foreground mt-1 font-mono text-xs break-all">
                  {o.value}
                  {o.sni && o.sni !== o.value ? ` · SNI ${o.sni}` : ""}
                  {o.address ? ` · ${o.address}` : ""}
                </p>
                {st?.message && <p className="mt-1.5 text-xs">{st.message}</p>}
                {(st?.actual || st?.expect) && (
                  <p className="text-muted-foreground mt-1 font-mono text-xs break-all">
                    {st.zone} · {st.actual ? `现有 ${st.actual}` : `待建 ${st.expect}`}
                  </p>
                )}
              </div>
              <div className="flex shrink-0 gap-2">
                {st?.canCreate && (
                  <Button
                    size="sm"
                    onClick={() => createRecord.mutate(o.id)}
                    disabled={createRecord.isPending}
                  >
                    {/* 只有正在建的那一行显示进行中, 否则一点下去整列都变"创建中" */}
                    {createRecord.isPending && createRecord.variables === o.id
                      ? "创建中…"
                      : "建解析"}
                  </Button>
                )}
                <Button variant="outline" size="sm" onClick={() => startEdit(o)}>
                  编辑
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => remove.mutate(o.id)}
                  disabled={remove.isPending}
                >
                  删除
                </Button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
