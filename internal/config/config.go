package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Path 返回 manager.yaml 的位置：LEANCLASH_CONFIG 优先，其次 MIHOMO_MANAGER_CONFIG，
// 默认 /opt/leanclash/manager.yaml。
func Path() string {
	if v := os.Getenv("LEANCLASH_CONFIG"); v != "" {
		return v
	}
	if v := os.Getenv("MIHOMO_MANAGER_CONFIG"); v != "" {
		return v
	}
	return DefaultConfigPath
}

// Load 读取 manager.yaml；文件不存在时先尝试从老布局迁移（/etc/mihomo 下的
// .conf），迁移不到就用内置默认值，并把结果落盘——保证首启即可用。
func Load() (*ManagerConfig, error) {
	path := Path()
	data, err := os.ReadFile(path)
	if err == nil {
		var cfg ManagerConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
		}
		fillDefaults(&cfg)
		return &cfg, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	cfg := Default()
	applyLegacyMigration(cfg)
	fillDefaults(cfg)
	if err := Save(cfg); err != nil {
		return nil, fmt.Errorf("生成默认配置 %s 失败: %w", path, err)
	}
	return cfg, nil
}

// Save 原子写入：临时文件 + rename，避免守护进程与 CLI 并发读到半截文件。
// manager.yaml 是机器管理文件，保存时会重排格式、丢弃手写注释。
func Save(cfg *ManagerConfig) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fillDefaults 补齐缺失字段，保证老版本/手改文件升级后结构完整。
func fillDefaults(cfg *ManagerConfig) {
	def := Default()
	if cfg.Dirs.ConfigDir == "" {
		cfg.Dirs.ConfigDir = def.Dirs.ConfigDir
	}
	if cfg.Dirs.DataDir == "" {
		cfg.Dirs.DataDir = def.Dirs.DataDir
	}
	if cfg.Daemon.WebAddr == "" {
		cfg.Daemon.WebAddr = def.Daemon.WebAddr
	}
	// 迁移旧默认值：":8081" 是双栈通配（含 IPv6），面板无鉴权不应默认暴露
	// IPv6——仅当配置仍是旧默认值时替换为 IPv4-only，用户自定义值不动。
	if cfg.Daemon.WebAddr == ":8081" {
		cfg.Daemon.WebAddr = def.Daemon.WebAddr
	}
	if cfg.Daemon.CliSocket == "" {
		cfg.Daemon.CliSocket = def.Daemon.CliSocket
	}
	if cfg.Daemon.ApplyDelayMs == 0 {
		cfg.Daemon.ApplyDelayMs = def.Daemon.ApplyDelayMs
	}
	if cfg.Daemon.ReconcileInterval == "" {
		cfg.Daemon.ReconcileInterval = def.Daemon.ReconcileInterval
	}
	if cfg.Modes != nil {
		// 旧版独立入站模式已移除，升级后不再作为可管理项出现。
		delete(cfg.Modes, "server")
	}
	for name, m := range cfg.Modes {
		if m == nil {
			cfg.Modes[name] = def.Modes[name]
			continue
		}
		if m.Label == "" {
			m.Label = name
		}
		if m.Unit == "" {
			m.Unit = "mihomo@" + name
		}
		if m.Config == "" {
			m.Config = "config_" + name + ".yaml"
		}
		// socks 入站由 env.socks_port 驱动：老版本端口藏在 preset 里，
		// 这里迁移取值并清掉 preset，端口成为唯一事实源。
		if name == "socks" {
			if m.Env == nil {
				m.Env = &Env{}
			}
			if m.Env.SocksPort <= 0 {
				m.Env.SocksPort = socksPortFromPreset(m.Preset)
			}
			m.Preset = ""
		}
	}
}

// socksPortFromPreset 从旧版 socks preset 的监听项中提取端口，取不到用默认值。
func socksPortFromPreset(preset string) int {
	if preset == "" {
		return defaultSocksPort
	}
	var doc struct {
		Listeners []map[string]interface{} `yaml:"listeners"`
	}
	if err := yaml.Unmarshal([]byte("listeners:\n"+preset), &doc); err != nil {
		return defaultSocksPort
	}
	for _, l := range doc.Listeners {
		if port, ok := l["port"].(int); ok && port > 0 {
			return port
		}
	}
	return defaultSocksPort
}

