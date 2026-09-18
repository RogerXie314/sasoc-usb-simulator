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
	OpenGaussDB       string   `json:"openGaussDB"`       // 数据库名（默认 wnt）
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
	Mode       string `json:"mode"`                // 当前模式
	LastError  string `json:"lastError,omitempty"` // 最近一次错误原因
}

// auditGenTask 单个审计生成任务
type auditGenTask struct {
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
	lastError  string
}

// auditGenManager 按 mode 管理多个审计生成任务
type auditGenManager struct {
	mu    sync.RWMutex
	tasks map[string]*auditGenTask // key: "local" | "platform"
}

var globalAuditGenMgr = &auditGenManager{
	tasks: make(map[string]*auditGenTask),
}

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

func normalizeAuditGenConfig(req *AuditGenConfig) {
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
}

func selectOnlineStations(ag *auditGenHandler, req AuditGenConfig) ([]*simulator.SimStation, int, error) {
	onlineStations := ag.hub.ListStationsByStatus(simulator.StateOnline)
	if len(onlineStations) == 0 {
		return nil, 0, fmt.Errorf("no online stations available, please start stations first")
	}

	count := req.StationCount
	if count > len(onlineStations) {
		count = len(onlineStations)
	}
	if count <= 0 {
		count = len(onlineStations)
	}
	return onlineStations[:count], len(onlineStations), nil
}

// startAuditGen POST /api/v1/auditgen/start
func (ag *auditGenHandler) startAuditGen(c *gin.Context) {
	var req AuditGenConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	normalizeAuditGenConfig(&req)

	mgr := globalAuditGenMgr
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// 同模式冲突检查
	if t, ok := mgr.tasks[req.Mode]; ok && t.running.Load() {
		responseError(c, http.StatusConflict, "audit generation for mode '"+req.Mode+"' is already running")
		return
	}

	// 生产环境保护
	if ag.cfg != nil && isProtectedSasocHost(ag.cfg.Sasoc.Host) {
		responseError(c, http.StatusBadRequest,
			fmt.Sprintf("refused: target sasoc host %s is a production platform, please config a test instance", ag.cfg.Sasoc.Host))
		return
	}

	selected, onlineCount, err := selectOnlineStations(ag, req)
	if err != nil {
		responseError(c, http.StatusBadRequest, err.Error())
		return
	}

	task := &auditGenTask{
		stopCh:    make(chan struct{}),
		rate:      int64(req.Rate),
		startTime: time.Now(),
		startStr:  time.Now().Format("2006-01-02 15:04:05"),
		mode:      req.Mode,
	}
	task.total.Store(int64(req.Total))
	task.running.Store(true)

	mgr.tasks[req.Mode] = task

	if req.Mode == "platform" {
		go runPlatformAuditGen(selected, req, task)
	} else {
		go runLocalAuditGen(selected, req, task)
	}

	zap.L().Info("audit generation started",
		zap.String("mode", req.Mode),
		zap.Int64("total", task.total.Load()),
		zap.Int("rate", req.Rate),
		zap.Int("stations", len(selected)),
	)

	responseSuccess(c, gin.H{
		"message":        "audit generation started",
		"mode":           req.Mode,
		"total":          req.Total,
		"rate":           req.Rate,
		"stationCount":   len(selected),
		"onlineStations": onlineCount,
	})
}

// stopAuditGen POST /api/v1/auditgen/stop
func (ag *auditGenHandler) stopAuditGen(c *gin.Context) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// 兼容旧版：请求体可能为空，尝试默认停止所有
		req.Mode = ""
	}

	mgr := globalAuditGenMgr
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if req.Mode != "" {
		// 停止指定模式
		task, ok := mgr.tasks[req.Mode]
		if !ok || !task.running.Load() {
			responseError(c, http.StatusBadRequest, "no audit generation is running for mode '"+req.Mode+"'")
			return
		}
		stopTask(task)
		responseSuccess(c, gin.H{
			"message": "audit generation stopped for mode " + req.Mode,
			"stats":   buildAuditGenStats(task),
		})
		return
	}

	// 未指定 mode：停止所有运行中的任务
	stopped := false
	var lastStats AuditGenStats
	for _, task := range mgr.tasks {
		if task.running.Load() {
			stopTask(task)
			lastStats = buildAuditGenStats(task)
			stopped = true
		}
	}
	if !stopped {
		responseError(c, http.StatusBadRequest, "no audit generation is running")
		return
	}
	responseSuccess(c, gin.H{
		"message": "all audit generation stopped",
		"stats":   lastStats,
	})
}

func stopTask(task *auditGenTask) {
	task.running.Store(false)
	task.endTime = time.Now()
	if task.stopCh != nil {
		close(task.stopCh)
		task.stopCh = nil
	}
}

// getAuditGenStats GET /api/v1/auditgen/stats
func (ag *auditGenHandler) getAuditGenStats(c *gin.Context) {
	mgr := globalAuditGenMgr
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	result := make(map[string]AuditGenStats)
	for mode, task := range mgr.tasks {
		result[mode] = buildAuditGenStats(task)
	}
	responseSuccess(c, result)
}

