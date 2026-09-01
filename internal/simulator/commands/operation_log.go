package commands

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
)

// OperationLogCommand CMDID=107 操作日志上报
type OperationLogCommand struct{}

func (c *OperationLogCommand) CmdID() uint32 { return protocol.CmdOperationLog }

// 操作类型常量（对齐《安检站CMDID107操作日志协议》）
const (
	OpInsert           = "insert"           // U盘插入
	OpRemove           = "remove"           // U盘拔出
	OpScan             = "scan"             // 病毒扫描
	OpKill             = "kill"             // 病毒处理
	OpCopy             = "copy"             // 数据摆渡
	OpQuarantineExport = "quarantineExport" // 隔离区导出
)

// 病毒处理方式枚举（kill 日志 message 中的 handleType）
const (
	VirusHandleSkip      = 1 // 跳过/暂不处理
	VirusHandleQuarantine = 2 // 隔离
	VirusHandleTrust      = 3 // 信任
)

func (c *OperationLogCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	operation, _ := params["operation"].(string)
	if operation == "" {
		operation = OpInsert
	}
	if !IsValidOperation(operation) {
		return nil, fmt.Errorf("invalid operation: %s", operation)
	}

	result, _ := params["result"].(string)
	if result == "" {
		result = "success"
	}
	if result != "success" && result != "fail" {
		return nil, fmt.Errorf("result must be success or fail")
	}

	// insert/remove 的 result 固定为 success
	if operation == OpInsert || operation == OpRemove {
		result = "success"
	}

	sn, _ := params["sn"].(string)

	// message 按协议格式生成（允许外部显式传入覆盖）
	message := buildOpLogMessage(operation, result, params)

	body := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"operation": operation,
		"result":    result,
		"message":   message,
	}

	// sn 为条件字段：有 SN 介质才生成；无 SN 介质（如数据摆渡、隔离区导出）省略该字段
	if sn != "" {
		body["sn"] = sn
	}

	return body, nil
}

// buildOpLogMessage 按协议规定生成 message
func buildOpLogMessage(operation, result string, params map[string]interface{}) string {
	// 外部显式指定 message 时优先使用
	if m, ok := params["message"].(string); ok && m != "" {
		return m
	}

	switch operation {
	case OpInsert:
		return "U盘插入"
	case OpRemove:
		return "U盘拔出"
	case OpScan:
		if result == "success" {
			// 生成扫描统计信息
			start := time.Now().Add(-time.Duration(rand.Intn(300)+30) * time.Second)
			end := time.Now()
			total := rand.Intn(500) + 10
			detected := 0
			if rand.Intn(10) < 3 {
				detected = rand.Intn(3) + 1
			}
			return fmt.Sprintf("scanStartTime=%d;scanEndTime=%d;totalFileCount=%d;detectedVirusCount=%d",
				start.UnixMilli(), end.UnixMilli(), total, detected)
		}
		return "启动扫描失败"
	case OpKill:
		virusName, _ := params["virusName"].(string)
		if virusName == "" {
			virusName = "Trojan.Test"
		}
		filePath, _ := params["filePath"].(string)
		if filePath == "" {
			filePath = "/media/usb/test.exe"
		}
		handleType, _ := params["handleType"].(int)
		if handleType < 1 || handleType > 3 {
			handleType = VirusHandleQuarantine
		}
		base := fmt.Sprintf("virusName=%s;filePath=%s;handleType=%d", virusName, filePath, handleType)
		if result == "fail" {
			errMsg, _ := params["error"].(string)
			if errMsg == "" {
				errMsg = "隔离文件失败"
			}
			return base + ";error=" + errMsg
		}
		return base
	case OpQuarantineExport:
		return "隔离区导出"
	case OpCopy:
		if result == "fail" {
			return "数据摆渡失败"
		}
		return "数据摆渡成功"
	}
	return ""
}

func (c *OperationLogCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	// 操作日志为单向上报，无业务应答
	return nil
}

// ValidOperations 有效操作类型列表
var ValidOperations = []string{OpInsert, OpRemove, OpScan, OpKill, OpCopy, OpQuarantineExport}

// IsValidOperation 校验操作类型
func IsValidOperation(op string) bool {
	for _, valid := range ValidOperations {
		if op == valid {
			return true
		}
	}
	return false
}