"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Origin } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
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
  { value: "saas_fallback", label: "CF SaaS 落点", hint: "填 SaaS 区里任意一条橙云记录的主机名; 留空则用该区当前的回退源" },
  { value: "saas_custom", label: "CF SaaS 自定义源", hint: "源站必须能路由这个 SNI, 否则回源 403" },
  { value: "cname", label: "第三方 CDN CNAME", hint: "如 EdgeOne 给的加速 CNAME" },
  { value: "ip", label: "直连源站 IP", hint: "解析直接落到这个 IP, 不过任何 CDN" },
];

const EMPTY: Origin = { id: 0, name: "", kind: "cname", value: "", address: "", sni: "", note: "" };

// 回源管理页。一个回源可被多个访问域名共用, 改一处全部跟着变。
export default function OriginsPage() {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<Origin>(EMPTY);

  const { data } = useQuery({
    queryKey: queryKeys.origins(),
    queryFn: () => api.get<Origin[] | null>("/api/origins"),
  });

  const save = useMutation({
    mutationFn: () => api.post<Origin>("/api/origins", draft),
    onSuccess: () => {
      toast.success("已保存");
      setDraft(EMPTY);
      qc.invalidateQueries({ queryKey: queryKeys.origins() });
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
              placeholder="KS-5 主力"
              required
            />
          </div>
          <div className="space-y-1.5">
            <Label>类型</Label>
            <Select value={draft.kind} onValueChange={(v) => setDraft({ ...draft, kind: v ?? draft.kind })}>
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
            <Input
              value={draft.value}
              onChange={(e) => setDraft({ ...draft, value: e.target.value })}
              placeholder="rs2000.20200511.xyz"
              required
            />
          </div>
          <div className="space-y-1.5">
            <Label>源站 IP</Label>
            <Input
              value={draft.address}
              onChange={(e) => setDraft({ ...draft, address: e.target.value })}
              placeholder="仅当需要程序替你建那条橙云记录时才填"
            />
          </div>
          <div className="space-y-1.5">
            <Label>SNI</Label>
            <Input
              value={draft.sni}
              onChange={(e) => setDraft({ ...draft, sni: e.target.value })}
              placeholder="自定义源服务器才需要"
            />
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
            <Button type="button" variant="ghost" onClick={() => setDraft(EMPTY)}>
              取消编辑
            </Button>
          )}
        </div>
      </form>

      <div className="space-y-2">
        {(data ?? []).map((o) => (
          <div
            key={o.id}
            className="border-border/60 flex flex-wrap items-center justify-between gap-3 rounded-xl border p-4"
          >
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{o.name}</span>
                <Badge variant="outline" className="font-normal">
                  {KINDS.find((k) => k.value === o.kind)?.label ?? o.kind}
                </Badge>
              </div>
              <p className="text-muted-foreground mt-1 font-mono text-xs break-all">
                {o.value}
                {o.sni && o.sni !== o.value ? ` · SNI ${o.sni}` : ""}
                {o.address ? ` · ${o.address}` : ""}
              </p>
            </div>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" onClick={() => setDraft(o)}>
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
        ))}
      </div>
    </div>
  );
}
