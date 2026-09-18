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
	"github.com/usb-simulator/internal/db"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"github.com/usb-simulator/internal/simulator/commands"
	"go.uber.org/zap"
)

// 生产平台地址黑名单：审计数据生成禁止指向生产环境，仅允许独立测试实例
var protectedSasocHosts = []string{
	"192.168.60.162",
}

// auditGenHandler 审计日志批量生成 handler
type auditGenHandler struct {
	hub *hub.Hub
	cfg *config.Config
}

func newAuditGenHandler(h *hub.Hub, cfg *config.Config) *auditGenHandler {
	return &auditGenHandler{hub: h, cfg: cfg}
}

// AuditGenConfig 审计数据生成配置
type AuditGenConfig struct {
	// 通用
	Total        int `json:"total"`        // 目标总条数/周期数
	Rate         int `json:"rate"`         // 生成速率：条/秒（默认 1000，上限 10000）
	StationCount int `json:"stationCount"` // 参与站点数（默认全部在线）

	// 本地模式（CMDID107）
	Mode       string   `json:"mode"`       // "local" | "platform"
	OpTypes    []string `json:"opTypes"`    // 操作类型：insert/remove/scan/kill
	SnPrefix   string   `json:"snPrefix"`   // U 盘 SN 前缀（默认 USB-）
	SnPoolSize int      `json:"snPoolSize"` // SN 池规模（默认 1000）

	// 平台数据模式（CMDID103→104→105）
	OpenGaussHost     string   `json:"openGaussHost"`     // openGauss 地址
	OpenGaussPort     int      `json:"openGaussPort"`     // 端口（默认 5432）
	OpenGaussDB       string   `json:"openGaussDB"`       // 数据库名（默认 sasoc）
	OpenGaussSchema   string   `json:"openGaussSchema"`   // schema（默认 soc）
	OpenGaussUser     string   `json:"openGaussUser"`     // 用户名
	OpenGaussPassword string   `json:"openGaussPassword"` // 密码
	DeviceSNs         []string `json:"deviceSNs"`         // 用户指定的 SN 列表（优先）
}

// AuditGenStats 审计数据生成统计
type AuditGenStats struct {
	Running    bool   `json:"running"`
	Total      int64  `json:"total"`
	Sent       int64  `json:"sent"`       // 已发送（local=107条数; platform=105周期数）
	ClaimSent  int64  `json:"claimSent"`  // 平台模式：已发 CMDID104 数
	ReturnSent int64  `json:"returnSent"` // 平台模式：已发 CMDID105 数
	Errors     int64  `json:"errors"`
	Rate       int64  `json:"rate"`       // 平均速率：条/秒
	RemainingS int64  `json:"remainingS"` // 预计剩余秒数
	StartTime  string `json:"startTime"`
	Elapsed    int64  `json:"elapsed"`
	EndTime    string `json:"endTime"`
	Mode       string `json:"mode"` // 当前模式
}

