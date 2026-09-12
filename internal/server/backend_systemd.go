//go:build !container

package server

import (
	"fmt"

	"leanclash/internal/config"
	"leanclash/internal/service"
	"leanclash/internal/systemd"
)

func newServiceManager(_ *config.ManagerConfig) (service.Manager, error) {
	manager, err := systemd.New()
	if err != nil {
		return nil, fmt.Errorf("创建 systemd 服务管理器失败: %w", err)
	}
	return manager, nil
}
