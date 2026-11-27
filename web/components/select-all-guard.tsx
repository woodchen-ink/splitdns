"use client";

import { useEffect } from "react";
import { isAppShell } from "@/lib/app-shell";

// 可整块全选的内容: 代码块, 以及显式挂了 data-select-block 的区域
const BLOCK_SELECTOR = "pre, [data-select-block]";

// SelectAllGuard 在客户端壳里接管 Ctrl/⌘+A。
//
// 焦点在输入框 / 可编辑区时照常全选输入内容; 否则不让它选中整页 (侧栏、按钮、标题全蓝一片),
// 最近一次点击落在代码块里时只选中那一块, 其余情况什么都不选。浏览器里打开时不干预。
export function SelectAllGuard() {
  useEffect(() => {
    if (!isAppShell()) return;

    let lastBlock: Element | null = null;
    const onPointerDown = (e: PointerEvent) => {
      lastBlock = e.target instanceof Element ? e.target.closest(BLOCK_SELECTOR) : null;
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() !== "a" || !(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return;
      if (isEditable(document.activeElement)) return;

      e.preventDefault();
      const selection = window.getSelection();
      if (!selection) return;
      if (lastBlock?.isConnected) {
        selection.selectAllChildren(lastBlock);
      } else {
        selection.removeAllRanges();
      }
    };

    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  return null;
}

function isEditable(el: Element | null): boolean {
  if (!el) return false;
  if (el instanceof HTMLTextAreaElement) return true;
  if (el instanceof HTMLInputElement) {
    // 复选框、按钮之类的 input 没有可全选的文字
    return !["checkbox", "radio", "button", "submit", "reset", "file", "range", "color"].includes(el.type);
  }
  return el instanceof HTMLElement && el.isContentEditable;
}
