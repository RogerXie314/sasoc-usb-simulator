package pressure

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// EngineState 压测引擎状态
type EngineState string

const (
	EngineIdle    EngineState = "idle"
	EngineRunning EngineState = "running"
	EngineStopped EngineState = "stopped"
)

// PressureConfig 压测配置
type PressureConfig struct {
	ScenarioName string `json:"scenarioName"`
	StationCount int    `json:"stationCount"`
	SasocHost    string `json:"sasocHost"`
	SasocPort    int    `json:"sasocPort"`
	Registration struct {
		Concurrency int    `json:"concurrency"`
		SNPrefix    string `json:"snPrefix"`
		Model       string `json:"model"`
		Version     string `json:"version"`
		IPRange     string `json:"ipRange"` // e.g., "192.168.2.{index}"
	} `json:"registration"`
	Heartbeat struct {
		Enabled         bool `json:"enabled"`
		IntervalSeconds int  `json:"intervalSeconds"`
		DurationSeconds int  `json:"durationSeconds"`
	} `json:"heartbeat"`
	Metrics struct {
		CollectIntervalSeconds int    `json:"collectIntervalSeconds"`
		ReportFormat           string `json:"reportFormat"`
	} `json:"metrics"`
}

// RegistrationResult 单个站点注册结果
type RegistrationResult struct {
	StationID string
	SN        string
	Success   bool
	Latency   time.Duration
	Error     error
}

// HeartbeatResult 单次心跳结果
type HeartbeatResult struct {
	StationID string
	Success   bool
	Latency   time.Duration
	Error     error
}

// PressureTestEngine 压力测试引擎
type PressureTestEngine struct {
	config  PressureConfig
	hub     *hub.Hub
	state   EngineState
	testID  int64

	stations []*simulator.SimStation

	// 指标采集
	collector  *MetricsCollector
	metricsCh  chan MetricsSnapshot

	// 注册结果通道
	regResultCh chan RegistrationResult

	// 心跳结果通道
	hbResultCh chan HeartbeatResult

	// 生命周期控制
	cancel  context.CancelFunc
	ctx     context.Context
	running int64 // atomic: 0=idle, 1=running

	mu     sync.Mutex
	logger *zap.Logger

	// 时间记录
	startTime time.Time
	stopTime  time.Time
}

// NewPressureTestEngine 创建压测引擎
func NewPressureTestEngine(cfg PressureConfig, h *hub.Hub) *PressureTestEngine {
	return &PressureTestEngine{
		config:     cfg,
		hub:        h,
		state:      EngineIdle,
		collector:  NewMetricsCollector(),
		metricsCh:  make(chan MetricsSnapshot, 256),
		regResultCh: make(chan RegistrationResult, cfg.StationCount),
		hbResultCh:  make(chan HeartbeatResult, cfg.StationCount*10),
		logger:      zap.L(),
	}
}

// Start 启动压测
func (e *PressureTestEngine) Start() error {
	if !atomic.CompareAndSwapInt64(&e.running, 0, 1) {
		return fmt.Errorf("pressure test already running")
	}

	e.mu.Lock()
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.testID = time.Now().UnixMilli()
	e.state = EngineRunning
	e.startTime = time.Now()
	e.stations = make([]*simulator.SimStation, 0, e.config.StationCount)
	e.mu.Unlock()

	// 启动指标聚合 goroutine
	go e.collectLoop()

	// 启动结果消费 goroutine
	go e.consumeResults()

	// 启动站点创建和注册
	go e.createAndRegister()

	// 启动心跳监控（如果配置了心跳持续时间）
	if e.config.Heartbeat.Enabled && e.config.Heartbeat.DurationSeconds > 0 {
		go e.heartbeatDurationTimer()
	}

	e.logger.Info("pressure test engine started",
		zap.String("scenario", e.config.ScenarioName),
		zap.Int("stationCount", e.config.StationCount),
		zap.Int64("testId", e.testID),
	)

	return nil
}

