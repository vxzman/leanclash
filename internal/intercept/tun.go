package intercept

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ─── TUN 模式清理 ────────────────────────────────────────────
// mihomo 的 auto-route/auto-redirect 自建 tun0 与策略路由规则。tun0 消失后
// 可能残留 ip rule（指向 iproute2-table-index 表）与 nft 表，需要清理。
// 与旧 tun_cleanup 的差异：规则数字从 manager.yaml 的 routing.table_index
// 派生，不再硬编码 8999/9000/9001/9002/9010。

// CleanupTun 删除指向 tableIndex 的全部 ip rule、flush 路由表、删除 nft 表
// 列表（表名含 family，如 "inet mihomo"）。全部幂等。
func CleanupTun(tableIndex, ruleIndex int, nftTables []string) error {
	table := strconv.Itoa(tableIndex)

	// mihomo 的 auto-route 规则包含 goto/suppress 规则，不一定 lookup
	// tableIndex，因此除了按路由表匹配，还清理 rule-index 附近的已知规则。
	for i := 0; i < 64; i++ {
		prefs, err := tunRulePriorities(table, ruleIndex)
		if err != nil {
			return err
		}
		if len(prefs) == 0 {
			break
		}
		for _, pref := range prefs {
			ipIgnore("rule", "del", "pref", pref)
		}
	}

	ipIgnore("route", "flush", "table", table)

	for _, t := range nftTables {
		parts := strings.Fields(t)
		if len(parts) == 2 { // "inet mihomo" / "ip mihomo_tproxy4"
			nftIgnore(append([]string{"delete", "table"}, parts...)...)
		}
	}
	return nil
}

// tunRulePriorities 返回 ip rule show 中与 table 或 ruleIndex 相关的优先级列表（单次解析）。
func tunRulePriorities(table string, ruleIndex int) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "rule", "show").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ip rule show: %s", strings.TrimSpace(string(out)))
	}

	lookup := "lookup " + table
	wantPriorities := map[string]struct{}{}
	if ruleIndex > 0 {
		for _, p := range []int{ruleIndex - 1, ruleIndex, ruleIndex + 1, ruleIndex + 2, ruleIndex + 10} {
			if p > 0 {
				wantPriorities[strconv.Itoa(p)] = struct{}{}
			}
		}
	}

	var prefs []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		pref := strings.TrimSpace(line[:idx])
		if strings.Contains(line, lookup) {
			if !seen[pref] {
				prefs = append(prefs, pref)
				seen[pref] = true
			}
			continue
		}
		if _, ok := wantPriorities[pref]; ok {
			if !seen[pref] {
				prefs = append(prefs, pref)
				seen[pref] = true
			}
		}
	}
	return prefs, nil
}

// TunRulesLeftover 检测指定路由表是否残留规则（只读检测，供状态页/reconcile）。
func TunRulesLeftover(tableIndex, ruleIndex int) (bool, error) {
	prefs, err := tunRulePriorities(strconv.Itoa(tableIndex), ruleIndex)
	if err != nil {
		return false, err
	}
	return len(prefs) > 0, nil
}
