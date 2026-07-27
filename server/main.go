package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/woodchen-ink/splitdns/server/initapp"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/router"
	"github.com/woodchen-ink/splitdns/server/service"
)

// main 同一个二进制既是网页服务也是 CLI:
// 不带子命令起 HTTP 服务, 带子命令跑一次性任务后退出。
func main() {
	cfg, err := initapp.Init()
	if err != nil {
		slog.Error("启动失败", "err", err)
		os.Exit(1)
	}

	args := os.Args[1:]
	if len(args) > 0 {
		if err := runCommand(args); err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		return
	}

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           router.New(cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("splitdns 启动", "addr", cfg.Addr(), "access", cfg.AccessEnabled())
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("服务退出", "err", err)
		os.Exit(1)
	}
}

// runCommand 分派 CLI 子命令。
func runCommand(args []string) error {
	switch args[0] {
	case "check":
		return cmdCheck(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	}
	printUsage()
	return fmt.Errorf("未知命令 %s", args[0])
}

// cmdCheck 巡检域名并把结果打到终端。不带参数时检查全部启用的域名。
func cmdCheck(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	targets, err := resolveTargets(args)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Println("没有可巡检的域名")
		return nil
	}

	worst := 0
	for _, h := range targets {
		report := service.Inspect(ctx, h)
		fmt.Printf("\n%s  [%s] %s\n", report.Hostname, strings.ToUpper(string(report.Level)), report.Summary)
		for _, f := range report.Findings {
			fmt.Printf("  %-5s %-28s %s\n", f.Level, f.Code, f.Title)
			if f.Detail != "" {
				fmt.Printf("        %s\n", f.Detail)
			}
			if f.Fix != "" {
				fmt.Printf("        → %s\n", f.Fix)
			}
		}
		if r := report.Level.Rank(); r > worst {
			worst = r
		}
	}

	fmt.Println()
	if worst >= 2 {
		return fmt.Errorf("存在需要处理的错误")
	}
	return nil
}

// resolveTargets 把命令行参数解析成待巡检的域名列表。
// 参数可以是域名 ID 也可以是域名本身, 不给参数则取全部启用的域名。
func resolveTargets(args []string) ([]model.Hostname, error) {
	list, _, err := service.ListHostnames("", 0, 200)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		var out []model.Hostname
		for _, h := range list {
			if h.Enabled {
				out = append(out, h)
			}
		}
		return out, nil
	}

	want := map[string]bool{}
	for _, a := range args {
		want[strings.ToLower(a)] = true
	}
	var out []model.Hostname
	for _, h := range list {
		if want[strings.ToLower(h.Hostname)] || want[strconv.FormatUint(uint64(h.ID), 10)] {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没找到匹配的域名: %s", strings.Join(args, ", "))
	}
	return out, nil
}

func printUsage() {
	fmt.Println(`splitdns —— 分线路解析配置与巡检

用法:
  splitdns              启动网页服务
  splitdns check        巡检全部启用的域名
  splitdns check <域名|ID>...   只巡检指定域名

配置全部走环境变量, 见 README。`)
}
