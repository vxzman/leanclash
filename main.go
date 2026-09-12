package main

import (
	"embed"
	"fmt"
	"os"
	"runtime"

	"leanclash/internal/cli"
	"leanclash/internal/server"
)

// 前端构建产物随二进制分发：部署 = 复制一个文件。
// web/dist 下保留占位 index.html，未执行 npm run build 时也能 go build。
//
//go:embed all:web/dist
var webFS embed.FS

// 构建信息：go build -ldflags 注入（见 README「本地构建」），
// 未注入时显示默认值；部署后用 leanclash info 核对是否为最新构建。
var (
	version   = "dev"     // 语义版本，-X main.version=v1.0.0
	commit    = "unknown" // git 提交，-X main.commit=<hash>
	buildTime = "unknown" // 编译时间（UTC），-X main.buildTime=2026-08-27T12:00:00Z
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	switch args[0] {
	case "serve":
		// 守护进程（systemd 服务入口）
		if err := server.RunDaemon(webFS); err != nil {
			fmt.Fprintf(os.Stderr, "leanclash serve 失败: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := cli.Status(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "status 失败: %v\n", err)
			os.Exit(1)
		}
	case "info":
		// 本地输出构建信息，无需守护进程
		printBuildInfo()
	case "config":
		if len(args) < 2 || args[1] != "sync" {
			usage()
			os.Exit(2)
		}
		if err := cli.ConfigSync(); err != nil {
			fmt.Fprintf(os.Stderr, "config sync 失败: %v\n", err)
			os.Exit(1)
		}
	default:
		// 模式操作：leanclash <mode> start|stop
		if len(args) < 2 {
			usage()
			os.Exit(2)
		}
		if err := cli.ModeOp(args[0], args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "%s %s 失败: %v\n", args[0], args[1], err)
			os.Exit(1)
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `leanclash — Mihomo 运行模式管理

用法:
  leanclash serve                      启动守护进程（systemd 服务入口）
  leanclash <mode> start|stop          启停模式（tun|socks|tproxy|redir-tproxy）
  leanclash status [--json]            展示各模式/单元/规则状态
  leanclash config sync                同步各模式配置文件
  leanclash info                       查看版本/编译时间/目标平台

提示: 模式操作与 config sync 均经本机守护进程执行，请先确保
      systemctl start leanclash 已运行。
`)
}

// printBuildInfo 输出版本/编译信息，用于确认部署的二进制是否为最新构建。
func printBuildInfo() {
	fmt.Printf(`leanclash 构建信息
  版本:     %s
  提交:     %s
  编译时间: %s
  目标平台: %s/%s
  go 版本:  %s
`, version, commit, buildTime, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
