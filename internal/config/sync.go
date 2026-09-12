package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const GeneralConfig = "config_general.yaml"

// ─── 配置同步 ────────────────────────────────────────────────
// 单一事实源：config_general.yaml（用户代理配置） + manager.yaml 各模式
// preset（预定义入站；socks 由 env.socks_port 生成）。SyncAll 将两者合并
// 生成全部 config_<mode>.yaml。

func (c *ManagerConfig) GeneralPath() string {
	return filepath.Join(c.ConfigDir(), GeneralConfig)
}

func (c *ManagerConfig) ModeConfigPath(mode string) (string, error) {
	m, ok := c.Modes[mode]
	if !ok {
		return "", fmt.Errorf("未知模式: %s", mode)
	}
	return filepath.Join(c.ConfigDir(), m.Config), nil
}

func ReadGeneral(c *ManagerConfig) (string, error) {
	data, err := os.ReadFile(c.GeneralPath())
	if err != nil {
		return "", fmt.Errorf("读取通用配置失败: %w", err)
	}
	return string(data), nil
}

func ReadModeConfig(c *ManagerConfig, mode string) (string, error) {
	path, err := c.ModeConfigPath(mode)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取配置 %s 失败: %w", mode, err)
	}
	return string(data), nil
}

// SaveGeneral 保存通用配置并同步全部模式配置。
func SaveGeneral(c *ManagerConfig, content string) error {
	if err := testMihomoConfig(c, content); err != nil {
		return fmt.Errorf("mihomo 配置测试失败: %w", err)
	}
	if err := os.WriteFile(c.GeneralPath(), []byte(content), 0644); err != nil {
		return fmt.Errorf("写入通用配置失败: %w", err)
	}
	return SyncAll(c, content)
}

// SyncAll 读取当前通用配置（content 为空则读盘），生成全部模式配置。
func SyncAll(c *ManagerConfig, content string) error {
	if content == "" {
		var err error
		content, err = ReadGeneral(c)
		if err != nil {
			return err
		}
	}
	general, err := parseGeneral(content)
	if err != nil {
		return err
	}
	for name := range c.Modes {
		listeners, err := modeListeners(c.Modes[name], name)
		if err != nil {
			return fmt.Errorf("生成 %s 配置失败: %w", name, err)
		}
		cfg, err := buildModeConfig(general, listeners)
		if err != nil {
			return fmt.Errorf("生成 %s 配置失败: %w", name, err)
		}
		if err := testMihomoConfig(c, string(cfg)); err != nil {
			return fmt.Errorf("%s 配置测试失败: %w", name, err)
		}
		path, err := c.ModeConfigPath(name)
		if err != nil {
			return err
		}
		header := modeConfigHeader(name)
		if err := os.WriteFile(path, append([]byte(header), cfg...), 0644); err != nil {
			return fmt.Errorf("写入 %s 配置失败: %w", name, err)
		}
	}
	return nil
}

// ─── 合并逻辑 ────────────────────────────────────────────────

func parseGeneral(content string) (map[string]interface{}, error) {
	var general map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &general); err != nil {
		return nil, fmt.Errorf("通用配置 YAML 语法错误: %w", err)
	}
	if general == nil {
		return nil, fmt.Errorf("通用配置为空")
	}
	return general, nil
}

// ValidatePreset 检查预定义入站是否为非空 listeners 列表。
func ValidatePreset(preset string) error {
	var doc struct {
		Listeners []map[string]interface{} `yaml:"listeners"`
	}
	if err := yaml.Unmarshal([]byte("listeners:\n"+preset), &doc); err != nil {
		return fmt.Errorf("YAML 语法错误: %w", err)
	}
	if len(doc.Listeners) == 0 {
		return fmt.Errorf("入站模块必须是至少包含一项的列表")
	}
	return nil
}