func buildAuditGenStats(task *auditGenTask) AuditGenStats {
	sent := task.sent.Load()
	elapsed := int64(0)
	if !task.startTime.IsZero() {
		elapsed = int64(time.Since(task.startTime).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
	}
	rate := int64(0)
	if elapsed > 0 {
		rate = sent / elapsed
	}
	remaining := int64(0)
	if rate > 0 && sent < task.total.Load() {
		remaining = (task.total.Load() - sent) / rate
	}

	stats := AuditGenStats{
		Running:    task.running.Load(),
		Total:      task.total.Load(),
		Sent:       sent,
		ClaimSent:  task.claimSent.Load(),
		ReturnSent: task.returnSent.Load(),
		Errors:     task.errs.Load(),
		Rate:       rate,
		RemainingS: remaining,
		StartTime:  task.startStr,
		Elapsed:    elapsed,
		Mode:       task.mode,
		LastError:  task.lastError,
	}
	if !task.endTime.IsZero() {
		stats.EndTime = task.endTime.Format("2006-01-02 15:04:05")
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

// resolveSnPool 优先使用用户提供的 SN 列表，否则按前缀自动生成
func resolveSnPool(cfg AuditGenConfig) []string {
	if len(cfg.DeviceSNs) > 0 {
		return cfg.DeviceSNs
	}
	size := cfg.SnPoolSize
	if size <= 0 {
		size = 1000
	}
	prefix := cfg.SnPrefix
	if prefix == "" {
		prefix = "USB-"
	}
	return buildSnPool(prefix, size)
}

func runLocalAuditGen(stations []*simulator.SimStation, cfg AuditGenConfig, task *auditGenTask) {
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

	snPool := resolveSnPool(cfg)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	idx := 0
	for {
		if task.sent.Load() >= task.total.Load() {
			finishTask(task)
			return
		}

		select {
		case <-task.stopCh:
			return
		case <-ticker.C:
			for i := 0; i < perTick; i++ {
				if task.sent.Load() >= task.total.Load() {
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
					task.errs.Add(1)
				} else {
					task.sent.Add(1)
				}
			}
		}
	}
}

// ==================== 平台数据模式（CMDID103→104→105）====================

func runPlatformAuditGen(stations []*simulator.SimStation, cfg AuditGenConfig, task *auditGenTask) {
	// 连接参数归一化
	if cfg.OpenGaussPort <= 0 {
		cfg.OpenGaussPort = 5432
	}
	if cfg.OpenGaussDB == "" {
		cfg.OpenGaussDB = "wnt"
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
		task.lastError = err.Error()
		task.errs.Add(task.total.Load())
		finishTask(task)
		return
	}
	defer client.Close()
	zap.L().Info("openGauss connected", zap.String("host", cfg.OpenGaussHost), zap.Int("port", cfg.OpenGaussPort), zap.String("db", cfg.OpenGaussDB))

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
			task.lastError = err.Error()
			task.errs.Add(task.total.Load())
			finishTask(task)
			return
		}
	}

	if len(sns) == 0 {
		msg := "no received devices found, please check openGauss config or input SN list"
		zap.L().Error(msg)
		task.lastError = msg
		task.errs.Add(task.total.Load())
		finishTask(task)
		return
	}

	snsPreview := sns
	if len(sns) > 5 {
		snsPreview = sns[:5]
	}
	zap.L().Info("platform audit gen ready", zap.Int("deviceCount", len(sns)), zap.Strings("sns", snsPreview))

	// 3. 发送循环：每个周期 = INSERT 新申领记录 → CMDID103 → 104 → 105
	const tickInterval = 100 * time.Millisecond
	perTick := cfg.Rate / 10
	if perTick < 1 {
		perTick = 1
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	cycleIdx := 0
	for {
		if task.sent.Load() >= task.total.Load() {
			finishTask(task)
			return
		}

		select {
		case <-task.stopCh:
			return
		case <-ticker.C:
			for i := 0; i < perTick; i++ {
				if task.sent.Load() >= task.total.Load() {
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
					task.errs.Add(1)
					zap.L().Warn("insert apply record failed", zap.String("sn", sn), zap.Error(err))
					continue
				}
				zap.L().Debug("insert apply record ok", zap.String("sn", sn), zap.String("code", code))

				// b. CMDID103 申领码验证（只校验，写策略审计）
				if err := commands.SendCommand(station, protocol.CmdClaimVerify, map[string]interface{}{
					"applyCode": code,
				}); err != nil {
					task.errs.Add(1)
					continue
				}

				// c. CMDID104 U盘领取（写类型4审计）
				if err := commands.SendCommand(station, protocol.CmdUsbClaim, map[string]interface{}{
					"applyCode": code,
					"sn":        sn,
					"result":    "success",
				}); err != nil {
					task.errs.Add(1)
					continue
				}
				task.claimSent.Add(1)

				// d. CMDID105 U盘归还（写类型5审计）
				if err := commands.SendCommand(station, protocol.CmdUsbReturn, map[string]interface{}{
					"sn": sn,
				}); err != nil {
					task.errs.Add(1)
					continue
				}
				task.returnSent.Add(1)
				task.sent.Add(1) // 一个完整周期计 1 条（平台侧类型4+类型5 各 +1，另策略审计 3 条）
			}
		}
	}
}

// applyTimeWindow 申领码有效时间窗：上一小时 ~ 未来7天，保证当前时间始终在窗内
func applyTimeWindow(now time.Time) (time.Time, time.Time) {
	return now.Add(-1 * time.Hour), now.Add(7 * 24 * time.Hour)
}

func finishTask(task *auditGenTask) {
	task.running.Store(false)
	task.endTime = time.Now()
	if task.stopCh != nil {
		close(task.stopCh)
		task.stopCh = nil
	}
	zap.L().Info("audit generation finished",
		zap.String("mode", task.mode),
		zap.Int64("sent", task.sent.Load()),
		zap.Int64("claimSent", task.claimSent.Load()),
		zap.Int64("returnSent", task.returnSent.Load()),
		zap.Int64("errors", task.errs.Load()),
		zap.Duration("elapsed", time.Since(task.startTime)))
}
