package lifecycle

import (
	"context"
	"testing"
	"time"

	"leanclash/internal/config"
	"leanclash/internal/service"
)

type stubSys struct {
	states  map[string]string
	stopped []string
}

func (s *stubSys) Start(context.Context, string) error { return nil }

func (s *stubSys) Stop(_ context.Context, unit string) error {
	s.stopped = append(s.stopped, service.Normalize(unit))
	return nil
}

func (s *stubSys) ActiveState(_ context.Context, unit string) string {
	if st, ok := s.states[service.Normalize(unit)]; ok {
		return st
	}
	if st, ok := s.states[unit]; ok {
		return st
	}
	return "inactive"
}

func (s *stubSys) IsActive(ctx context.Context, unit string) bool {
	switch s.ActiveState(ctx, unit) {
	case "active", "activating", "reloading":
		return true
	default:
		return false
	}
}

func (s *stubSys) SubscribeStates(time.Duration) (<-chan map[string]*service.UnitStatus, <-chan error) {
	return make(chan map[string]*service.UnitStatus), make(chan error)
}

func (s *stubSys) Close() {}

type listingSys struct {
	*stubSys
	instances []string
}

func (s *listingSys) ListActiveInstances(context.Context) ([]string, error) {
	return s.instances, nil
}

func testConfig() *config.ManagerConfig {
	return &config.ManagerConfig{
		Modes: map[string]*config.Mode{
			"tun":    {Label: "TUN", Unit: "mihomo@tun", Config: "config_tun.yaml"},
			"tproxy": {Label: "TPROXY", Unit: "mihomo@tproxy", Config: "config_tproxy.yaml"},
			"socks":  {Label: "SOCKS", Unit: "mihomo@socks", Config: "config_socks.yaml"},
		},
	}
}

func TestStatusWithoutInstanceListerUsesUnitState(t *testing.T) {
	sys := &stubSys{states: map[string]string{"mihomo@tun.service": "active"}}
	m := New(testConfig(), sys)
	st := m.Status(context.Background())
	if !st.Modes["tun"].Active {
		t.Fatal("tun should be active from unit state")
	}
	if st.ActiveMode != "tun" {
		t.Fatalf("active_mode = %q, want tun", st.ActiveMode)
	}
	if _, ok := st.Modes["custom"]; ok {
		t.Fatal("container-like backend should not invent extra modes")
	}
}

func TestStatusOverlaysSystemdInstances(t *testing.T) {
	sys := &listingSys{
		stubSys:   &stubSys{states: map[string]string{"mihomo@tun.service": "inactive"}},
		instances: []string{"tproxy"},
	}
	m := New(testConfig(), sys)
	st := m.Status(context.Background())
	if st.Modes["tun"].Active {
		t.Fatal("tun unit is inactive and was not discovered")
	}
	if !st.Modes["tproxy"].Active {
		t.Fatal("discovered tproxy instance should be active")
	}
	if st.Modes["tproxy"].UnitState != "active" {
		t.Fatalf("tproxy unit_state = %q, want active", st.Modes["tproxy"].UnitState)
	}
	if st.ActiveMode != "tproxy" {
		t.Fatalf("active_mode = %q, want tproxy", st.ActiveMode)
	}
}

func TestStatusShowsUnknownDiscoveredInstance(t *testing.T) {
	sys := &listingSys{
		stubSys:   &stubSys{states: map[string]string{}},
		instances: []string{"legacy"},
	}
	m := New(testConfig(), sys)
	st := m.Status(context.Background())
	ms, ok := st.Modes["legacy"]
	if !ok || !ms.Active {
		t.Fatalf("expected discovered legacy instance, got %#v", st.Modes)
	}
	if st.ActiveMode != "legacy" {
		t.Fatalf("active_mode = %q, want legacy", st.ActiveMode)
	}
	if ms.Unit != "mihomo@legacy" {
		t.Fatalf("unit = %q, want mihomo@legacy", ms.Unit)
	}
}

func TestStopModeDiscoveredInstance(t *testing.T) {
	sys := &listingSys{
		stubSys:   &stubSys{states: map[string]string{}},
		instances: []string{"legacy"},
	}
	m := New(testConfig(), sys)
	if err := m.StopMode(context.Background(), "legacy"); err != nil {
		t.Fatal(err)
	}
	if len(sys.stopped) != 1 || sys.stopped[0] != "mihomo@legacy.service" {
		t.Fatalf("stopped = %v, want mihomo@legacy.service", sys.stopped)
	}
}

func TestStopModeUnknownWithoutDiscovery(t *testing.T) {
	sys := &stubSys{states: map[string]string{}}
	m := New(testConfig(), sys)
	if err := m.StopMode(context.Background(), "legacy"); err == nil {
		t.Fatal("expected unknown mode error")
	}
}