// modeListeners 返回模式的预定义入站列表：socks 由 env.socks_port 生成
//（端口即事实源，与透明代理模式一致），其余模式解析 preset。
func modeListeners(m *Mode, name string) ([]map[string]interface{}, error) {
	if name == "socks" {
		if m.Env == nil || m.Env.SocksPort <= 0 {
			return nil, fmt.Errorf("缺少 env.socks_port")
		}
		return []map[string]interface{}{{
			"name":   "mixed-in",
			"type":   "mixed",
			"port":   m.Env.SocksPort,
			"listen": "0.0.0.0",
			"udp":    true,
		}}, nil
	}
	var doc struct {
		Listeners []map[string]interface{} `yaml:"listeners"`
	}
	if err := yaml.Unmarshal([]byte("listeners:\n"+m.Preset), &doc); err != nil {
		return nil, fmt.Errorf("解析预定义配置失败: %w", err)
	}
	if m.Env != nil {
		for _, l := range doc.Listeners {
			t, _ := l["type"].(string)
			if t == "tproxy" && m.Env.TproxyPort > 0 {
				l["port"] = m.Env.TproxyPort
			} else if t == "redir" && m.Env.RedirectPort > 0 {
				l["port"] = m.Env.RedirectPort
			}
		}
	}
	return doc.Listeners, nil
}

// buildModeConfig 将通用配置与模式预定义入站合并：用户自写 listeners 保留，
// 预定义中同名不覆盖、缺名追加。
func buildModeConfig(general map[string]interface{}, listeners []map[string]interface{}) ([]byte, error) {
	cfg := make(map[string]interface{}, len(general)+1)
	for k, v := range general {
		cfg[k] = v
	}

	if existing, ok := cfg["listeners"].([]interface{}); ok && len(existing) > 0 {
		existingNames := map[string]bool{}
		for _, l := range existing {
			if lm, ok := l.(map[string]interface{}); ok {
				if name, ok := lm["name"].(string); ok {
					existingNames[name] = true
				}
			}
		}
		for _, pl := range listeners {
			name, _ := pl["name"].(string)
			if name != "" && existingNames[name] {
				continue
			}
			existing = append(existing, pl)
		}
		cfg["listeners"] = existing
	} else if len(listeners) > 0 {
		cfg["listeners"] = listeners
	}

	return yaml.Marshal(cfg)
}

func modeConfigHeader(mode string) string {
	label := strings.ToUpper(mode)
	return fmt.Sprintf(`# ============================================================
# Mihomo %s Mode Configuration
# Auto-generated from config_general.yaml + manager.yaml preset
# DO NOT EDIT DIRECTLY — edit config_general.yaml and run config sync
# ============================================================

`, label)
}

// testMihomoConfig 用 mihomo -t 校验配置；机器上没有 mihomo（如 CI）则跳过。
//
// 防挂起要点（实测教训）：mihomo 校验含 GEOIP 规则的配置时，若 -d 目录里
// 没有 MMDB 数据会尝试联网下载——网络受限环境会永久挂起。因此：
//  1. 整个校验包在 20s 超时内（context 杀死子进程）；
//  2. 优先用数据目录（生产环境含 geodata）；
//  3. 数据目录不存在时（dev/非 root）把常见 geodata 文件拷入临时目录。
func testMihomoConfig(c *ManagerConfig, content string) error {
	if _, err := exec.LookPath("mihomo"); err != nil {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "leanclash-test-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return err
	}

	workDir := c.DataDir()
	if _, err := os.Stat(workDir); err != nil {
		workDir = tmpDir
		copyGeodata(workDir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mihomo", "-t", "-d", workDir, "-f", tmpFile)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("配置校验超时（20s）：mihomo 可能因缺少 geodata 而尝试联网下载，请检查 %s 下的 geoip/geosite 数据", workDir)
	}
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// copyGeodata 把常见 geodata 文件从当前用户 mihomo 主目录拷入目标目录，
// 让 mihomo -t 无需联网。找不到则跳过（最坏情况触发上面的超时报错）。
func copyGeodata(dst string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	srcDir := filepath.Join(home, ".config", "mihomo")
	names := []string{
		"geoip.metadb", "geosite.dat", "GeoIP.dat", "GeoSite.dat",
		"ASN.mmdb", "geoip.db", "geosite.db", "country.mmdb",
	}
	for _, n := range names {
		if data, err := os.ReadFile(filepath.Join(srcDir, n)); err == nil {
			_ = os.WriteFile(filepath.Join(dst, n), data, 0644)
		}
	}
}
