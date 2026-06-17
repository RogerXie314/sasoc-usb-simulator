package api

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// pressureHandler 压力测试 API 处理器
type pressureHandler struct {
	hub    *hub.Hub
	mu     sync.Mutex
	active int64 // atomic: 0 = idle, >0 = running
	testID int64
	cancel chan struct{}

	// 统计数据
	stats pressureStats
}

// pressureStats 压力测试统计数据
type pressureStats struct {
	TestID               int64     `json:"testId"`
	StartTime            time.Time `json:"startTime"`
	OnlineCount          int       `json:"onlineCount"`
	HeartbeatTotal       int64     `json:"heartbeatTotal"`
	HeartbeatSuccess     int64     `json:"heartbeatSuccess"`
	HeartbeatSuccessRate float64   `json:"heartbeatSuccessRate"`
	AvgLatencyMs         float64   `json:"avgLatencyMs"`
	Throughput           float64   `json:"throughput"` // msg/sec
	TotalLatencyMs       int64     `json:"-"`
	Running              bool      `json:"running"`
}

// newPressureHandler 创建压力测试处理器
func newPressureHandler(h *hub.Hub) *pressureHandler {
	return &pressureHandler{
		hub:    h,
		cancel: make(chan struct{}),
	}
}

// startPressureRequest 启动压测请求
type startPressureRequest struct {
	StationCount      int  `json:"stationCount"`
	HeartbeatInterval int  `json:"heartbeatInterval"` // 秒
	Duration          int  `json:"duration"`           // 秒，0=无限
	SasocHost         string `json:"sasocHost"`
	SasocPort         int  `json:"sasocPort"`
	Encrypt           bool `json:"encrypt"`
	Compress          bool `json:"compress"`
}

// startPressure POST /api/v1/pressure/start
func (ph *pressureHandler) startPressure(c *gin.Context) {
	// 检查是否已有压测在运行
	if !atomic.CompareAndSwapInt64(&ph.active, 0, 1) {
		responseError(c, http.StatusConflict, "pressure test already running")
		return
	}

	var req startPressureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		atomic.StoreInt64(&ph.active, 0)
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// 默认值
	if req.StationCount <= 0 {
		req.StationCount = 10
	}
	if req.HeartbeatInterval <= 0 {
		req.HeartbeatInterval = 30
	}

	ph.mu.Lock()
	ph.testID++
	ph.cancel = make(chan struct{})
	ph.stats = pressureStats{
		TestID:    ph.testID,
		StartTime: time.Now(),
		Running:   true,
	}
	ph.mu.Unlock()

	// 启动压测 goroutine
	go ph.runPressureTest(req)

	zap.L().Info("pressure test started",
		zap.Int64("testId", ph.testID),
		zap.Int("stationCount", req.StationCount),
	)

	responseSuccess(c, gin.H{
		"testId":       ph.testID,
		"stationCount": req.StationCount,
		"message":      "pressure test started",
	})
}

// stopPressure POST /api/v1/pressure/stop
func (ph *pressureHandler) stopPressure(c *gin.Context) {
	if atomic.LoadInt64(&ph.active) == 0 {
		responseError(c, http.StatusConflict, "no pressure test running")
		return
	}

	// 发送停止信号
	close(ph.cancel)
	atomic.StoreInt64(&ph.active, 0)

	ph.mu.Lock()
	ph.stats.Running = false
	ph.mu.Unlock()

	zap.L().Info("pressure test stopped", zap.Int64("testId", ph.testID))

	responseSuccess(c, gin.H{
		"testId":  ph.testID,
		"message": "pressure test stopped",
	})
}

// getStatus GET /api/v1/pressure/status
func (ph *pressureHandler) getStatus(c *gin.Context) {
	ph.mu.Lock()
	stats := ph.stats
	ph.mu.Unlock()

	// 计算实时指标
	if stats.Running {
		stats.OnlineCount = ph.countOnlineStations()
		if stats.HeartbeatTotal > 0 {
			stats.HeartbeatSuccessRate = float64(stats.HeartbeatSuccess) / float64(stats.HeartbeatTotal) * 100
		}
		if stats.HeartbeatTotal > 0 {
			stats.AvgLatencyMs = float64(stats.TotalLatencyMs) / float64(stats.HeartbeatTotal)
		}
		elapsed := time.Since(stats.StartTime).Seconds()
		if elapsed > 0 {
			stats.Throughput = float64(stats.HeartbeatTotal) / elapsed
		}
	}

	responseSuccess(c, stats)
}

// getReport GET /api/v1/pressure/report
func (ph *pressureHandler) getReport(c *gin.Context) {
	ph.mu.Lock()
	stats := ph.stats
	ph.mu.Unlock()

	// 最终统计
	if stats.HeartbeatTotal > 0 {
		stats.HeartbeatSuccessRate = float64(stats.HeartbeatSuccess) / float64(stats.HeartbeatTotal) * 100
		stats.AvgLatencyMs = float64(stats.TotalLatencyMs) / float64(stats.HeartbeatTotal)
	}
	elapsed := time.Since(stats.StartTime).Seconds()
	if elapsed > 0 {
		stats.Throughput = float64(stats.HeartbeatTotal) / elapsed
	}

	stations := ph.hub.ListStations()
	type stationSummary struct {
		ID         string `json:"stationId"`
		Status     string `json:"status"`
		MsgSent    int64  `json:"msgSent"`
		MsgRecv    int64  `json:"msgReceived"`
	}

	summaries := make([]stationSummary, 0, len(stations))
	onlineCount := 0
	for _, s := range stations {
		status := string(s.GetState())
		if s.IsOnline() {
			onlineCount++
		}
		summaries = append(summaries, stationSummary{
			ID:      s.ID,
			Status:  status,
			MsgSent: s.MsgSent,
			MsgRecv: s.MsgReceived,
		})
	}

	responseSuccess(c, gin.H{
		"testId":               stats.TestID,
		"startTime":            stats.StartTime,
		"running":              stats.Running,
		"totalStations":        len(stations),
		"onlineCount":          onlineCount,
		"heartbeatTotal":       stats.HeartbeatTotal,
		"heartbeatSuccess":     stats.HeartbeatSuccess,
		"heartbeatSuccessRate": stats.HeartbeatSuccessRate,
		"avgLatencyMs":         stats.AvgLatencyMs,
		"throughput":           stats.Throughput,
		"stations":             summaries,
	})
}

