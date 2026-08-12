package commands

import (
	"fmt"
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
	upgradeType, _ := params["upgradeType"].(string)

	body := map[string]interface{}{
		"virusType":   virusType,
		"version":     version,
		"downloadUrl": downloadUrl,
		"checksum":    checksum,
	}
	if upgradeType != "" {
		body["upgradeType"] = upgradeType
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
		// 提取 msgId 作为 taskId
		taskId, _ := resp["msgId"].(string)
		if taskId == "" {
			taskId = fmt.Sprintf("upgrade-%d", time.Now().UnixMilli())
		}
		virusType, _ := cmdContent["virusType"].(float64)
		version, _ := cmdContent["version"].(string)
		downloadUrl, _ := cmdContent["downloadUrl"].(string)
		checksum, _ := cmdContent["checksum"].(string)

		// 判断升级类型
		upgradeType, _ := cmdContent["upgradeType"].(string)
		if upgradeType == "" {
			upgradeType, _ = cmdContent["type"].(string)
		}
		if upgradeType == "" {
			if _, hasPkg := cmdContent["PackageName"]; hasPkg {
				upgradeType = "software"
			} else {
				upgradeType = "virus"
			}
		}

		task := &simulator.UpgradeTask{
			TaskID:      taskId,
			UpgradeType: upgradeType,
			VirusType:   int(virusType),
			Version:     version,
			DownloadURL: downloadUrl,
			Checksum:    checksum,
			Status:      "downloading",
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
// 对齐真实客户端 SasocUpgradeBridge.cpp:70-126：必须包含 upgradeType
func BuildUpgradeResultBody(station *simulator.SimStation, taskId string, status string, progress int, params map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{
		"taskId": taskId,
		"status": status,
	}

	task := station.GetUpgradeTask()
	if task != nil {
		body["upgradeType"] = task.UpgradeType
	}

	if status == "running" {
		body["progress"] = progress
	}

	if status == "completed" {
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
