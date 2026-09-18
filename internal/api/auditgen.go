package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"github.com/usb-simulator/internal/simulator/commands"
	"go.uber.org/zap"
)

// 生产平台地址黑名单：审计数据生成禁止指向生产环境，仅允许独立测试实例
var protectedSasocHosts = []string{
	"192.168.123.24",
	"192.168.60.162",
}

// auditGenHandler 审计日志批量生成 handler
// 平台侧 CMDID107 操作日志（insert/remove/scan/kill 且必带 sn）经
// UsbStationLifecycleHandler.operationToAuditType 映射为审计类型 8/9/12/13，
// 由 UsbStationRegistrationHandler.writeAuditLog 写入 wl_usb_audit_log，
// 同时写一条 wl_usb_strategy_audit 安检站策略审计（见平台源码 493-514 行）。
// 故本工具每成功发送 1 条 107 操作日志，平台侧两张审计表各 +1。
type auditGenHandler struct {
	hub *hub.Hub
	cfg *config.Config
}

func newAuditGenHandler(h *hub.Hub, cfg *config.Config) *auditGenHandler {
	return &auditGenHandler{hub: h, cfg: cfg}
}

// AuditGenConfig 审计数据生成配置
type AuditGenConfig struct {
	Total        int      `json:"total"`        // 目标总条数（默认 1000000）
	Rate         int      `json:"rate"`         // 生成速率：条/秒（默认 1000，上限 10000）
	StationCount int      `json:"stationCount"` // 参与站点数（默认全部在线）
	OpTypes      []string `json:"opTypes"`      // 操作类型：insert/remove/scan/kill（为空=全部）
	SnPrefix     string   `json:"snPrefix"`     // U 盘 SN 前缀（默认 USB-）
	SnPoolSize   int      `json:"snPoolSize"`   // SN 池规模（默认 1000）
}

// AuditGenStats 审计数据生成统计
type AuditGenStats struct {
	Running    bool   `json:"running"`
	Total      int64  `json:"total"`
	Sent       int64  `json:"sent"`
	Errors     int64  `json:"errors"`
	Rate       int64  `json:"rate"`       // 平均速率：条/秒
	RemainingS int64  `json:"remainingS"` // 预计剩余秒数（按平均速率）
	StartTime  string `json:"startTime"`
	Elapsed    int64  `json:"elapsed"`
	EndTime    string `json:"endTime"`
}

// auditGenManager 全局审计生成管理器
type auditGenManager struct {
	mu        sync.Mutex
	running   atomic.Bool
	stopCh    chan struct{}
	sent      atomic.Int64
	errs      atomic.Int64
	total     atomic.Int64
	rate      int64
	startTime time.Time
	endTime   time.Time
	startStr  string
}

var globalAuditGenMgr = &auditGenManager{}

// isProtectedSasocHost 校验目标地址是否落在生产平台黑名单
func isProtectedSasocHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	for _, p := range protectedSasocHosts {
		if strings.Contains(h, p) {
			return true
		}
	}
	return false
}

// startAuditGen POST /api/v1/auditgen/start
func (ag *auditGenHandler) startAuditGen(c *gin.Context) {
	var req AuditGenConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	mgr := globalAuditGenMgr
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.running.Load() {
		responseError(c, http.StatusConflict, "audit generation is already running")
		return
	}

	// 生产环境保护：目标地址为生产平台时拒绝启动
	if ag.cfg != nil && isProtectedSasocHost(ag.cfg.Sasoc.Host) {
		responseError(c, http.StatusBadRequest,
			fmt.Sprintf("refused: target sasoc host %s is a production platform, please config a test instance", ag.cfg.Sasoc.Host))
		return
	}

	// 参数归一化
	if req.Total <= 0 {
		req.Total = 1000000
	}
	if req.Rate <= 0 {
		req.Rate = 1000
	}
	if req.Rate > 10000 {
		req.Rate = 10000
	}
	if req.SnPoolSize <= 0 {
		req.SnPoolSize = 1000
	}
	if req.SnPoolSize > 100000 {
		req.SnPoolSize = 100000
	}
	if req.SnPrefix == "" {
		req.SnPrefix = "USB-"
	}
	if len(req.OpTypes) == 0 {
		req.OpTypes = []string{commands.OpInsert, commands.OpRemove, commands.OpScan, commands.OpKill}
	}
	for _, op := range req.OpTypes {
		if !commands.IsValidOperation(op) {
			responseError(c, http.StatusBadRequest, "invalid operation type: "+op)
			return
		}
	}

	onlineStations := ag.hub.ListStationsByStatus(simulator.StateOnline)
	if len(onlineStations) == 0 {
		responseError(c, http.StatusBadRequest, "no online stations available, please start stations first")
		return
	}

	count := req.StationCount
	if count > len(onlineStations) {
		count = len(onlineStations)
	}
	if count <= 0 {
		count = len(onlineStations)
	}
	selected := onlineStations[:count]

	mgr.stopCh = make(chan struct{})
	mgr.sent.Store(0)
	mgr.errs.Store(0)
	mgr.total.Store(int64(req.Total))
	mgr.rate = int64(req.Rate)
	mgr.startTime = time.Now()
	mgr.endTime = time.Time{}
	mgr.startStr = mgr.startTime.Format("2006-01-02 15:04:05")
	mgr.running.Store(true)

	go runAuditGen(selected, req)

	zap.L().Info("audit generation started",
		zap.Int64("total", mgr.total.Load()),
		zap.Int("rate", req.Rate),
		zap.Int("stations", count),
		zap.Any("opTypes", req.OpTypes),
		zap.String("snPrefix", req.SnPrefix),
		zap.Int("snPoolSize", req.SnPoolSize),
	)

	responseSuccess(c, gin.H{
		"message":        "audit generation started",
		"total":          req.Total,
		"rate":           req.Rate,
		"stationCount":   count,
		"onlineStations": len(onlineStations),
	})
}