// Validate 检查结构完整性；tproxy/redir-tproxy 的回环避免必须 gid/mark 至少其一。
func Validate(cfg *ManagerConfig) error {
	if len(cfg.Modes) == 0 {
		return fmt.Errorf("modes 不能为空")
	}
	for name, m := range cfg.Modes {
		if m == nil {
			return fmt.Errorf("模式 %s 定义为空", name)
		}
		if m.Unit == "" || m.Config == "" {
			return fmt.Errorf("模式 %s 缺少 unit/config", name)
		}
		switch name {
		case "tproxy", "redir-tproxy":
			env := m.Env
			if env == nil {
				return fmt.Errorf("模式 %s 缺少 env", name)
			}
			if env.TproxyPort <= 0 || env.Fwmark <= 0 || env.TableID <= 0 || env.NftablesTable == "" {
				return fmt.Errorf("模式 %s env 不完整", name)
			}
			if name == "redir-tproxy" && env.RedirectPort <= 0 {
				return fmt.Errorf("模式 %s 缺少 redirect_port", name)
			}
			if env.ExcludeGID <= 0 && env.RoutingMark <= 0 {
				return fmt.Errorf("模式 %s 必须配置 exclude_gid 或 routing_mark 之一（回环避免）", name)
			}
		case "tun":
			if m.Routing == nil || m.Routing.TableIndex <= 0 {
				return fmt.Errorf("模式 tun 缺少 routing.table_index")
			}
		case "socks":
			if m.Env == nil || m.Env.SocksPort <= 0 {
				return fmt.Errorf("模式 socks 缺少 env.socks_port")
			}
		}
		if m.Preset != "" {
			if err := ValidatePreset(m.Preset); err != nil {
				return fmt.Errorf("模式 %s 预定义配置无效: %w", name, err)
			}
		}
	}
	return nil
}

// ConfigDir / DataDir 返回解析后的目录；相对路径以 manager.yaml 所在目录为基准
// （dev fixture 因此可写相对路径、可整体搬移）。
func (c *ManagerConfig) ConfigDir() string {
	return resolveAgainst(c.Dirs.ConfigDir, Path())
}

func (c *ManagerConfig) DataDir() string {
	return resolveAgainst(c.Dirs.DataDir, Path())
}

func resolveAgainst(dir, cfgPath string) string {
	if dir == "" || filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(filepath.Dir(cfgPath), dir)
}

// ─── 老布局迁移 ─────────────────────────────────────────────
// 从 /etc/mihomo/mihomo_tproxy.conf（脚本读 .conf）与 /etc/mihomo/mihomo_tproxy
// （旧面板读无后缀）迁移 env 值；.conf 优先。其余保持内置默认。

func applyLegacyMigration(cfg *ManagerConfig) {
	tproxyEnv := parseLegacyConf("/etc/mihomo/mihomo_tproxy.conf", "/etc/mihomo/mihomo_tproxy")
	mixEnv := parseLegacyConf("/etc/mihomo/mihomo_redir-tproxy.conf", "/etc/mihomo/mihomo_redir-tproxy")

	if e := tproxyEnv; e != nil {
		if m := cfg.Modes["tproxy"]; m != nil && m.Env != nil {
			m.Env.TproxyPort = e.TproxyPort
			m.Env.ExcludeGID = e.ExcludeGID
			m.Env.RoutingMark = e.RoutingMark
			m.Env.Fwmark = e.Fwmark
			m.Env.TableID = e.TableID
			m.Env.NftablesTable = e.NftablesTable
		}
	}
	if e := mixEnv; e != nil {
		if m := cfg.Modes["redir-tproxy"]; m != nil && m.Env != nil {
			m.Env.TproxyPort = e.TproxyPort
			m.Env.RedirectPort = e.RedirectPort
			m.Env.ExcludeGID = e.ExcludeGID
			m.Env.RoutingMark = e.RoutingMark
			m.Env.Fwmark = e.Fwmark
			m.Env.TableID = e.TableID
			m.Env.NftablesTable = e.NftablesTable
		}
	}
}

// parseLegacyConf 解析 KEY=VALUE 格式的旧 env 文件；全部缺失返回 nil。
func parseLegacyConf(paths ...string) *Env {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		env := &Env{}
		found := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			kv := strings.SplitN(line, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key, val := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
			switch key {
			case "TPROXY_PORT":
				env.TproxyPort, _ = strconv.Atoi(val)
			case "REDIRECT_PORT":
				env.RedirectPort, _ = strconv.Atoi(val)
			case "EXCLUDE_GID":
				env.ExcludeGID, _ = strconv.Atoi(val)
			case "ROUTING_MARK":
				env.RoutingMark, _ = strconv.Atoi(val)
			case "FWMARK":
				env.Fwmark, _ = strconv.Atoi(val)
			case "TABLE_ID":
				env.TableID, _ = strconv.Atoi(val)
			case "NFTABLES_TABLE":
				env.NftablesTable = val
			}
			found = true
		}
		if found {
			return env
		}
	}
	return nil
}
