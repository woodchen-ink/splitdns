"use client";

import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";

// 数据搬家页: 服务端转桌面版、换机器、日常备份, 都靠这一对。
export default function DataPage() {
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [pending, setPending] = useState<File | null>(null);

  const importDb = useMutation({
    mutationFn: async (file: File) => {
      const form = new FormData();
      form.append("file", file);
      const resp = await fetch("/api/import/db", { method: "POST", body: form });
      const body = await resp.json();
      if (body.code !== 200) {
        throw new ApiError(body.code, body.msg || "导入失败");
      }
      return body.msg as string;
    },
    onSuccess: (msg) => {
      setPending(null);
      if (fileRef.current) {
        fileRef.current.value = "";
      }
      // 整个库都换了, 手里的缓存一律作废
      qc.clear();
      toast.success(msg);
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">数据</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          备份、换机器、服务端与桌面版之间搬家，都用这里。
        </p>
      </div>

      <section className="border-border/60 space-y-3 rounded-xl border p-4">
        <h2 className="text-sm font-medium">导出</h2>
        <p className="text-muted-foreground text-sm">
          下载一份当前数据库的快照。导出用的是一致性快照，不会因为正好有写入而拿到半截数据。
        </p>
        <p className="text-sm">
          文件里有<strong>明文的平台密钥</strong>，请当作密钥文件对待，别随手丢共享盘。
        </p>
        <a
          href="/api/export/db"
          className="bg-primary text-primary-foreground hover:bg-primary/90 inline-flex h-9 items-center rounded-md px-4 text-sm font-medium transition-colors"
        >
          下载数据库
        </a>
      </section>

      <Separator />

      <section className="border-border/60 space-y-3 rounded-xl border p-4">
        <h2 className="text-sm font-medium">导入</h2>
        <p className="text-muted-foreground text-sm">
          用导出的文件<strong>整体替换</strong>当前数据——现有的域名、回源、凭据、流程全部被覆盖。
          替换前会自动把现有数据另存一份到数据目录，导错了还能换回来。
        </p>

        <input
          ref={fileRef}
          type="file"
          accept=".db"
          onChange={(e) => setPending(e.target.files?.[0] ?? null)}
          className="text-sm file:mr-3 file:rounded-md file:border-0 file:bg-secondary file:px-3 file:py-1.5 file:text-sm file:font-medium"
        />

        {pending && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-3">
            <p className="text-sm">
              确认用 <span className="font-mono">{pending.name}</span> 替换当前全部数据？
            </p>
            <div className="mt-3 flex gap-2">
              <Button
                size="sm"
                onClick={() => importDb.mutate(pending)}
                disabled={importDb.isPending}
              >
                {importDb.isPending ? "导入中…" : "确认导入"}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setPending(null);
                  if (fileRef.current) {
                    fileRef.current.value = "";
                  }
                }}
              >
                取消
              </Button>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}