// stopAuditGen POST /api/v1/auditgen/stop
func (ag *auditGenHandler) stopAuditGen(c *gin.Context) {
	mgr := globalAuditGenMgr
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if !mgr.running.Load() {
		responseError(c, http.StatusBadRequest, "no audit generation is running")
		return
	}
	mgr.running.Store(false)
	mgr.endTime = time.Now()
	if mgr.stopCh != nil {
		close(mgr.stopCh)
		mgr.stopCh = nil
	}

	responseSuccess(c, gin.H{
		"message": "audit generation stopped",
		"stats":   auditGenStatsInternal(),
	})
}

// getAuditGenStats GET /api/v1/auditgen/stats
func (ag *auditGenHandler) getAuditGenStats(c *gin.Context) {
	responseSuccess(c, auditGenStatsInternal())
}

func auditGenStatsInternal() AuditGenStats {
	mgr := globalAuditGenMgr
	sent := mgr.sent.Load()
	elapsed := int64(0)
	if !mgr.startTime.IsZero() {
		elapsed = int64(time.Since(mgr.startTime).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
	}
	rate := int64(0)
	if elapsed > 0 {
		rate = sent / elapsed
	}
	remaining := int64(0)
	if rate > 0 && sent < mgr.total.Load() {
		remaining = (mgr.total.Load() - sent) / rate
	}

	stats := AuditGenStats{
		Running:    mgr.running.Load(),
		Total:      mgr.total.Load(),
		Sent:       sent,
		Errors:     mgr.errs.Load(),
		Rate:       rate,
		RemainingS: remaining,
		StartTime:  mgr.startStr,
		Elapsed:    elapsed,
	}
	if !mgr.endTime.IsZero() {
		stats.EndTime = mgr.endTime.Format("2006-01-02 15:04:05")
	}
	return stats
}

// buildSnPool 生成本地 U 盘 SN 池（工具侧数据，无需从平台获取）
func buildSnPool(prefix string, size int) []string {
	pool := make([]string, 0, size)
	for i := 1; i <= size; i++ {
		pool = append(pool, fmt.Sprintf("%s%06d", prefix, i))
	}
	return pool
}

// runAuditGen 批量发送 CMDID107 操作日志，直至达到目标条数或被停止
func runAuditGen(stations []*simulator.SimStation, cfg AuditGenConfig) {
	mgr := globalAuditGenMgr

	// 批量模式：每 100ms 一个 tick，每 tick 发 rate/10 条，速率平滑且精度稳定
	const tickInterval = 100 * time.Millisecond
	perTick := cfg.Rate / 10
	if perTick < 1 {
		perTick = 1
	}

	snPool := buildSnPool(cfg.SnPrefix, cfg.SnPoolSize)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	idx := 0 // 站点/类型/SN 轮询游标（取各池模值保证分布）
	for {
		// 达到目标条数：自动停止
		if mgr.sent.Load() >= mgr.total.Load() {
			mgr.mu.Lock()
			mgr.running.Store(false)
			mgr.endTime = time.Now()
			if mgr.stopCh != nil {
				close(mgr.stopCh)
				mgr.stopCh = nil
			}
			mgr.mu.Unlock()
			zap.L().Info("audit generation finished",
				zap.Int64("sent", mgr.sent.Load()),
				zap.Int64("errors", mgr.errs.Load()),
				zap.Duration("elapsed", time.Since(mgr.startTime)))
			return
		}

		select {
		case <-mgr.stopCh:
			return
		case <-ticker.C:
			for i := 0; i < perTick; i++ {
				if mgr.sent.Load() >= mgr.total.Load() {
					break
				}
				// 轮询选择在线站点
				station := stations[idx%len(stations)]
				idx++
				if !station.IsOnline() {
					continue
				}

				// 轮询选择操作类型（保证 8/9/12/13 各类分布）
				op := cfg.OpTypes[idx%len(cfg.OpTypes)]
				sn := snPool[(idx/len(cfg.OpTypes))%len(snPool)]

				params := map[string]interface{}{
					"operation": op,
					"result":    "success",
					"sn":        sn,
				}
				// kill 补充病毒字段，贴近真实查毒/杀毒日志
				if op == commands.OpKill {
					params["virusName"] = randomVirusName()
					params["filePath"] = "/media/usb/" + randomFileName()
					params["handleType"] = 2 // 隔离
				}

				if err := commands.SendCommand(station, protocol.CmdOperationLog, params); err != nil {
					mgr.errs.Add(1)
				} else {
					mgr.sent.Add(1)
				}
			}
		}
	}
}