// runPressureTest 运行压力测试
func (ph *pressureHandler) runPressureTest(req startPressureRequest) {
	defer atomic.StoreInt64(&ph.active, 0)

	zap.L().Info("creating pressure test stations",
		zap.Int("count", req.StationCount),
	)

	// 创建模拟安检站
	stations := make([]*simulator.SimStation, 0, req.StationCount)
	for i := 0; i < req.StationCount; i++ {
		id := fmt.Sprintf("pressure-%d-%d", ph.testID, i)
		sn := fmt.Sprintf("SN-PRESS-%04d", i)
		name := fmt.Sprintf("压测站点-%d", i)

		s := simulator.NewSimStation(id, sn, "SASOC-M100", "v2.0.0", name)
		s.HeartbeatInterval = req.HeartbeatInterval
		s.HeartbeatEnabled = true
		s.EncryptEnabled = req.Encrypt
		s.CompressEnabled = req.Compress

		if req.SasocHost != "" {
			s.SasocHost = req.SasocHost
		}
		if req.SasocPort > 0 {
			s.SasocPort = req.SasocPort
		}

		if err := ph.hub.AddStation(s); err != nil {
			zap.L().Warn("failed to add pressure station",
				zap.String("stationId", id),
				zap.Error(err),
			)
			continue
		}
		stations = append(stations, s)
	}

	// 启动所有站点
	for _, s := range stations {
		go func(station *simulator.SimStation) {
			if err := station.Start(); err != nil {
				zap.L().Warn("pressure station start failed",
					zap.String("stationId", station.ID),
					zap.Error(err),
				)
			}
		}(s)
	}

	// 定时采集指标
	collectTicker := time.NewTicker(5 * time.Second)
	defer collectTicker.Stop()

	// 设置超时
	var timeout <-chan time.Time
	if req.Duration > 0 {
		timeout = time.After(time.Duration(req.Duration) * time.Second)
	}

	for {
		select {
		case <-ph.cancel:
			ph.cleanup(stations)
			return
		case <-timeout:
			ph.cleanup(stations)
			return
		case <-collectTicker.C:
			ph.collectMetrics(stations)
		}
	}
}

// collectMetrics 采集压测指标
func (ph *pressureHandler) collectMetrics(stations []*simulator.SimStation) {
	onlineCount := 0
	var totalSent, totalRecv int64

	for _, s := range stations {
		if s.IsOnline() {
			onlineCount++
		}
		totalSent += s.MsgSent
		totalRecv += s.MsgReceived
	}

	ph.mu.Lock()
	ph.stats.OnlineCount = onlineCount
	ph.stats.HeartbeatTotal = totalSent
	ph.stats.HeartbeatSuccess = totalRecv
	ph.stats.TotalLatencyMs = totalSent * 10 // 模拟平均延迟
	ph.mu.Unlock()

	// 发布压测指标事件
	ph.hub.NotifyPressureMetric(hub.PressureMetricPayload{
		TestID:               ph.testID,
		OnlineCount:          onlineCount,
		HeartbeatSuccessRate: ph.calcSuccessRate(),
		AvgLatencyMs:         ph.calcAvgLatency(),
		Throughput:           ph.calcThroughput(),
	})
}

// cleanup 清理压测站点
func (ph *pressureHandler) cleanup(stations []*simulator.SimStation) {
	ph.mu.Lock()
	ph.stats.Running = false
	ph.mu.Unlock()

	for _, s := range stations {
		_ = s.Stop()
		_ = ph.hub.RemoveStation(s.ID)
	}

	zap.L().Info("pressure test cleanup completed",
		zap.Int64("testId", ph.testID),
		zap.Int("stationCount", len(stations)),
	)
}

// countOnlineStations 统计在线站点数
func (ph *pressureHandler) countOnlineStations() int {
	stations := ph.hub.ListStations()
	count := 0
	for _, s := range stations {
		if s.IsOnline() {
			count++
		}
	}
	return count
}

// calcSuccessRate 计算心跳成功率
func (ph *pressureHandler) calcSuccessRate() float64 {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	if ph.stats.HeartbeatTotal == 0 {
		return 0
	}
	return float64(ph.stats.HeartbeatSuccess) / float64(ph.stats.HeartbeatTotal) * 100
}

// calcAvgLatency 计算平均延迟
func (ph *pressureHandler) calcAvgLatency() float64 {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	if ph.stats.HeartbeatTotal == 0 {
		return 0
	}
	return float64(ph.stats.TotalLatencyMs) / float64(ph.stats.HeartbeatTotal)
}

// calcThroughput 计算吞吐量
func (ph *pressureHandler) calcThroughput() float64 {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	elapsed := time.Since(ph.stats.StartTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(ph.stats.HeartbeatTotal) / elapsed
}
