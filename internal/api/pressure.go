package api

import (
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"github.com/usb-simulator/internal/simulator/commands"
	"go.uber.org/zap"
)

// pressureHandler 压测管理 handler
type pressureHandler struct {
	hub *hub.Hub
}

func newPressureHandler(h *hub.Hub) *pressureHandler {
	return &pressureHandler{hub: h}
}

// PressureTestConfig 压测配置
type PressureTestConfig struct {
	StationCount  int      `json:"stationCount"`
	AlarmTypes    []string `json:"alarmTypes"`
	LogTypes      []string `json:"logTypes"`
	Frequency     int      `json:"frequency"`
	Duration      int      `json:"duration"`
	IncludeAlarms bool     `json:"includeAlarms"`
	IncludeLogs   bool     `json:"includeLogs"`
}

// PressureTestStats 压测统计
type PressureTestStats struct {
	Running      bool   `json:"running"`
	StationCount int    `json:"stationCount"`
	AlarmsSent   int64  `json:"alarmsSent"`
	LogsSent     int64  `json:"logsSent"`
	Errors       int64  `json:"errors"`
	StartTime    string `json:"startTime"`
	Elapsed      int64  `json:"elapsed"`
}

// pressureManager 全局压测管理器实例
type pressureManager struct {
	mu         sync.Mutex
	running    atomic.Bool
	stopCh     chan struct{}
	alarmsSent atomic.Int64
	logsSent   atomic.Int64
	errors     atomic.Int64
	startTime  time.Time
	endTime    time.Time
	stationCnt int
	startStr   string
}

var globalPressureMgr = &pressureManager{}

// 随机数据池
var (
	virusNames = []string{
		"Trojan.Generic", "Worm.AutoRun", "Backdoor.Agent",
		"Malware.Win32", "Trojan.Stealer", "Adware.Bundler",
		"Rootkit.Necurs", "Ransom.Crypto", "Spyware.Keylogger",
		"Virus.Poly",
	}
	virusTypesList = []string{"trojan", "worm", "backdoor", "adware", "rootkit", "ransomware", "spyware"}
	fileNamesList  = []string{
		"unknown.exe", "autorun.inf", "temp.dll", "svchost.exe",
		"config.sys", "setup.exe", "update.exe", "service.dll",
	}
	usbSNPool = []string{
		"USB-001", "USB-002", "USB-003", "USB-004", "USB-005",
		"USB-006", "USB-007", "USB-008", "USB-009", "USB-010",
	}
	opResultsList = []string{"success", "success", "success", "success", "fail"}
)

func randomVirusName() string { return virusNames[rand.Intn(len(virusNames))] }
func randomVirusType() string { return virusTypesList[rand.Intn(len(virusTypesList))] }
func randomFileName() string  { return fileNamesList[rand.Intn(len(fileNamesList))] }
func randomUsbSN() string     { return usbSNPool[rand.Intn(len(usbSNPool))] }
func randomOpResult() string  { return opResultsList[rand.Intn(len(opResultsList))] }
func randomFileHash() string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, 32)
	for i := range b {
		b[i] = hexChars[rand.Intn(len(hexChars))]
	}
	return string(b)
}

// startPressure POST /api/v1/pressure/start
func (ph *pressureHandler) startPressure(c *gin.Context) {
	var req PressureTestConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	mgr := globalPressureMgr
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.running.Load() {
		responseError(c, http.StatusConflict, "pressure test is already running")
		return
	}

	onlineStations := ph.hub.ListStationsByStatus(simulator.StateOnline)
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

	if req.Frequency <= 0 {
		req.Frequency = 1
	}

	mgr.stopCh = make(chan struct{})
	mgr.alarmsSent.Store(0)
	mgr.logsSent.Store(0)
	mgr.errors.Store(0)
	mgr.startTime = time.Now()
	mgr.endTime = time.Time{}
	mgr.stationCnt = count
	mgr.startStr = mgr.startTime.Format("2006-01-02 15:04:05")
	mgr.running.Store(true)

	go ph.runPressure(selected, req)

	zap.L().Info("pressure test started",
		zap.Int("stations", count),
		zap.Int("frequency", req.Frequency),
		zap.Int("duration", req.Duration),
	)

	responseSuccess(c, gin.H{
		"message":        "pressure test started",
		"stationCount":   count,
		"onlineStations": len(onlineStations),
	})
}

// stopPressure POST /api/v1/pressure/stop
func (ph *pressureHandler) stopPressure(c *gin.Context) {
	mgr := globalPressureMgr
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if !mgr.running.Load() {
		responseError(c, http.StatusBadRequest, "no pressure test is running")
		return
	}
	mgr.running.Store(false)
	mgr.endTime = time.Now()
	if mgr.stopCh != nil {
		close(mgr.stopCh)
		mgr.stopCh = nil
	}

	responseSuccess(c, gin.H{
		"message": "pressure test stopped",
		"stats":   ph.getStatsInternal(),
	})
}

