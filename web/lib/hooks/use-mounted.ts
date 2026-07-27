"use client";

import { useSyncExternalStore } from "react";

const subscribe = () => () => {};

// useMounted 判断当前是否已经在浏览器端。
// 静态导出的页面在构建期和浏览器端拿到的 URL 不同, 依赖真实 URL 的判定必须等挂载后再做,
// 否则首屏 HTML 与水合结果对不上。用 useSyncExternalStore 而不是 effect + setState,
// 后者会被 react-hooks/set-state-in-effect 规则拦下。
export function useMounted(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
}
