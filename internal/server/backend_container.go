//go:build container

package server

import (
	"leanclash/internal/config"
	"leanclash/internal/service"
	"leanclash/internal/service/process"
)

func newServiceManager(cfg *config.ManagerConfig) (service.Manager, error) {
	return process.New(cfg), nil
}
