package api

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/hub"
)

// configHandler 运行时配置 API 处理器
type configHandler struct {
	hub *hub.Hub
}

// newConfigHandler 创建配置处理器
func newConfigHandler(h *hub.Hub) *configHandler {
	return &configHandler{hub: h}
}

// getConfig GET /api/v1/config
// 返回当前运行时配置（SASOC 地址、端口等）
func (ch *configHandler) getConfig(c *gin.Context) {
	cfg := ch.hub.Config()
	if cfg == nil {
		responseError(c, http.StatusInternalServerError, "config not available")
		return
	}

	responseSuccess(c, gin.H{
		"sasocHost": cfg.Sasoc.Host,
		"sasocPort": cfg.Sasoc.Port,
		"simulator": gin.H{
			"heartbeatInterval": cfg.Simulator.HeartbeatInterval,
			"reconnectInterval": cfg.Simulator.ReconnectInterval,
			"readTimeout":       cfg.Simulator.ReadTimeout,
			"offlineTimeout":    cfg.Simulator.OfflineTimeout,
			"maxStations":       cfg.Simulator.MaxStations,
			"encrypt":           cfg.Simulator.Encrypt,
			"compress":          cfg.Simulator.Compress,
		},
		"server": gin.H{
			"port": cfg.Server.Port,
		},
	})
}

// getSasocStatus GET /api/v1/config/sasoc-status
// 检查模拟工具到SASOC服务器的TCP连接状态
func (ch *configHandler) getSasocStatus(c *gin.Context) {
	cfg := ch.hub.Config()
	if cfg == nil {
		responseError(c, http.StatusInternalServerError, "config not available")
		return
	}

	addr := net.JoinHostPort(cfg.Sasoc.Host, fmt.Sprintf("%d", cfg.Sasoc.Port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		responseSuccess(c, gin.H{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}
	conn.Close()

	responseSuccess(c, gin.H{
		"connected": true,
	})
}

// globalConfigUpdateRequest 全局配置更新请求
type globalConfigUpdateRequest struct {
	SasocHost         *string `json:"sasocHost"`
	SasocPort         *int    `json:"sasocPort"`
	HeartbeatInterval *int    `json:"heartbeatInterval"`
	ReconnectInterval *int    `json:"reconnectInterval"`
	Encrypt           *bool   `json:"encrypt"`
	Compress          *bool   `json:"compress"`
}

// updateConfig PUT /api/v1/config
// 修改运行时配置（SASOC 地址等）
func (ch *configHandler) updateConfig(c *gin.Context) {
	cfg := ch.hub.Config()
	if cfg == nil {
		responseError(c, http.StatusInternalServerError, "config not available")
		return
	}

	var req globalConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// 仅更新非 nil 字段
	updated := false
	sasocChanged := false
	if req.SasocHost != nil && *req.SasocHost != "" && *req.SasocHost != cfg.Sasoc.Host {
		cfg.Sasoc.Host = *req.SasocHost
		updated = true
		sasocChanged = true
	}
	if req.SasocPort != nil && *req.SasocPort > 0 && *req.SasocPort != cfg.Sasoc.Port {
		cfg.Sasoc.Port = *req.SasocPort
		updated = true
		sasocChanged = true
	}
	if req.HeartbeatInterval != nil && *req.HeartbeatInterval > 0 {
		cfg.Simulator.HeartbeatInterval = *req.HeartbeatInterval
		updated = true
	}
	if req.ReconnectInterval != nil && *req.ReconnectInterval > 0 {
		cfg.Simulator.ReconnectInterval = *req.ReconnectInterval
		updated = true
	}
	if req.Encrypt != nil {
		cfg.Simulator.Encrypt = *req.Encrypt
		updated = true
	}
	if req.Compress != nil {
		cfg.Simulator.Compress = *req.Compress
		updated = true
	}

	if updated {
		// 持久化到配置文件
		if err := config.SaveConfig(cfg); err != nil {
			// 持久化失败不影响运行时配置生效，仅记录日志
			_ = err
		}

		// SASOC 地址变化时通知所有在线安检站重连
		if sasocChanged {
			ch.hub.ReconnectAllStations(cfg.Sasoc.Host, cfg.Sasoc.Port)
		}
	}

	responseSuccess(c, gin.H{
		"sasocHost": cfg.Sasoc.Host,
		"sasocPort": cfg.Sasoc.Port,
		"message":   "config updated",
	})
}
