import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { Level } from "@/lib/types";

const LEVEL_STYLE: Record<string, { label: string; className: string }> = {
  ok: { label: "正常", className: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-400" },
  warn: { label: "提醒", className: "bg-amber-500/15 text-amber-700 dark:text-amber-400" },
  error: { label: "错误", className: "bg-red-500/12 text-red-700 dark:text-red-400" },
};

// LevelBadge 渲染严重程度。未登记的取值原样展示为中性样式,
// 后端新增级别不会让界面上凭空少一块。
// 传 children 可以换掉默认文案, 只借用配色 —— 结论本身够短时比"错误 + 另起一行说明"更好读。
export function LevelBadge({
  level,
  className,
  children,
}: {
  level: Level | string;
  className?: string;
  children?: ReactNode;
}) {
  const style = LEVEL_STYLE[level];
  return (
    <Badge
      variant="secondary"
      className={cn("border-0 font-medium", style?.className, className)}
    >
      {children ?? style?.label ?? level}
    </Badge>
  );
}
