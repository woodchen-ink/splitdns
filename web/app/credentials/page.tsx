"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { queryKeys } from "@/lib/queries";
import type { Credential } from "@/lib/types";
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

const EMPTY: Credential = { id: 0, kind: "cloudflare", name: "", hasSecret: false };

// 凭据管理页。密钥只写不读: 后端永远不回传密钥字段, 编辑时留空表示不修改。
export default function CredentialsPage() {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<Credential>(EMPTY);

  const { data } = useQuery({
    queryKey: queryKeys.credentials(),
    queryFn: () => api.get<Credential[] | null>("/api/credentials"),
  });

  const save = useMutation({
    mutationFn: () => api.post<Credential>("/api/credentials", draft),
    onSuccess: () => {
      toast.success("已保存");
      setDraft(EMPTY);
      qc.invalidateQueries({ queryKey: queryKeys.credentials() });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const remove = useMutation({
    mutationFn: (id: number) => api.del(`/api/credentials/${id}`),
    onSuccess: () => {
      toast.success("已删除");
      qc.invalidateQueries({ queryKey: queryKeys.credentials() });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const isCF = draft.kind === "cloudflare";

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">凭据</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          调用 Cloudflare 与腾讯云 DNSPod 用的密钥。保存后不会再回显, 只能覆盖。
        </p>
      </div>

      <form
        className="border-border/60 space-y-4 rounded-xl border p-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <h2 className="text-sm font-medium">{draft.id ? `编辑「${draft.name}」` : "新增凭据"}</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label>平台</Label>
            <Select
              value={draft.kind}
              onValueChange={(v) => setDraft({ ...draft, kind: v ?? draft.kind })}
              disabled={draft.id !== 0}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="cloudflare">Cloudflare</SelectItem>
                <SelectItem value="dnspod">腾讯云 DNSPod</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>备注名</Label>
            <Input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              placeholder={isCF ? "CF 主账号" : "腾讯云 wood"}
              required
            />
          </div>

          {isCF ? (
            <div className="space-y-1.5 sm:col-span-2">
              <Label>API Token</Label>
              <Input
                type="password"
                autoComplete="off"
                value={draft.apiToken ?? ""}
                onChange={(e) => setDraft({ ...draft, apiToken: e.target.value })}
                placeholder={draft.id ? "留空表示不修改" : "需要 Zone:DNS 编辑与 SSL 权限"}
              />
            </div>
          ) : (
            <>
              <div className="space-y-1.5">
                <Label>SecretId</Label>
                <Input
                  autoComplete="off"
                  value={draft.secretId ?? ""}
                  onChange={(e) => setDraft({ ...draft, secretId: e.target.value })}
                  placeholder={draft.id ? "留空表示不修改" : ""}
                />
              </div>
              <div className="space-y-1.5">
                <Label>SecretKey</Label>
                <Input
                  type="password"
                  autoComplete="off"
                  value={draft.secretKey ?? ""}
                  onChange={(e) => setDraft({ ...draft, secretKey: e.target.value })}
                  placeholder={draft.id ? "留空表示不修改" : ""}
                />
              </div>
            </>
          )}
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
        {(data ?? []).map((c) => (
          <div
            key={c.id}
            className="border-border/60 flex flex-wrap items-center justify-between gap-3 rounded-xl border p-4"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{c.name}</span>
              <Badge variant="outline" className="font-normal">
                {c.kind === "cloudflare" ? "Cloudflare" : "腾讯云 DNSPod"}
              </Badge>
              {!c.hasSecret && <Badge variant="secondary">未填密钥</Badge>}
            </div>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setDraft({ ...c, apiToken: "", secretId: "", secretKey: "" })}
              >
                编辑
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => remove.mutate(c.id)}
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
