@echo off
rem NOTE: keep this file in GBK/ANSI (codepage 936). Saving it as UTF-8 breaks cmd.exe's
rem line parsing on the Chinese text below -- "chcp 65001" does not fix it, it makes it worse.
setlocal enabledelayedexpansion
cd /d "%~dp0"

echo ============================================
echo   splitdns 本地出包 (Windows 安装版 + 绿色版)
echo ============================================
echo.

rem ---- Go ----
where go >nul 2>nul
if errorlevel 1 (
    echo [x] 找不到 go。装一个 Go 1.25+ 再来: https://go.dev/dl/
    goto :fail
)
for /f "tokens=3" %%v in ('go version') do set "GOVER=%%v"
echo [1/5] Go        %GOVER%

rem ---- Node ----
where node >nul 2>nul
if errorlevel 1 (
    echo [x] 找不到 node。前端产物要靠它构建, 装 Node 22+ 再来: https://nodejs.org/
    goto :fail
)
for /f %%v in ('node --version') do set "NODEVER=%%v"
echo [2/5] Node      %NODEVER%

rem ---- wails: 不在 PATH 时到 GOPATH\bin 里找, 还没有就按 CI 的版本装一份 ----
for /f "delims=" %%p in ('go env GOPATH') do set "GOTOOLDIR=%%p\bin"
where wails >nul 2>nul
if errorlevel 1 (
    if not exist "!GOTOOLDIR!\wails.exe" (
        echo [3/5] wails     没装, 正在安装 v2.12.0 ...
        go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
        if errorlevel 1 (
            echo [x] wails 安装失败
            goto :fail
        )
    )
    set "PATH=!GOTOOLDIR!;%PATH%"
)
echo [3/5] wails     已就绪

rem ---- NSIS: 只有出安装版才需要, 装了不在 PATH 的情况很常见 ----
rem 变量名里带括号, 必须先在块外取出来, 否则 if 块会被 (x86) 的右括号提前截断
set "NSISDIR=%ProgramFiles(x86)%\NSIS"
if not exist "!NSISDIR!\makensis.exe" set "NSISDIR=%ProgramFiles%\NSIS"
where makensis >nul 2>nul
if errorlevel 1 (
    if not exist "!NSISDIR!\makensis.exe" (
        echo [x] 找不到 makensis, 出不了安装版。装 NSIS 再来, 二选一:
        echo        winget install NSIS.NSIS
        echo        choco install nsis -y
        goto :fail
    )
    set "PATH=!NSISDIR!;%PATH%"
)
echo [4/5] NSIS      已就绪

rem 前端由 wails 按 wails.json 里的 frontend:build 自己跑, 不要加 -s 跳过 ——
rem desktop/frontend/ 不进仓库, 跳过的话 wails 会塞一个占位 index.html, 编译能过但装出来是空壳
echo [5/5] 开始构建, 头一次要拉依赖, 慢一点是正常的
echo.
cd desktop
wails build -platform windows/amd64 -webview2 embed -skipbindings -nsis
if errorlevel 1 (
    cd ..
    echo.
    echo [x] 构建失败, 往上翻看具体报错
    goto :fail
)
cd ..

echo.
echo ============================================
echo   构建完成
echo ============================================
echo   安装版  desktop\build\bin\splitdns-amd64-installer.exe
echo   绿色版  desktop\build\bin\splitdns.exe
echo.
echo   两个用的是同一份二进制, 数据都在:
echo     %%LOCALAPPDATA%%\CZL\splitdns\data
echo   产物目录: %~dp0desktop\build\bin
echo.
pause
exit /b 0

:fail
echo.
pause
exit /b 1
