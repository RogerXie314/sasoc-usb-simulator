package commands

import (
	"time"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// UpgradeCommand CMDID=108/109 病毒库升级流程
type UpgradeCommand struct{}

func (c *UpgradeCommand) CmdID() uint32 { return protocol.CmdUpgradeIssue }

func (c *UpgradeCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	// CMDID=108 是 S→C 下发，模拟器不需要主动构建
	// 此方法用于模拟下发升级命令的场景
	virusType, _ := params["virusType"].(int)
	version, _ := params["version"].(string)
	downloadUrl, _ := params["downloadUrl"].(string)
	checksum, _ := params["checksum"].(string)

	body := map[string]interface{}{
		"virusType":   virusType,
		"version":     version,
		"downloadUrl": downloadUrl,
		"checksum":    checksum,
	}

	return body, nil
}

func (c *UpgradeCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	var resp map[string]interface{}
	if err := frame.DecodeJSONBody(&resp); err != nil {
		return err
	}

	cmdContent := extractCMDContent(resp)
	code, _ := cmdContent["code"].(float64)

	switch int(code) {
	case protocol.CodeSuccess:
		// 升级命令接收成功，启动升级流程
		virusType, _ := cmdContent["virusType"].(float64)
		version, _ := cmdContent["version"].(string)
		downloadUrl, _ := cmdContent["downloadUrl"].(string)
		checksum, _ := cmdContent["checksum"].(string)

		task := &simulator.UpgradeTask{
			VirusType:   int(virusType),
			Version:     version,
			DownloadURL: downloadUrl,
			Checksum:    checksum,
			Status:      "running",
			Progress:    0,
		}
		station.SetUpgradeTask(task)

		// 启动升级流程 goroutine
		go station.ExecuteUpgrade()

	case protocol.CodeTaskExclusive:
		station.GetLogger().Warn("upgrade rejected: task already running",
			zap.String("station", station.ID),
		)
	}
	return nil
}

// BuildUpgradeResultBody 构建升级结果上报体（CMDID=109）
// 对齐协议 §7.9：taskId 等于下发请求(CMDID=108)的 msgId
func BuildUpgradeResultBody(station *simulator.SimStation, taskId string, status string, progress int, params map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{
		"taskId":   taskId,
		"status":   status,
		"progress": progress,
	}

	if status == "running" {
		body["progress"] = progress
	}

	if status == "completed" {
		task := station.GetUpgradeTask()
		if task != nil {
			body["virusType"] = task.VirusType
			body["version"] = task.Version
		}
	}

	if status == "failed" {
		errorCode, _ := params["errorCode"].(int)
		body["errorCode"] = errorCode
		body["message"] = params["message"]
	}

	return body
}

// UpgradeTaskState 升级任务状态跟踪
type UpgradeTaskState struct {
	TaskID     string
	StartTime  time.Time
	VirusType  int
	Version    string
	IsRunning  bool
}