// auditGenManager 全局审计生成管理器
type auditGenManager struct {
	mu         sync.Mutex
	running    atomic.Bool
	stopCh     chan struct{}
	sent       atomic.Int64
	claimSent  atomic.Int64
	returnSent atomic.Int64
	errs       atomic.Int64
	total      atomic.Int64
	rate       int64
	startTime  time.Time
	endTime    time.Time
	startStr   string
	mode       string
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

	// 生产环境保护
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
	if req.Mode == "" {
		req.Mode = "local"
	}
	req.Mode = strings.ToLower(req.Mode)

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
	mgr.claimSent.Store(0)
	mgr.returnSent.Store(0)
	mgr.errs.Store(0)
	mgr.total.Store(int64(req.Total))
	mgr.rate = int64(req.Rate)
	mgr.startTime = time.Now()
	mgr.endTime = time.Time{}
	mgr.startStr = mgr.startTime.Format("2006-01-02 15:04:05")
	mgr.mode = req.Mode
	mgr.running.Store(true)

	if req.Mode == "platform" {
		go runPlatformAuditGen(selected, req)
	} else {
		go runLocalAuditGen(selected, req)
	}

	zap.L().Info("audit generation started",
		zap.String("mode", req.Mode),
		zap.Int64("total", mgr.total.Load()),
		zap.Int("rate", req.Rate),
		zap.Int("stations", count),
	)

	responseSuccess(c, gin.H{
		"message":        "audit generation started",
		"mode":           req.Mode,
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
		ClaimSent:  mgr.claimSent.Load(),
		ReturnSent: mgr.returnSent.Load(),
		Errors:     mgr.errs.Load(),
		Rate:       rate,
		RemainingS: remaining,
		StartTime:  mgr.startStr,
		Elapsed:    elapsed,
		Mode:       mgr.mode,
	}
	if !mgr.endTime.IsZero() {
		stats.EndTime = mgr.endTime.Format("2006-01-02 15:04:05")
	}
	return stats
}

// ==================== 本地模式（CMDID107）====================

func buildSnPool(prefix string, size int) []string {
	pool := make([]string, 0, size)
	for i := 1; i <= size; i++ {
		pool = append(pool, fmt.Sprintf("%s%06d", prefix, i))
	}
	return pool
}

func runLocalAuditGen(stations []*simulator.SimStation, cfg AuditGenConfig) {
	mgr := globalAuditGenMgr

	const tickInterval = 100 * time.Millisecond
	perTick := cfg.Rate / 10
	if perTick < 1 {
		perTick = 1
	}

	if cfg.SnPoolSize <= 0 {
		cfg.SnPoolSize = 1000
	}
	if cfg.SnPrefix == "" {
		cfg.SnPrefix = "USB-"
	}
	if len(cfg.OpTypes) == 0 {
		cfg.OpTypes = []string{commands.OpInsert, commands.OpRemove, commands.OpScan, commands.OpKill}
	}

	snPool := buildSnPool(cfg.SnPrefix, cfg.SnPoolSize)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	idx := 0
	for {
		if mgr.sent.Load() >= mgr.total.Load() {
			finishAuditGen(mgr)
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
				station := stations[idx%len(stations)]
				idx++
				if !station.IsOnline() {
					continue
				}

				op := cfg.OpTypes[idx%len(cfg.OpTypes)]
				sn := snPool[(idx/len(cfg.OpTypes))%len(snPool)]

				params := map[string]interface{}{
					"operation": op,
					"result":    "success",
					"sn":        sn,
				}
				if op == commands.OpKill {
					params["virusName"] = randomVirusName()
					params["filePath"] = "/media/usb/" + randomFileName()
					params["handleType"] = 2
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

// ==================== 平台数据模式（CMDID103→104→105）====================

func runPlatformAuditGen(stations []*simulator.SimStation, cfg AuditGenConfig) {
	mgr := globalAuditGenMgr

	// 连接参数归一化
	if cfg.OpenGaussPort <= 0 {
		cfg.OpenGaussPort = 5432
	}
	if cfg.OpenGaussDB == "" {
		cfg.OpenGaussDB = "sasoc"
	}
	if cfg.OpenGaussSchema == "" {
		cfg.OpenGaussSchema = "soc"
	}

	// 1. 连接 openGauss
	gaussCfg := db.OpenGaussConfig{
		Host:     cfg.OpenGaussHost,
		Port:     cfg.OpenGaussPort,
		Database: cfg.OpenGaussDB,
		Username: cfg.OpenGaussUser,
		Password: cfg.OpenGaussPassword,
		Schema:   cfg.OpenGaussSchema,
	}
	client, err := db.NewOpenGaussClient(gaussCfg)
	if err != nil {
		zap.L().Error("openGauss connect failed", zap.Error(err))
		mgr.errs.Add(mgr.total.Load())
		finishAuditGen(mgr)
		return
	}
	defer client.Close()

	// 2. 获取已收录的 SN 列表
	var sns []string
	if len(cfg.DeviceSNs) > 0 {
		for _, sn := range cfg.DeviceSNs {
			ok, err := client.QueryDeviceBySN(sn)
			if err != nil {
				zap.L().Warn("query device failed", zap.String("sn", sn), zap.Error(err))
				continue
			}
			if ok {
				sns = append(sns, sn)
			} else {
				zap.L().Warn("device not received", zap.String("sn", sn))
			}
		}
	} else {
		sns, err = client.QueryReceivedDevices(1000) // 默认取已收录设备做轮询池
		if err != nil {
			zap.L().Error("query devices failed", zap.Error(err))
			mgr.errs.Add(mgr.total.Load())
			finishAuditGen(mgr)
			return
		}
	}

	if len(sns) == 0 {
		zap.L().Error("no received devices found, please check openGauss config or input SN list")
		mgr.errs.Add(mgr.total.Load())
		finishAuditGen(mgr)
		return
	}

	zap.L().Info("platform audit gen ready", zap.Int("deviceCount", len(sns)))

	// 3. 发送循环：每个周期 = INSERT 新申领记录 → CMDID103 → 104 → 105
	//    平台侧 103 只校验不消费；104 领取（条件更新 apply RECEIVED，写类型4审计）；
	//    105 归还（apply RETURNED、device 恢复已收录，写类型5审计）。
	//    申领记录一次性，故每周期必须新插一条（getApplyTimeWindow 保证时间窗覆盖当前）。
	const tickInterval = 100 * time.Millisecond
	perTick := cfg.Rate / 10
	if perTick < 1 {
		perTick = 1
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	cycleIdx := 0
	for {
		if mgr.sent.Load() >= mgr.total.Load() {
			finishAuditGen(mgr)
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

				// 轮询选择站点和 SN
				station := stations[cycleIdx%len(stations)]
				sn := sns[cycleIdx%len(sns)]
				cycleIdx++

				if !station.IsOnline() {
					continue
				}

				// a. 为新周期生成申领码并插入申领记录（status=0, use_count=0）
				code := db.GenerateApplyCode()
				startTime, endTime := applyTimeWindow(time.Now())
				if err := client.InsertApplyRecord(sn, code, "AuditGen", "AUDIT001", "1", startTime, endTime); err != nil {
					mgr.errs.Add(1)
					zap.L().Warn("insert apply record failed", zap.String("sn", sn), zap.Error(err))
					continue
				}

				// b. CMDID103 申领码验证（只校验，写策略审计）
				if err := commands.SendCommand(station, protocol.CmdClaimVerify, map[string]interface{}{
					"applyCode": code,
				}); err != nil {
					mgr.errs.Add(1)
					continue
				}

				// c. CMDID104 U盘领取（写类型4审计）
				if err := commands.SendCommand(station, protocol.CmdUsbClaim, map[string]interface{}{
					"applyCode": code,
					"sn":        sn,
					"result":    "success",
				}); err != nil {
					mgr.errs.Add(1)
					continue
				}
				mgr.claimSent.Add(1)

				// d. CMDID105 U盘归还（写类型5审计）
				if err := commands.SendCommand(station, protocol.CmdUsbReturn, map[string]interface{}{
					"sn": sn,
				}); err != nil {
					mgr.errs.Add(1)
					continue
				}
				mgr.returnSent.Add(1)
				mgr.sent.Add(1) // 一个完整周期计 1 条（平台侧类型4+类型5 各 +1，另策略审计 3 条）
			}
		}
	}
}

// applyTimeWindow 申领码有效时间窗：上一小时 ~ 未来7天，保证当前时间始终在窗内
func applyTimeWindow(now time.Time) (time.Time, time.Time) {
	return now.Add(-1 * time.Hour), now.Add(7 * 24 * time.Hour)
}

func finishAuditGen(mgr *auditGenManager) {
	mgr.mu.Lock()
	mgr.running.Store(false)
	mgr.endTime = time.Now()
	if mgr.stopCh != nil {
		close(mgr.stopCh)
		mgr.stopCh = nil
	}
	mgr.mu.Unlock()
	zap.L().Info("audit generation finished",
		zap.String("mode", mgr.mode),
		zap.Int64("sent", mgr.sent.Load()),
		zap.Int64("claimSent", mgr.claimSent.Load()),
		zap.Int64("returnSent", mgr.returnSent.Load()),
		zap.Int64("errors", mgr.errs.Load()),
		zap.Duration("elapsed", time.Since(mgr.startTime)))
}
