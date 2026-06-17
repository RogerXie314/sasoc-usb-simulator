package pressure

import (
	"sync"
	"time"
)

// MetricsSnapshot 指标快照
type MetricsSnapshot struct {
	Timestamp           time.Time `json:"timestamp"`
	OnlineCount         int       `json:"onlineCount"`
	RegistrationTotal   int       `json:"registrationTotal"`
	RegistrationSuccess int       `json:"registrationSuccess"`
	RegistrationFail    int       `json:"registrationFail"`
	AvgRegLatencyMs     float64   `json:"avgRegLatencyMs"`
	HeartbeatTotal      int64     `json:"heartbeatTotal"`
	HeartbeatSuccess    int64     `json:"heartbeatSuccess"`
	HeartbeatFail       int64     `json:"heartbeatFail"`
	AvgHbLatencyMs      float64   `json:"avgHbLatencyMs"`
	Throughput          float64   `json:"throughput"`         // registrations or heartbeats per second
	ConnectionDropRate  float64   `json:"connectionDropRate"` // percentage
}

// MetricsCollector 指标采集器（线程安全）
type MetricsCollector struct {
	mu sync.RWMutex

	// 注册指标
	regTotal   int
	regSuccess int
	regFail    int
	regLatency time.Duration // 累计注册延迟

	// 心跳指标
	hbTotal   int64
	hbSuccess int64
	hbFail    int64
	hbLatency time.Duration // 累计心跳延迟

	// 断线追踪
	droppedConnections int64
	peakOnline         int

	// 时间范围
	startTime time.Time

	// 时序数据
	snapshots []MetricsSnapshot
}

// NewMetricsCollector 创建指标采集器
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime: time.Now(),
		snapshots: make([]MetricsSnapshot, 0, 1024),
	}
}

// RecordRegistration 记录一次成功注册
func (mc *MetricsCollector) RecordRegistration(latency time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.regTotal++
	mc.regSuccess++
	mc.regLatency += latency
}

// RecordRegistrationFail 记录一次注册失败
func (mc *MetricsCollector) RecordRegistrationFail() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.regTotal++
	mc.regFail++
}

// RecordHeartbeat 记录一次成功心跳
func (mc *MetricsCollector) RecordHeartbeat(latency time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.hbTotal++
	mc.hbSuccess++
	mc.hbLatency += latency
}

// RecordHeartbeatFail 记录一次心跳失败
func (mc *MetricsCollector) RecordHeartbeatFail() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.hbTotal++
	mc.hbFail++
}

// RecordConnectionDrop 记录一次连接断开
func (mc *MetricsCollector) RecordConnectionDrop() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.droppedConnections++
}

// TakeSnapshot 采集当前指标快照并追加到时序数据
func (mc *MetricsCollector) TakeSnapshot(onlineCount, totalStations int) MetricsSnapshot {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// 更新峰值在线数
	if onlineCount > mc.peakOnline {
		mc.peakOnline = onlineCount
	}

	elapsed := time.Since(mc.startTime).Seconds()

	snapshot := MetricsSnapshot{
		Timestamp:           time.Now(),
		OnlineCount:         onlineCount,
		RegistrationTotal:   mc.regTotal,
		RegistrationSuccess: mc.regSuccess,
		RegistrationFail:    mc.regFail,
		AvgRegLatencyMs:     safeAvgMs(mc.regLatency, mc.regSuccess),
		HeartbeatTotal:      mc.hbTotal,
		HeartbeatSuccess:    mc.hbSuccess,
		HeartbeatFail:       mc.hbFail,
		AvgHbLatencyMs:      safeAvgMs(mc.hbLatency, int(mc.hbSuccess)),
		Throughput:          safeDiv(float64(mc.hbTotal), elapsed),
		ConnectionDropRate:  safeDiv(float64(mc.droppedConnections), float64(totalStations)) * 100,
	}

	mc.snapshots = append(mc.snapshots, snapshot)

	return snapshot
}

// LatestSnapshot 获取最新快照（如果还没有则返回空快照）
func (mc *MetricsCollector) LatestSnapshot() MetricsSnapshot {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if len(mc.snapshots) == 0 {
		return MetricsSnapshot{
			Timestamp: time.Now(),
		}
	}
	return mc.snapshots[len(mc.snapshots)-1]
}

// AllSnapshots 返回所有时序快照
func (mc *MetricsCollector) AllSnapshots() []MetricsSnapshot {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make([]MetricsSnapshot, len(mc.snapshots))
	copy(result, mc.snapshots)
	return result
}

// Summary 返回汇总统计
func (mc *MetricsCollector) Summary() map[string]interface{} {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	elapsed := time.Since(mc.startTime).Seconds()

	return map[string]interface{}{
		"registrationTotal":     mc.regTotal,
		"registrationSuccess":   mc.regSuccess,
		"registrationFail":      mc.regFail,
		"registrationSuccessRate": safeDiv(float64(mc.regSuccess), float64(mc.regTotal)) * 100,
		"avgRegLatencyMs":       safeAvgMs(mc.regLatency, mc.regSuccess),
		"heartbeatTotal":        mc.hbTotal,
		"heartbeatSuccess":      mc.hbSuccess,
		"heartbeatFail":         mc.hbFail,
		"heartbeatSuccessRate":  safeDiv(float64(mc.hbSuccess), float64(mc.hbTotal)) * 100,
		"avgHbLatencyMs":        safeAvgMs(mc.hbLatency, int(mc.hbSuccess)),
		"throughput":            safeDiv(float64(mc.hbTotal), elapsed),
		"droppedConnections":    mc.droppedConnections,
		"peakOnline":            mc.peakOnline,
		"elapsedSeconds":        elapsed,
		"snapshotCount":         len(mc.snapshots),
	}
}

// Reset 重置所有指标
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.regTotal = 0
	mc.regSuccess = 0
	mc.regFail = 0
	mc.regLatency = 0

	mc.hbTotal = 0
	mc.hbSuccess = 0
	mc.hbFail = 0
	mc.hbLatency = 0

	mc.droppedConnections = 0
	mc.peakOnline = 0
	mc.startTime = time.Now()
	mc.snapshots = mc.snapshots[:0]
}

// safeAvgMs 安全计算平均毫秒延迟
func safeAvgMs(total time.Duration, count int) float64 {
	if count <= 0 {
		return 0
	}
	return float64(total.Milliseconds()) / float64(count)
}

// safeDiv 安全除法
func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
