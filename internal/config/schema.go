package config

// ManagerConfig 是 /opt/leanclash/manager.yaml 的类型化视图：
// 所有 ip rule 数字、env 变量、模式定义与预定义入站配置的单一事实源。
type ManagerConfig struct {
	Dirs   Dirs             `yaml:"dirs" json:"dirs"`
	Daemon DaemonSettings   `yaml:"daemon" json:"daemon"`
	Modes  map[string]*Mode `yaml:"modes" json:"modes"`
}

type Dirs struct {
	ConfigDir string `yaml:"config_dir" json:"config_dir"`
	DataDir   string `yaml:"data_dir" json:"data_dir"`
}

type DaemonSettings struct {
	WebAddr           string `yaml:"web_addr" json:"web_addr"`
	CliSocket         string `yaml:"cli_socket" json:"cli_socket"`
	ApplyDelayMs      int    `yaml:"apply_delay_ms" json:"apply_delay_ms"`
	ReconcileInterval string `yaml:"reconcile_interval" json:"reconcile_interval"`
}

type Mode struct {
	Label   string   `yaml:"label" json:"label"`
	Unit    string   `yaml:"unit" json:"unit"`
	Config  string   `yaml:"config" json:"config"`
	Routing *Routing `yaml:"routing,omitempty" json:"routing,omitempty"`
	Cleanup *Cleanup `yaml:"cleanup,omitempty" json:"cleanup,omitempty"`
	Env     *Env     `yaml:"env,omitempty" json:"env,omitempty"`
	Preset  string   `yaml:"preset,omitempty" json:"preset,omitempty"`
}

// Routing 承载 tun 模式的 iproute2 索引：清理逻辑据此派生，不再硬编码数字。
type Routing struct {
	RuleIndex  int `yaml:"rule_index" json:"rule_index"`
	TableIndex int `yaml:"table_index" json:"table_index"`
}

// Cleanup 列出 tun 接口消失后需要清理的 nft 表。
type Cleanup struct {
	NftTables []string `yaml:"nft_tables" json:"nft_tables"`
}

// Env 承载各模式的网络参数：tproxy / redir-tproxy 的透明代理参数，
// socks 的入站监听端口。
// 回环避免：ExcludeGID（meta skgid）优先，RoutingMark（meta mark）备选，至少其一。
type Env struct {
	TproxyPort    int    `yaml:"tproxy_port,omitempty" json:"tproxy_port,omitempty"`
	RedirectPort  int    `yaml:"redirect_port,omitempty" json:"redirect_port,omitempty"`
	SocksPort     int    `yaml:"socks_port,omitempty" json:"socks_port,omitempty"`
	ExcludeGID    int    `yaml:"exclude_gid,omitempty" json:"exclude_gid,omitempty"`
	RoutingMark   int    `yaml:"routing_mark,omitempty" json:"routing_mark,omitempty"`
	Fwmark        int    `yaml:"fwmark,omitempty" json:"fwmark,omitempty"`
	TableID       int    `yaml:"table_id,omitempty" json:"table_id,omitempty"`
	NftablesTable string `yaml:"nftables_table,omitempty" json:"nftables_table,omitempty"`
}

// ─── 默认配置 ────────────────────────────────────────────────

const (
	DefaultConfigPath = "/opt/leanclash/manager.yaml"

	// 与旧面板/脚本保持一致的默认值，迁移时被旧 .conf 覆盖。
	defaultTproxyPort   = 22016
	defaultRedirectPort = 22017
	defaultSocksPort    = 20260
	defaultExcludeGID   = 988
	defaultRoutingMark  = 6666
	defaultFwmark       = 1
	defaultTableID      = 100
)

const tunPreset = `  - name: tun-in
    type: tun
    stack: system
    dns-hijack:
      - any:53
      - tcp://any:53
    device: tun0
    mtu: 1500
    gso: true
    gso-max-size: 65536
    udp-timeout: 300
    iproute2-table-index: 2022
    iproute2-rule-index: 9000
    endpoint-independent-nat: true
    auto-detect-interface: true
    auto-route: true
    auto-redirect: true
    inet4-address:
      - 192.0.2.0/30
`

const tproxyPreset = `  - name: tproxy-in
    type: tproxy
    port: 22016
    listen: 0.0.0.0
    udp: true
`

const redirTproxyPreset = `  - name: tproxy-in
    type: tproxy
    port: 22016
    listen: 0.0.0.0
    udp: true
  - name: redir-in
    type: redir
    port: 22017
    listen: 0.0.0.0
`

// ManagedModes 是面板维护的四种运行模式，展示顺序固定。
var ManagedModes = []string{"tun", "tproxy", "redir-tproxy", "socks"}

// Default builds the built-in manager config, used for first-run installs and
// as the base for migration from the old .conf layout.
func Default() *ManagerConfig {
	return &ManagerConfig{
		Dirs: Dirs{
			ConfigDir: "/etc/mihomo",
			DataDir:   "/var/lib/mihomo",
		},
		Daemon: DaemonSettings{
			// 面板无鉴权：默认仅监听 IPv4（0.0.0.0），不暴露 IPv6。
			// 需要 IPv6 时显式设置：[::]:8081（仅 IPv6）或 :8081（双栈）。
			WebAddr:           "0.0.0.0:8081",
			CliSocket:         "/run/leanclash/leanclash.sock",
			ApplyDelayMs:      1000,
			ReconcileInterval: "5s",
		},
		Modes: map[string]*Mode{
			"tun": {
				Label:  "TUN",
				Unit:   "mihomo@tun",
				Config: "config_tun.yaml",
				Routing: &Routing{
					RuleIndex:  9000,
					TableIndex: 2022,
				},
				Cleanup: &Cleanup{
					NftTables: []string{"inet mihomo"},
				},
				Preset: tunPreset,
			},
			"tproxy": {
				Label:  "TPROXY",
				Unit:   "mihomo@tproxy",
				Config: "config_tproxy.yaml",
				Env: &Env{
					TproxyPort:    defaultTproxyPort,
					ExcludeGID:    defaultExcludeGID,
					RoutingMark:   defaultRoutingMark,
					Fwmark:        defaultFwmark,
					TableID:       defaultTableID,
					NftablesTable: "mihomo_tproxy4",
				},
				Preset: tproxyPreset,
			},
			"redir-tproxy": {
				Label:  "REDIR-TPROXY",
				Unit:   "mihomo@redir-tproxy",
				Config: "config_redir-tproxy.yaml",
				Env: &Env{
					TproxyPort:    defaultTproxyPort,
					RedirectPort:  defaultRedirectPort,
					ExcludeGID:    defaultExcludeGID,
					RoutingMark:   defaultRoutingMark,
					Fwmark:        defaultFwmark,
					TableID:       defaultTableID,
					NftablesTable: "mihomo_redir_tproxy4",
				},
				Preset: redirTproxyPreset,
			},
			"socks": {
				Label:  "SOCKS",
				Unit:   "mihomo@socks",
				Config: "config_socks.yaml",
				Env: &Env{
					SocksPort: defaultSocksPort,
				},
			},
		},
	}
}