// Stop 停止压测，生成报告
func (e *PressureTestEngine) Stop() (string, error) {
	if atomic.LoadInt64(&e.running) == 0 {
		return "", fmt.Errorf("pressure test not running")
	}

	e.mu.Lock()
	e.cancel()
	e.state = EngineStopped
	e.stopTime = time.Now()
	e.mu.Unlock()

	// 停止所有站点
	e.cleanupStations()

	atomic.StoreInt64(&e.running, 0)

	// 生成报告
	reportPath, err := e.GenerateReport()
	if err != nil {
		e.logger.Error("failed to generate report", zap.Error(err))
		return "", fmt.Errorf("generate report: %w", err)
	}

	e.logger.Info("pressure test engine stopped",
		zap.Int64("testId", e.testID),
		zap.String("reportPath", reportPath),
	)

	return reportPath, nil
}

// Status 获取当前压测状态
func (e *PressureTestEngine) Status() map[string]interface{} {
	e.mu.Lock()
	defer e.mu.Unlock()

	onlineCount := 0
	for _, s := range e.stations {
		if s.IsOnline() {
			onlineCount++
		}
	}

	snapshot := e.collector.LatestSnapshot()

	return map[string]interface{}{
		"testId":       e.testID,
		"scenarioName": e.config.ScenarioName,
		"state":        string(e.state),
		"startTime":    e.startTime,
		"stopTime":     e.stopTime,
		"stationTotal": len(e.stations),
		"onlineCount":  onlineCount,
		"snapshot":     snapshot,
	}
}

// GetCollector 返回指标采集器
func (e *PressureTestEngine) GetCollector() *MetricsCollector {
	return e.collector
}

// createAndRegister 批量创建站点并发注册
func (e *PressureTestEngine) createAndRegister() {
	total := e.config.StationCount
	concurrency := e.config.Registration.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}
	if concurrency > total {
		concurrency = total
	}

	// 创建所有站点实例
	stations := make([]*simulator.SimStation, 0, total)
	for i := 0; i < total; i++ {
		s := e.createStation(i)
		if err := e.hub.AddStation(s); err != nil {
			e.logger.Warn("failed to add station to hub",
				zap.String("stationId", s.ID),
				zap.Error(err),
			)
			// 记录注册失败
			e.regResultCh <- RegistrationResult{
				StationID: s.ID,
				SN:        s.SN,
				Success:   false,
				Error:     err,
			}
			continue
		}
		stations = append(stations, s)

		e.mu.Lock()
		e.stations = append(e.stations, s)
		e.mu.Unlock()
	}

	e.logger.Info("stations created, starting concurrent registration",
		zap.Int("total", len(stations)),
		zap.Int("concurrency", concurrency),
	)

	// 使用 semaphore 控制并发
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, s := range stations {
		wg.Add(1)
		sem <- struct{}{} // acquire

		go func(station *simulator.SimStation) {
			defer wg.Done()
			defer func() { <-sem }() // release

			start := time.Now()
			err := station.Start()
			latency := time.Since(start)

			result := RegistrationResult{
				StationID: station.ID,
				SN:        station.SN,
				Success:   err == nil,
				Latency:   latency,
				Error:     err,
			}

			e.regResultCh <- result

			if err != nil {
				e.logger.Warn("station registration failed",
					zap.String("stationId", station.ID),
					zap.Error(err),
				)
			} else {
				e.logger.Debug("station registered",
					zap.String("stationId", station.ID),
					zap.Duration("latency", latency),
				)
			}
		}(s)
	}

	wg.Wait()
	e.logger.Info("all station registrations completed")
}

// createStation 创建单个模拟站点
func (e *PressureTestEngine) createStation(index int) *simulator.SimStation {
	sn := fmt.Sprintf("%s%04d", e.config.Registration.SNPrefix, index)
	id := fmt.Sprintf("pt-%d-%04d", e.testID, index)
	name := fmt.Sprintf("压测站点-%d", index)

	s := simulator.NewSimStation(id, sn, e.config.Registration.Model, e.config.Registration.Version, name)

	// 设置 SASOC 连接
	if e.config.SasocHost != "" {
		s.SasocHost = e.config.SasocHost
	}
	if e.config.SasocPort > 0 {
		s.SasocPort = e.config.SasocPort
	}

	// 设置 IP
	ip := e.resolveIP(index)
	if ip != "" {
		s.IP = ip
	}

	// 设置心跳
	s.HeartbeatEnabled = e.config.Heartbeat.Enabled
	if e.config.Heartbeat.IntervalSeconds > 0 {
		s.HeartbeatInterval = e.config.Heartbeat.IntervalSeconds
	}

	// 压测模式默认启用加密和压缩
	s.EncryptEnabled = true
	s.CompressEnabled = true

	return s
}

