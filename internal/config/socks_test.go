package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// socks 入站由 env.socks_port 驱动：验证老 preset 迁移、默认值与生成结果。

func TestSocksPortMigrationFromPreset(t *testing.T) {
	cfg := Default()
	socks := cfg.Modes["socks"]
	// 模拟老版本 manager.yaml：端口藏在 preset 里，env 为空
	socks.Env = nil
	socks.Preset = "  - name: mixed-in\n    type: mixed\n    port: 25000\n    listen: 0.0.0.0\n    udp: true\n"
	fillDefaults(cfg)
	if socks.Env == nil || socks.Env.SocksPort != 25000 {
		t.Fatalf("迁移未提取端口: %+v", socks.Env)
	}
	if socks.Preset != "" {
		t.Fatalf("迁移后 socks preset 应为空，实际: %q", socks.Preset)
	}
}

func TestSocksPortKeptWhenSet(t *testing.T) {
	cfg := Default()
	cfg.Modes["socks"].Env.SocksPort = 26000
	fillDefaults(cfg)
	if got := cfg.Modes["socks"].Env.SocksPort; got != 26000 {
		t.Fatalf("已有端口被覆盖: %d", got)
	}
}

func TestSyncSocksGeneratedFromPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIHOMO_MANAGER_CONFIG", filepath.Join(dir, "manager.yaml"))
	cfg := Default()
	cfg.Dirs.ConfigDir = filepath.Join(dir, "etc")
	cfg.Modes["socks"].Env.SocksPort = 26000
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.GeneralPath(), []byte("mode: rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SyncAll(cfg, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.ConfigDir(), "config_socks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "port: 26000") {
		t.Fatalf("生成的 socks 配置未包含端口 26000:\n%s", data)
	}
}

func TestSyncTproxyAndRedirPortsFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEANCLASH_CONFIG", filepath.Join(dir, "manager.yaml"))
	cfg := Default()
	cfg.Dirs.ConfigDir = filepath.Join(dir, "etc")
	cfg.Modes["tproxy"].Env.TproxyPort = 33016
	cfg.Modes["redir-tproxy"].Env.TproxyPort = 33016
	cfg.Modes["redir-tproxy"].Env.RedirectPort = 33017

	if err := os.MkdirAll(cfg.ConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.GeneralPath(), []byte("mode: rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SyncAll(cfg, ""); err != nil {
		t.Fatal(err)
	}

	tpData, err := os.ReadFile(filepath.Join(cfg.ConfigDir(), "config_tproxy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tpData), "port: 33016") {
		t.Fatalf("生成的 tproxy 配置未更新端口 33016:\n%s", tpData)
	}

	redirData, err := os.ReadFile(filepath.Join(cfg.ConfigDir(), "config_redir-tproxy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(redirData), "port: 33016") || !strings.Contains(string(redirData), "port: 33017") {
		t.Fatalf("生成的 redir-tproxy 配置未更新端口:\n%s", redirData)
	}
}

func TestDefaultModesAreTheFourManagedOnes(t *testing.T) {
	cfg := Default()
	if _, ok := cfg.Modes["server"]; ok {
		t.Fatal("Default() 不应再包含 server 模式")
	}
	if got, want := len(cfg.Modes), len(ManagedModes); got != want {
		t.Fatalf("Default() 模式数 = %d, want %d", got, want)
	}
	for _, name := range ManagedModes {
		if cfg.Modes[name] == nil {
			t.Fatalf("Default() 缺少模式 %s", name)
		}
	}
}

func TestFillDefaultsDropsServerMode(t *testing.T) {
	cfg := Default()
	cfg.Modes["server"] = &Mode{
		Label:  "SERVER",
		Unit:   "mihomo@server",
		Config: "config_server.yaml",
	}
	fillDefaults(cfg)
	if _, ok := cfg.Modes["server"]; ok {
		t.Fatal("fillDefaults 应移除已废弃的 server 模式")
	}
}

func TestConfigPathPrecedence(t *testing.T) {
	t.Setenv("LEANCLASH_CONFIG", "/custom/leanclash.yaml")
	t.Setenv("MIHOMO_MANAGER_CONFIG", "/custom/mihomo.yaml")
	if got := Path(); got != "/custom/leanclash.yaml" {
		t.Fatalf("Path() = %q, want /custom/leanclash.yaml", got)
	}

	t.Setenv("LEANCLASH_CONFIG", "")
	if got := Path(); got != "/custom/mihomo.yaml" {
		t.Fatalf("Path() = %q, want /custom/mihomo.yaml", got)
	}
}
