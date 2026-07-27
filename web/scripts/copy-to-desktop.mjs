// 把静态导出产物拷进 desktop/frontend/dist —— Go 的 embed 不能引用模块目录之外的文件,
// 所以桌面版要把产物先搬进自己的模块里才能嵌进二进制。
import { cp, rm, mkdir, access } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const src = resolve(webDir, "out");
const dest = resolve(webDir, "..", "desktop", "frontend", "dist");

try {
  await access(src);
} catch {
  console.error(`找不到导出产物 ${src}, 先跑 npm run build`);
  process.exit(1);
}

await rm(dest, { recursive: true, force: true });
await mkdir(dirname(dest), { recursive: true });
await cp(src, dest, { recursive: true });

console.log(`已同步前端产物到 ${dest}`);
