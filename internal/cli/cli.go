// Package cli 实现 leanclash 的 CLI 子命令：经 Unix socket 请求
// 守护进程执行操作（模式启停、状态、配置同步）。守护进程不在线时报错
// 并提示启动方式。
package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"text/tabwriter"
	"time"

	"leanclash/internal/config"
)

type request struct {
	Action string `json:"action"`
	Mode   string `json:"mode"`
}

type response struct {
	OK      bool            `json:"ok"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// socketPath 从 manager.yaml 读取 CLI socket 路径；读取失败退回默认值。
func socketPath() string {
	if cfg, err := config.Load(); err == nil {
		return cfg.Daemon.CliSocket
	}
	return config.Default().Daemon.CliSocket
}

func dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", socketPath(), 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("无法连接守护进程（%s）: %w\n提示: 请先执行 systemctl start leanclash", socketPath(), err)
	}
	return conn, nil
}

func call(req request) (*response, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(90 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("请求发送失败: %w", err)
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	return &resp, nil
}

// ModeOp 执行 leanclash <mode> start|stop。
func ModeOp(mode, action string) error {
	if action != "start" && action != "stop" {
		return fmt.Errorf("无效操作: %s（仅支持 start/stop）", action)
	}
	req := request{Action: action, Mode: mode}
	resp, err := call(req)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Message)
	}
	fmt.Println(resp.Message)
	return nil
}

// ConfigSync 请求守护进程执行配置同步。
func ConfigSync() error {
	resp, err := call(request{Action: "config-sync"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Message)
	}
	fmt.Println(resp.Message)
	return nil
}

// ─── Status ──────────────────────────────────────────────────

type statusData struct {
	Modes      map[string]modeStatus `json:"modes"`
	ActiveMode string                `json:"active_mode"`
}

type modeStatus struct {
	Label     string `json:"label"`
	Unit      string `json:"unit"`
	UnitState string `json:"unit_state"`
	Rules     string `json:"rules"`
	Active    bool   `json:"active"`
}

// Status 展示各模式/单元/规则状态，--json 输出原始 JSON。
func Status(args []string) error {
	resp, err := call(request{Action: "status"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Message)
	}

	for _, a := range args {
		if a == "--json" || a == "-j" {
			fmt.Println(string(resp.Data))
			return nil
		}
	}

	var st statusData
	if err := json.Unmarshal(resp.Data, &st); err != nil {
		return fmt.Errorf("状态解析失败: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "MODE\tLABEL\tUNIT\tUNIT STATE\tRULES")
	for _, name := range orderedModeNames(st.Modes) {
		m := st.Modes[name]
		mark := " "
		if m.Active {
			mark = "▶"
		}
		fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\t%s\n", mark, name, m.Label, m.Unit, m.UnitState, m.Rules)
	}
	w.Flush()

	if st.ActiveMode != "" {
		fmt.Printf("\n当前活跃模式: %s\n", st.ActiveMode)
	} else {
		fmt.Println("\n无活跃模式")
	}
	return nil
}

// orderedModeNames 按固定展示顺序排列模式名（tun、透明代理在前）。
func orderedModeNames(modes map[string]modeStatus) []string {
	preferred := config.ManagedModes
	var names []string
	seen := map[string]bool{}
	for _, p := range preferred {
		if _, ok := modes[p]; ok {
			names = append(names, p)
			seen[p] = true
		}
	}
	for name := range modes {
		if !seen[name] {
			names = append(names, name)
		}
	}
	return names
}
