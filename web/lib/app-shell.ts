// 客户端壳识别: 在 html 上打 data-app-shell, 文字选择的那套规则 (globals.css) 全部挂在这个标记下,
// 普通浏览器里打开时保持浏览器默认。
//
// 判据用 webview 自带的原生对象而不是 Wails 注入的 runtime: 后者是 Wails 往 HTML 里插的脚本,
// 和这段内联脚本谁先跑说不准; 原生对象在任何页面脚本之前就在。
// WebView2 (Windows) 有 chrome.webview, WKWebView / WebKitGTK 有 webkit.messageHandlers,
// 桌面版 Chrome / Safari 都没有。
export const appShellScript = `(function(){try{var w=window;if((w.chrome&&w.chrome.webview)||(w.webkit&&w.webkit.messageHandlers)){document.documentElement.setAttribute("data-app-shell","")}}catch(e){}})();`;

export function isAppShell(): boolean {
  return document.documentElement.hasAttribute("data-app-shell");
}