// getPressureStats GET /api/v1/pressure/stats
func (ph *pressureHandler) getPressureStats(c *gin.Context) {
	responseSuccess(c, ph.getStatsInternal())
}

func (ph *pressureHandler) getStatsInternal() PressureTestStats {
	mgr := globalPressureMgr
	if mgr.running.Load() {
		return PressureTestStats{
			Running:      true,
			StationCount: mgr.stationCnt,
			AlarmsSent:   mgr.alarmsSent.Load(),
			LogsSent:     mgr.logsSent.Load(),
			Errors:       mgr.errors.Load(),
			StartTime:    mgr.startStr,
			Elapsed:      int64(time.Since(mgr.startTime).Seconds()),
		}
	}
	// 任务已结束，返回最终统计（保持显示）
	if !mgr.endTime.IsZero() {
		elapsed := int64(mgr.endTime.Sub(mgr.startTime).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		return PressureTestStats{
			Running:      false,
			StationCount: mgr.stationCnt,
			AlarmsSent:   mgr.alarmsSent.Load(),
			LogsSent:     mgr.logsSent.Load(),
			Errors:       mgr.errors.Load(),
			StartTime:    mgr.startStr,
			Elapsed:      elapsed,
		}
	}
	return PressureTestStats{Running: false}
}

func (ph *pressureHandler) runPressure(stations []*simulator.SimStation, cfg PressureTestConfig) {
	interval := time.Second
	if cfg.Frequency > 0 {
		interval = time.Second / time.Duration(cfg.Frequency)
	}

	var deadline <-chan time.Time
	if cfg.Duration > 0 {
		deadline = time.After(time.Duration(cfg.Duration) * time.Second)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	mgr := globalPressureMgr

	for {
		select {
		case <-mgr.stopCh:
			return
		case <-deadline:
			mgr.mu.Lock()
			mgr.running.Store(false)
			mgr.endTime = time.Now()
			if mgr.stopCh != nil {
				close(mgr.stopCh)
				mgr.stopCh = nil
			}
			mgr.mu.Unlock()
			return
		case <-ticker.C:
			for _, s := range stations {
				if !s.IsOnline() {
					continue
				}

				// 发送告警：遍历所有勾选的告警类型，每种都发一次
				if cfg.IncludeAlarms && len(cfg.AlarmTypes) > 0 {
					for _, alarmType := range cfg.AlarmTypes {
						params := map[string]interface{}{
							"alarmType": alarmType,
							"sn":        s.SN,
						}
						if alarmType == commands.AlarmTypeMalwareDetected {
							params["virusName"] = randomVirusName()
							params["virusType"] = randomVirusType()
							params["fileName"] = randomFileName()
							params["fileHash"] = randomFileHash()
						}
						// 安全U盘告警需要合法 reason 与 doorNo，并按携带规则处理
						if reasons, ok := commands.AlarmTypeReasons[alarmType]; ok {
							reason := reasons[rand.Intn(len(reasons))]
							params["reason"] = reason
							switch reason {
							case 5, 6: // 无可用归还柜位：doorNo="", sn=""
								params["sn"] = ""
								params["doorNo"] = ""
							case 29: // 使用违规：doorNo=""
								params["doorNo"] = ""
							default:
								params["doorNo"] = rand.Intn(24) + 1
							}
						}
						if err := commands.SendCommand(s, protocol.CmdAlarm, params); err != nil {
							mgr.errors.Add(1)
						} else {
							mgr.alarmsSent.Add(1)
						}
					}
				}

				// 发送操作日志：遍历所有勾选的日志类型，每种都发一次
				if cfg.IncludeLogs && len(cfg.LogTypes) > 0 {
					for _, opType := range cfg.LogTypes {
						params := map[string]interface{}{
							"operation": opType,
							"result":    randomOpResult(),
						}
						// 数据摆渡、隔离区导出为无SN介质操作，省略 sn 字段
						if opType != commands.OpCopy && opType != commands.OpQuarantineExport {
							params["sn"] = randomUsbSN()
							// insert/remove 结果固定 success
							if opType == commands.OpInsert || opType == commands.OpRemove {
								params["result"] = "success"
							}
						}
						if err := commands.SendCommand(s, protocol.CmdOperationLog, params); err != nil {
							mgr.errors.Add(1)
						} else {
							mgr.logsSent.Add(1)
						}
					}
				}
			}
		}
	}
}

// formatDuration 格式化持续时间
func formatDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	mins := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%dm%ds", mins, secs)
}
