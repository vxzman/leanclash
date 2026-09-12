//go:build container

// Package process implements service.Manager without systemd. It manages the
// mihomo@<mode> instances declared in ManagerConfig directly as child
// processes.
package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"leanclash/internal/config"
	"leanclash/internal/service"
)

const stopTimeout = 10 * time.Second

type Manager struct {
	cfg   *config.ManagerConfig
	bin   string
	mu    sync.Mutex
	units map[string]*unit
	done  chan struct{}
	once  sync.Once
}

type unit struct {
	name       string
	mode       string
	state      string
	cmd        *exec.Cmd
	generation uint64
	done       chan struct{}
	ready      chan struct{}
}

// 容器后端不实现 service.InstanceLister：不探测宿主机 mihomo@ 实例。
var _ service.Manager = (*Manager)(nil)

func New(cfg *config.ManagerConfig) *Manager {
	bin := os.Getenv("MIHOMO_BINARY")
	if bin == "" {
		bin = "/usr/bin/mihomo"
	}
	return &Manager{
		cfg:   cfg,
		bin:   bin,
		units: make(map[string]*unit),
		done:  make(chan struct{}),
	}
}

func (m *Manager) Start(ctx context.Context, name string) error {
	name = service.Normalize(name)
	for {
		m.mu.Lock()
		u, err := m.lookupLocked(name)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		switch u.state {
		case "active":
			m.mu.Unlock()
			return nil
		case "activating":
			wait := u.ready
			m.mu.Unlock()
			if err := waitFor(ctx, wait); err != nil {
				return err
			}
			continue
		case "deactivating":
			wait := u.done
			m.mu.Unlock()
			if err := waitFor(ctx, wait); err != nil {
				return err
			}
			continue
		case "failed":
			u.state = "inactive"
		}

		md := m.cfg.Modes[u.mode]
		cfgPath := filepath.Join(m.cfg.ConfigDir(), md.Config)
		if _, err := os.Stat(cfgPath); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("启动 %s 失败：配置文件不可用: %w", name, err)
		}

		cmd := exec.Command(m.bin, "-d", m.cfg.DataDir(), "-f", cfgPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		u.state = "activating"
		u.cmd = cmd
		u.generation++
		generation := u.generation
		u.done = make(chan struct{})
		u.ready = make(chan struct{})
		done := u.done
		ready := u.ready
		m.mu.Unlock()

		if err := cmd.Start(); err != nil {
			m.finish(name, generation, "failed")
			return fmt.Errorf("启动 %s 失败: %w", name, err)
		}

		go m.wait(name, generation, cmd, done, ready)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
			m.mu.Lock()
			state := u.state
			m.mu.Unlock()
			if state != "active" {
				return fmt.Errorf("启动 %s 失败", name)
			}
		case <-time.After(300 * time.Millisecond):
			m.mu.Lock()
			if u.generation == generation && u.state == "activating" {
				u.state = "active"
				close(ready)
			}
			state := u.state
			m.mu.Unlock()
			if state != "active" {
				return fmt.Errorf("启动 %s 失败", name)
			}
			return nil
		}
	}
}

func (m *Manager) Stop(ctx context.Context, name string) error {
	name = service.Normalize(name)
	m.mu.Lock()
	u, err := m.lookupLocked(name)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if u.state == "inactive" || u.state == "failed" {
		u.state = "inactive"
		m.mu.Unlock()
		return nil
	}
	if u.state == "deactivating" {
		wait := u.done
		m.mu.Unlock()
		return waitFor(ctx, wait)
	}
	u.state = "deactivating"
	cmd := u.cmd
	wait := u.done
	generation := u.generation
	m.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			return fmt.Errorf("停止 %s 失败: %w", name, err)
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wait:
		return nil
	case <-time.After(stopTimeout):
		if cmd != nil && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		m.finish(name, generation, "inactive")
		return nil
	}
}

func (m *Manager) ActiveState(ctx context.Context, name string) string {
	name = service.Normalize(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.lookupLocked(name)
	if err != nil {
		return "unknown"
	}
	return u.state
}

func (m *Manager) IsActive(ctx context.Context, name string) bool {
	switch m.ActiveState(ctx, name) {
	case "active", "activating", "reloading":
		return true
	default:
		return false
	}
}

func (m *Manager) SubscribeStates(interval time.Duration) (<-chan map[string]*service.UnitStatus, <-chan error) {
	updates := make(chan map[string]*service.UnitStatus, 1)
	errs := make(chan error, 1)
	if interval <= 0 {
		interval = time.Second
	}
	go func() {
		defer close(updates)
		defer close(errs)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var previous map[string]string
		for {
			snapshot := m.snapshot()
			changed := diff(previous, snapshot)
			if len(changed) > 0 {
				select {
				case updates <- changed:
				case <-m.done:
					return
				}
			}
			previous = snapshot
			select {
			case <-ticker.C:
			case <-m.done:
				return
			}
		}
	}()
	return updates, errs
}

func (m *Manager) Close() {
	m.once.Do(func() {
		close(m.done)
		m.mu.Lock()
		units := make([]*unit, 0, len(m.units))
		for _, u := range m.units {
			units = append(units, u)
		}
		m.mu.Unlock()
		for _, u := range units {
			if u.cmd != nil && u.cmd.Process != nil {
				_ = syscall.Kill(-u.cmd.Process.Pid, syscall.SIGTERM)
			}
		}
	})
}

func (m *Manager) lookupLocked(name string) (*unit, error) {
	if !strings.HasPrefix(name, "mihomo@") || !strings.HasSuffix(name, ".service") {
		return nil, fmt.Errorf("不支持的服务单元: %s", name)
	}
	mode := strings.TrimSuffix(strings.TrimPrefix(name, "mihomo@"), ".service")
	if _, ok := m.cfg.Modes[mode]; !ok {
		return nil, fmt.Errorf("未知服务单元: %s", name)
	}
	if u, ok := m.units[name]; ok {
		return u, nil
	}
	u := &unit{name: name, mode: mode, state: "inactive"}
	m.units[name] = u
	return u, nil
}

func (m *Manager) wait(name string, generation uint64, cmd *exec.Cmd, done, ready chan struct{}) {
	_ = cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.units[name]
	if !ok || u.generation != generation || u.done != done {
		return
	}
	if u.state == "deactivating" {
		u.state = "inactive"
	} else {
		u.state = "failed"
	}
	u.cmd = nil
	select {
	case <-ready:
	default:
		close(ready)
	}
	close(done)
}

func (m *Manager) finish(name string, generation uint64, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.units[name]
	if !ok || u.generation != generation {
		return
	}
	u.state = state
	u.cmd = nil
	if u.ready != nil {
		select {
		case <-u.ready:
		default:
			close(u.ready)
		}
	}
	select {
	case <-u.done:
	default:
		close(u.done)
	}
}

func (m *Manager) snapshot() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := make(map[string]string, len(m.units))
	for name, u := range m.units {
		snapshot[name] = u.state
	}
	return snapshot
}

func diff(previous, current map[string]string) map[string]*service.UnitStatus {
	changed := make(map[string]*service.UnitStatus)
	for name, state := range current {
		if previous != nil && previous[name] == state {
			continue
		}
		changed[name] = &service.UnitStatus{Name: name, ActiveState: state}
	}
	for name := range previous {
		if _, ok := current[name]; !ok {
			changed[name] = nil
		}
	}
	return changed
}

func waitFor(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
