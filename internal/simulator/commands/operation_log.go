package commands

import (
	"fmt"
	"time"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
)

// OperationLogCommand CMDID=107 操作日志上报
type OperationLogCommand struct{}

func (c *OperationLogCommand) CmdID() uint32 { return protocol.CmdOperationLog }

// 操作类型常量（对齐协议文档 §7.8）
const (
	OpInsert   = "insert"  // 插入U盘
	OpRemove   = "remove"  // 拔出U盘
	OpScan     = "scan"    // 查毒
	OpKill     = "kill"    // 杀毒
	OpCopy     = "copy"    // 复制文件
)

func (c *OperationLogCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	operation, _ := params["operation"].(string)
	if operation == "" {
		operation = OpInsert
	}

	sn, _ := params["sn"].(string)
	if sn == "" {
		return nil, fmt.Errorf("sn is required")
	}

	result, _ := params["result"].(string)
	if result == "" {
		result = "success"
	}

	body := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"sn":        sn,
		"operation": operation,
		"result":    result,
		"message":   params["message"],
	}

	return body, nil
}

func (c *OperationLogCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	// 操作日志为单向上报，无业务应答
	return nil
}

// ValidOperations 有效操作类型列表
var ValidOperations = []string{OpInsert, OpRemove, OpScan, OpKill, OpCopy}

// IsValidOperation 校验操作类型
func IsValidOperation(op string) bool {
	for _, valid := range ValidOperations {
		if op == valid {
			return true
		}
	}
	return false
}
