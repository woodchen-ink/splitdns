"use client";

import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// MANUAL_ZONE 是"后缀不在 CF 里"这一档。第三方 CDN 给的 CNAME 就属于这种,
// 不给手打的余地会让这类值没法编辑。
const MANUAL_ZONE = "__manual__";

// splitHost 把主机名拆成 前缀 + CF 区。取能匹配上的最长后缀:
// example.com 与 sub.example.com 同时存在时, a.sub.example.com 属于更具体的那个。
export function splitHost(value: string, zones: string[]): { prefix: string; zone: string } {
  const host = value.trim();
  const lower = host.toLowerCase();
  let best = "";
  for (const z of zones) {
    if ((lower === z || lower.endsWith(`.${z}`)) && z.length > best.length) {
      best = z;
    }
  }
  if (!best) {
    return { prefix: value, zone: "" };
  }
  // 从原串上切, 不用小写过的那份 —— 否则边打字边被改成小写, 手感很怪
  return { prefix: lower === best ? "" : host.slice(0, -(best.length + 1)), zone: best };
}

// joinHost 把前缀与区拼回完整主机名, 前缀留空时就是区顶点本身。
function joinHost(prefix: string, zone: string): string {
  const p = prefix.trim().replace(/\.+$/, "");
  return p ? `${p}.${zone}` : zone;
}

// HostPicker 让人"打前缀 + 选 CF 域名后缀"来组主机名, 少打一截就少一类打错的可能。
//
// manual 由调用方持有: 后缀选不到时要退回手打整串, 靠"当前值匹不匹配得上"反推的话,
// 用户刚清空前缀就会被弹回选择模式。zones 为空 (没凭据 / 没读到) 时自动只剩手打。
export function HostPicker({
  value,
  onChange,
  zones,
  manual,
  onManualChange,
  prefixPlaceholder = "img",
  fullPlaceholder = "img.example.com",
  required,
}: {
  value: string;
  onChange: (value: string) => void;
  zones: string[];
  manual: boolean;
  onManualChange: (manual: boolean) => void;
  prefixPlaceholder?: string;
  fullPlaceholder?: string;
  required?: boolean;
}) {
  const { prefix, zone } = splitHost(value, zones);
  // 已有的值在 CF 里找不到对应后缀时也退回手打 —— 否则那串值会卡在禁用的前缀框里改不动
  const picking = zones.length > 0 && !manual && (value === "" || zone !== "");

  return (
    <div className="flex gap-2">
      <Input
        value={picking ? prefix : value}
        onChange={(e) => onChange(picking ? joinHost(e.target.value, zone) : e.target.value)}
        placeholder={picking ? prefixPlaceholder : fullPlaceholder}
        className="flex-1"
        // 还没选后缀时先别让人打前缀: 打了也拼不出完整主机名
        disabled={picking && !zone}
        required={required && !picking}
      />
      {zones.length > 0 && (
        <Select
          // 没选中时给空串而不是 undefined: 始终是受控的, 免得后面选上时 React 抱怨切换
          value={picking ? zone : MANUAL_ZONE}
          onValueChange={(v) => {
            const picked = String(v ?? "");
            if (picked === MANUAL_ZONE) {
              onManualChange(true);
              return;
            }
            onManualChange(false);
            onChange(joinHost(picking ? prefix : "", picked));
          }}
        >
          <SelectTrigger className="w-52 shrink-0">
            <SelectValue>
              {(v) => {
                const picked = String(v ?? "");
                if (!picked) return "选择域名";
                return picked === MANUAL_ZONE ? "手动输入" : `.${picked}`;
              }}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {zones.map((z) => (
              <SelectItem key={z} value={z}>
                .{z}
              </SelectItem>
            ))}
            <SelectItem value={MANUAL_ZONE}>手动输入完整主机名</SelectItem>
          </SelectContent>
        </Select>
      )}
    </div>
  );
}
