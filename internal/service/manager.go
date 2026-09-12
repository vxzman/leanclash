// Package service defines the service-management contract used by the
// lifecycle layer. Backends may use systemd, a container process supervisor,
// or another service manager without exposing backend-specific types.
package service

import (
	"context"
	"strings"
	"time"
)

type UnitStatus struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
}

type Manager interface {
	Start(ctx context.Context, unit string) error
	Stop(ctx context.Context, unit string) error
	ActiveState(ctx context.Context, unit string) string
	IsActive(ctx context.Context, unit string) bool
	SubscribeStates(interval time.Duration) (<-chan map[string]*UnitStatus, <-chan error)
	Close()
}

// InstanceLister is an optional capability of a service.Manager.
// The systemd backend implements it so the homepage can show whichever
// mihomo@<instance> unit is actually running, even if it was started
// outside LeanClash. The container process supervisor does not implement
// this interface.
type InstanceLister interface {
	ListActiveInstances(ctx context.Context) ([]string, error)
}

// Normalize adds the .service suffix used by service-manager APIs.
func Normalize(unit string) string {
	if unit == "" {
		return unit
	}
	if i := strings.LastIndex(unit, "."); i >= 0 && i > strings.LastIndex(unit, "@") {
		return unit
	}
	return unit + ".service"
}

// MihomoInstance extracts the instance name from a mihomo@ unit.
// "mihomo@tun.service" and "mihomo@tun" both yield "tun".
func MihomoInstance(unit string) (string, bool) {
	name := Normalize(unit)
	const prefix = "mihomo@"
	const suffix = ".service"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	inst := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if inst == "" {
		return "", false
	}
	return inst, true
}
