// Package server 装配守护进程：manager.yaml 加载、服务管理器连接、
// lifecycle 编排、REST/SSE（TCP）与 CLI（Unix socket）双监听。
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"leanclash/internal/api"
	"leanclash/internal/config"
	"leanclash/internal/lifecycle"
)

func RunDaemon(webFS embed.FS) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	sys, err := newServiceManager(cfg)
	if err != nil {
		return err
	}
	defer sys.Close()

	lc := lifecycle.New(cfg, sys)
	handler, hub := api.New(cfg, lc, webFS)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 启动自愈：通用配置存在则同步各模式配置（首装/修复场景）。
	if _, err := os.Stat(cfg.GeneralPath()); err == nil {
		if err := config.SyncAll(cfg, ""); err != nil {
			log.Printf("[daemon] 启动配置同步失败: %v", err)
		}
	}

	// lifecycle 守护循环：服务状态同生共死 + tun0 事件 + 定期 reconcile。
	go func() {
		if err := lc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[daemon] lifecycle 循环退出: %v", err)
		}
	}()

	// SSE 状态定时推送（2s），操作后的即时推送在 api 内部完成。
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				api.PublishStatus(hub, lc)
			}
		}
	}()

	// ─── TCP：Web 前端 + REST/SSE ───
	webLn, err := net.Listen("tcp", cfg.Daemon.WebAddr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", cfg.Daemon.WebAddr, err)
	}
	log.Printf("LeanClash Web 面板监听: %s", cfg.Daemon.WebAddr)
	webSrv := &http.Server{Handler: handler}
	go func() {
		if err := webSrv.Serve(webLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[daemon] Web 服务退出: %v", err)
		}
	}()

	// ─── Unix socket：CLI 接口 ───
	cliLn, err := listenUnix(cfg.Daemon.CliSocket)
	if err != nil {
		return fmt.Errorf("监听 CLI socket 失败: %w", err)
	}
	defer os.Remove(cfg.Daemon.CliSocket)
	log.Printf("CLI socket: %s", cfg.Daemon.CliSocket)

	cliErr := make(chan error, 1)
	go func() {
		for {
			conn, err := cliLn.Accept()
			if err != nil {
				cliErr <- err
				return
			}
			go handleCLI(conn, lc, cfg)
		}
	}()

	select {
	case <-ctx.Done():
		log.Printf("[daemon] 收到退出信号，关闭")
	case err := <-cliErr:
		if !errors.Is(err, net.ErrClosed) {
			log.Printf("[daemon] CLI socket 退出: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = webSrv.Shutdown(shutdownCtx)
	_ = cliLn.Close()
	return nil
}

func listenUnix(path string) (net.Listener, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建 socket 目录失败: %w", err)
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// ─── CLI socket 协议 ─────────────────────────────────────────
// 每连接一个 JSON 请求/响应。action: start|stop|status|config-sync。

type cliRequest struct {
	Action string `json:"action"`
	Mode   string `json:"mode"`
}

type cliResponse struct {
	OK      bool            `json:"ok"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func handleCLI(conn net.Conn, lc *lifecycle.Manager, cfg *config.ManagerConfig) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(90 * time.Second))

	var req cliRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeCLI(conn, cliResponse{OK: false, Message: "请求解析失败: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var resp cliResponse
	switch req.Action {
	case "start":
		resp = doCLI(func() error { return lc.StartMode(ctx, req.Mode) },
			fmt.Sprintf("模式 %s 已启动", req.Mode))
	case "stop":
		resp = doCLI(func() error { return lc.StopMode(ctx, req.Mode) },
			fmt.Sprintf("模式 %s 已停止", req.Mode))
	case "status":
		data, _ := json.Marshal(lc.Status(ctx))
		resp = cliResponse{OK: true, Data: data}
	case "config-sync":
		resp = doCLI(func() error { return config.SyncAll(cfg, "") }, "配置同步完成")
	default:
		resp = cliResponse{OK: false, Message: "未知 action: " + req.Action}
	}
	writeCLI(conn, resp)
}

func doCLI(fn func() error, okMsg string) cliResponse {
	if err := fn(); err != nil {
		return cliResponse{OK: false, Message: err.Error()}
	}
	return cliResponse{OK: true, Message: okMsg}
}

func writeCLI(conn net.Conn, resp cliResponse) {
	_ = json.NewEncoder(conn).Encode(resp)
}
