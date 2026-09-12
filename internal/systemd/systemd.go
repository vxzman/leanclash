//go:build !container

// Package systemd implements the native service.Manager backend using
// systemd's D-Bus API.
package systemd

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"

	"leanclash/internal/service"
)

const opTimeout = 60 * time.Second

type Client struct {
	conn *dbus.Conn
}

var _ service.Manager = (*Client)(nil)
var _ service.InstanceLister = (*Client)(nil)

// Normalize is kept as a package-local compatibility helper for callers of
// the systemd backend. New code should use service.Normalize.
func Normalize(unit string) string { return service.Normalize(unit) }

func New() (*Client, error) {
	// 注意：不能传带超时的 ctx 并随后 cancel——godbus 的 WithContext(ctx)
	// 会在 ctx 取消时关闭连接，这里用 Background，生命周期由 Close() 管理。
	conn, err := dbus.NewSystemConnectionContext(context.Background())
	if err != nil {
		return nil, fmt.Errorf("连接 systemd dbus 失败: %w", err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() {
	c.conn.Close()
}

// Start 启动单元并等待任务完成（"done" 或 "failed"）。
func (c *Client) Start(ctx context.Context, unit string) error {
	unit = Normalize(unit)
	ch := make(chan string)
	if _, err := c.conn.StartUnitContext(ctx, unit, "replace", ch); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", unit, err)
	}
	return waitJob(unit, ch)
}

// Stop 停止单元并等待任务完成。
func (c *Client) Stop(ctx context.Context, unit string) error {
	unit = Normalize(unit)
	ch := make(chan string)
	if _, err := c.conn.StopUnitContext(ctx, unit, "replace", ch); err != nil {
		return fmt.Errorf("停止 %s 失败: %w", unit, err)
	}
	return waitJob(unit, ch)
}

func waitJob(unit string, ch chan string) error {
	select {
	case res := <-ch:
		switch res {
		case "done", "skipped":
			return nil
		case "failed":
			return fmt.Errorf("%s 任务失败", unit)
		default:
			return fmt.Errorf("%s 任务异常: %s", unit, res)
		}
	case <-time.After(opTimeout):
		return fmt.Errorf("%s 操作超时(%v)", unit, opTimeout)
	}
}

// ActiveState 返回单元当前 ActiveState；查询失败返回 "unknown"。
func (c *Client) ActiveState(ctx context.Context, unit string) string {
	prop, err := c.conn.GetUnitPropertyContext(ctx, Normalize(unit), "ActiveState")
	if err != nil {
		return "unknown"
	}
	state, ok := prop.Value.Value().(string)
	if !ok {
		return "unknown"
	}
	return state
}

// IsActive 判断单元是否处于活跃状态（含 activating，等价旧脚本 is_service_alive）。
func (c *Client) IsActive(ctx context.Context, unit string) bool {
	switch c.ActiveState(ctx, unit) {
	case "active", "activating", "reloading":
		return true
	}
	return false
}

// ListActiveInstances returns instance names of running mihomo@ units
// (e.g. "tun" for mihomo@tun.service). Used by the homepage to reflect
// instances started outside LeanClash.
func (c *Client) ListActiveInstances(ctx context.Context) ([]string, error) {
	units, err := c.conn.ListUnitsByPatternsContext(ctx,
		[]string{"active", "activating", "reloading"},
		[]string{"mihomo@*.service"},
	)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(units))
	seen := map[string]bool{}
	for _, u := range units {
		switch u.ActiveState {
		case "active", "activating", "reloading":
		default:
			continue
		}
		inst, ok := service.MihomoInstance(u.Name)
		if !ok || seen[inst] {
			continue
		}
		seen[inst] = true
		out = append(out, inst)
	}
	return out, nil
}

// SubscribeStates adapts systemd unit snapshots to the backend-neutral
// service.UnitStatus type. Deleted units are represented by nil values.
func (c *Client) SubscribeStates(interval time.Duration) (<-chan map[string]*service.UnitStatus, <-chan error) {
	rawUpdates, rawErrors := c.conn.SubscribeUnits(interval)
	updates := make(chan map[string]*service.UnitStatus)
	errs := make(chan error)

	go func() {
		defer close(updates)
		defer close(errs)
		for {
			select {
			case snapshot, ok := <-rawUpdates:
				if !ok {
					return
				}
				converted := make(map[string]*service.UnitStatus, len(snapshot))
				for name, status := range snapshot {
					if status == nil {
						converted[name] = nil
						continue
					}
					converted[name] = &service.UnitStatus{
						Name:        status.Name,
						Description: status.Description,
						LoadState:   status.LoadState,
						ActiveState: status.ActiveState,
						SubState:    status.SubState,
					}
				}
				updates <- converted
			case err, ok := <-rawErrors:
				if !ok {
					rawErrors = nil
					continue
				}
				errs <- err
			}
		}
	}()

	return updates, errs
}