// resolveIP 解析 IP 范围模板
func (e *PressureTestEngine) resolveIP(index int) string {
	tpl := e.config.Registration.IPRange
	if tpl == "" {
		return ""
	}
	return strings.ReplaceAll(tpl, "{index}", fmt.Sprintf("%d", index))
}

// collectLoop 定时采集指标
func (e *PressureTestEngine) collectLoop() {
	interval := e.config.Metrics.CollectIntervalSeconds
	if interval <= 0 {
		interval = 5
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			// 采集最终快照
			e.takeSnapshot()
			return
		case <-ticker.C:
			e.takeSnapshot()
		}
	}
}

// takeSnapshot 采集一次指标快照
func (e *PressureTestEngine) takeSnapshot() {
	e.mu.Lock()
	stations := make([]*simulator.SimStation, len(e.stations))
	copy(stations, e.stations)
	e.mu.Unlock()

	onlineCount := 0
	for _, s := range stations {
		if s.IsOnline() {
			onlineCount++
		}
	}

	snapshot := e.collector.TakeSnapshot(onlineCount, len(stations))

	// 发布指标事件
	e.hub.NotifyPressureMetric(hub.PressureMetricPayload{
		TestID:               e.testID,
		OnlineCount:          snapshot.OnlineCount,
		HeartbeatSuccessRate: safeDiv(float64(snapshot.HeartbeatSuccess), float64(snapshot.HeartbeatTotal)) * 100,
		AvgLatencyMs:         snapshot.AvgHbLatencyMs,
		Throughput:           snapshot.Throughput,
	})

	// 发送到通道（非阻塞）
	select {
	case e.metricsCh <- snapshot:
	default:
		e.logger.Warn("metrics channel full, dropping snapshot")
	}
}

// consumeResults 消费注册和心跳结果
func (e *PressureTestEngine) consumeResults() {
	for {
		select {
		case <-e.ctx.Done():
			return
		case result := <-e.regResultCh:
			if result.Success {
				e.collector.RecordRegistration(result.Latency)
			} else {
				e.collector.RecordRegistrationFail()
			}
		case result := <-e.hbResultCh:
			if result.Success {
				e.collector.RecordHeartbeat(result.Latency)
			} else {
				e.collector.RecordHeartbeatFail()
			}
		}
	}
}

// heartbeatDurationTimer 心跳持续时间计时器
func (e *PressureTestEngine) heartbeatDurationTimer() {
	duration := time.Duration(e.config.Heartbeat.DurationSeconds) * time.Second
	select {
	case <-e.ctx.Done():
		return
	case <-time.After(duration):
		e.logger.Info("heartbeat duration reached, stopping test",
			zap.Int64("testId", e.testID),
		)
		_, _ = e.Stop()
	}
}

// cleanupStations 清理所有站点
func (e *PressureTestEngine) cleanupStations() {
	e.mu.Lock()
	stations := make([]*simulator.SimStation, len(e.stations))
	copy(stations, e.stations)
	e.mu.Unlock()

	var wg sync.WaitGroup
	batchSize := 50
	for i := 0; i < len(stations); i += batchSize {
		end := i + batchSize
		if end > len(stations) {
			end = len(stations)
		}

		for j := i; j < end; j++ {
			wg.Add(1)
			go func(s *simulator.SimStation) {
				defer wg.Done()
				_ = s.Stop()
				_ = e.hub.RemoveStation(s.ID)
			}(stations[j])
		}
	}

	wg.Wait()
	e.logger.Info("all pressure stations cleaned up",
		zap.Int("count", len(stations)),
	)
}


