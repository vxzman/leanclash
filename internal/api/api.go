// Package api 提供守护进程的 REST + SSE 接口，以及内嵌前端的静态资源服务。
package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"leanclash/internal/config"
	"leanclash/internal/lifecycle"
)

type API struct {
	cfg *config.ManagerConfig
	lc  *lifecycle.Manager
	sse *Hub
}

// New 装配路由。statusPush 由调用方（server 包）提供定时推送入口。
func New(cfg *config.ManagerConfig, lc *lifecycle.Manager, webFS embed.FS) (http.Handler, *Hub) {
	a := &API{cfg: cfg, lc: lc, sse: NewHub()}

	mux := http.NewServeMux()

	// ─── 状态与操作 ───
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("POST /api/modes/{mode}/start", a.handleModeStart)
	mux.HandleFunc("POST /api/modes/{mode}/stop", a.handleModeStop)
	mux.HandleFunc("GET /api/events", a.sse.ServeHTTP)
	mux.HandleFunc("GET /events", a.sse.ServeHTTP)

	// ─── 配置 ───
	mux.HandleFunc("POST /api/config/sync", a.handleConfigSync)
	mux.HandleFunc("GET /api/config/general", a.handleGetGeneral)
	mux.HandleFunc("PUT /api/config/general", a.handlePutGeneral)
	mux.HandleFunc("GET /api/config/mode/{mode}", a.handleGetModeConfig)

	// ─── 系统设置（manager.yaml） ───
	mux.HandleFunc("GET /api/settings", a.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", a.handlePutSettings)

	// ─── 前端静态资源（内嵌 dist） ───
	staticFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Printf("[api] 挂载前端资源失败: %v", err)
	}
	mux.Handle("/", http.StripPrefix("/", http.FileServer(http.FS(staticFS))))

	return mux, a.sse
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// PublishStatus 构造状态快照帧并广播（供定时推送与操作后即时推送复用）。
func PublishStatus(hub *Hub, lc *lifecycle.Manager) {
	hub.Broadcast(mustJSON(map[string]interface{}{
		"type": "status",
		"data": lc.Status(context.Background()),
	}))
}

// publishStatus 操作完成后立即推送，SSE 定时推送兜底。
func (a *API) publishStatus() {
	PublishStatus(a.sse, a.lc)
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ─── Handlers ────────────────────────────────────────────────

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.lc.Status(r.Context()))
}

func (a *API) handleModeStart(w http.ResponseWriter, r *http.Request) {
	mode := r.PathValue("mode")
	if !a.validMode(mode) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("未知模式: %s", mode))
		return
	}
	if err := a.lc.StartMode(r.Context(), mode); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.publishStatus()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "mode": mode})
}

func (a *API) handleModeStop(w http.ResponseWriter, r *http.Request) {
	mode := r.PathValue("mode")
	if err := a.lc.StopMode(r.Context(), mode); err != nil {
		if strings.Contains(err.Error(), "未知模式") {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.publishStatus()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "mode": mode})
}

func (a *API) handleConfigSync(w http.ResponseWriter, r *http.Request) {
	if err := config.SyncAll(a.cfg, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.publishStatus()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (a *API) handleGetGeneral(w http.ResponseWriter, r *http.Request) {
	content, err := config.ReadGeneral(a.cfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (a *API) handlePutGeneral(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := config.SaveGeneral(a.cfg, body.Content); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.publishStatus()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (a *API) handleGetModeConfig(w http.ResponseWriter, r *http.Request) {
	mode := r.PathValue("mode")
	if !a.validMode(mode) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("未知模式: %s", mode))
		return
	}
	content, err := config.ReadModeConfig(a.cfg, mode)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (a *API) validMode(mode string) bool {
	_, ok := a.cfg.Modes[mode]
	return ok
}

// ─── Settings（manager.yaml） ────────────────────────────────

type modeUpdate struct {
	Env     *config.Env     `json:"env,omitempty"`
	Routing *config.Routing `json:"routing,omitempty"`
	Preset  string          `json:"preset,omitempty"`
}

type settingsUpdate struct {
	Daemon *config.DaemonSettings `json:"daemon,omitempty"`
	Modes  map[string]*modeUpdate `json:"modes,omitempty"`
}

func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.cfg)
}

// handlePutSettings 合并更新 manager.yaml：只动客户端提供的字段
// （env/routing/preset 整体替换），preset 或 socks 端口变化后自动 config sync。
func (a *API) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	syncNeeded := false
	reapplyModes := map[string]bool{}
	for name, up := range body.Modes {
		md, ok := a.cfg.Modes[name]
		if !ok || up == nil {
			continue
		}
		if up.Env != nil {
			md.Env = up.Env
			if name == "tproxy" || name == "redir-tproxy" {
				reapplyModes[name] = true
				syncNeeded = true
			}
			if name == "socks" {
				// socks 入站由 env.socks_port 生成，端口变化需要重新生成配置
				syncNeeded = true
			}
		}
		if up.Routing != nil {
			md.Routing = up.Routing
		}
		if up.Preset != "" {
			if err := config.ValidatePreset(up.Preset); err != nil {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("模式 %s 预定义配置无效: %w", name, err))
				return
			}
			md.Preset = up.Preset
			syncNeeded = true
		}
	}
	if body.Daemon != nil {
		*a.cfg = *withDaemon(a.cfg, body.Daemon)
	}

	if err := config.Save(a.cfg); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// preset/env 变化影响生成配置：同步各模式配置（通用配置存在时）。
	if syncNeeded {
		if _, err := os.Stat(a.cfg.GeneralPath()); err == nil {
			if err := config.SyncAll(a.cfg, ""); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
	}

	// 运行中的透明代理模式若参数发生变更，重新套用网络规则。
	for mode := range reapplyModes {
		if err := a.lc.ReapplyModeRules(r.Context(), mode); err != nil {
			writeErr(w, http.StatusInternalServerError, fmt.Errorf("应用模式 %s 网络规则失败: %w", mode, err))
			return
		}
	}
	a.publishStatus()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func withDaemon(cfg *config.ManagerConfig, d *config.DaemonSettings) *config.ManagerConfig {
	merged := *d
	if merged.WebAddr == "" {
		merged.WebAddr = cfg.Daemon.WebAddr
	}
	if merged.CliSocket == "" {
		merged.CliSocket = cfg.Daemon.CliSocket
	}
	if merged.ApplyDelayMs == 0 {
		merged.ApplyDelayMs = cfg.Daemon.ApplyDelayMs
	}
	if merged.ReconcileInterval == "" {
		merged.ReconcileInterval = cfg.Daemon.ReconcileInterval
	}
	cfg.Daemon = merged
	return cfg
}
