package commands

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
)

// generateMsgID 生成消息ID（时间戳+随机数，连接内唯一）
func generateMsgID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Intn(100000))
}

// Command 命令接口
type Command interface {
	// CmdID 返回命令ID
	CmdID() uint32
	// BuildBody 构建请求体
	BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error)
	// HandleResponse 处理响应
	HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error
}

// 全局命令注册表
var commandRegistry = map[uint32]Command{}

// RegisterCommand 注册命令
func RegisterCommand(cmd Command) {
	commandRegistry[cmd.CmdID()] = cmd
}

// GetCommand 获取命令
func GetCommand(cmdID uint32) (Command, bool) {
	cmd, ok := commandRegistry[cmdID]
	return cmd, ok
}

// AllCommands 返回所有注册的命令
func AllCommands() map[uint32]Command {
	return commandRegistry
}

// init 注册所有命令
func init() {
	RegisterCommand(&HeartbeatCommand{})
	RegisterCommand(&InfoReportCommand{})
	RegisterCommand(&RegisterCmd{})
	RegisterCommand(&ClaimVerifyCommand{})
	RegisterCommand(&UsbClaimCommand{})
	RegisterCommand(&UsbReturnCommand{})
	RegisterCommand(&AlarmCommand{})
	RegisterCommand(&OperationLogCommand{})
	RegisterCommand(&UpgradeCommand{})
}

// SendCommand 发送指定命令（自动添加协议外层Head包装）
func SendCommand(station *simulator.SimStation, cmdID uint32, params map[string]interface{}) error {
	cmd, ok := GetCommand(cmdID)
	if !ok {
		return fmt.Errorf("unknown command: %d", cmdID)
	}

	innerBody, err := cmd.BuildBody(station, params)
	if err != nil {
		return fmt.Errorf("build body: %w", err)
	}

	// 协议 §5：JSON包体需要外层 Head 包装
	// {ComputerID, CMDID, CMDVER, msgId, CMDContent:{业务字段}}
	// 心跳(CMDID=100)无需 msgId
	wrappedBody := map[string]interface{}{
		"ComputerID": station.ComputerID(),
		"CMDID":      cmdID,
		"CMDVER":     1,
		"CMDContent": innerBody,
	}
	if cmdID != protocol.CmdHeartbeat {
		wrappedBody["msgId"] = generateMsgID()
	}

	return station.SendFrame(cmdID, wrappedBody)
}

// HandleResponse 处理响应
func HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	cmd, ok := GetCommand(frame.Header.CmdID)
	if !ok {
		return fmt.Errorf("unknown command response: %d", frame.Header.CmdID)
	}
	return cmd.HandleResponse(station, frame)
}

// extractCMDContent 从协议响应中提取 CMDContent 嵌套结构
// SASOC 响应格式：{CMDID, msgId, CMDContent:{code, message, data:{...}}}
// 兼容无包裹的扁平格式（Echo Server 场景）
func extractCMDContent(resp map[string]interface{}) map[string]interface{} {
	if cmdContent, ok := resp["CMDContent"].(map[string]interface{}); ok {
		return cmdContent
	}
	return resp // 扁平格式直接返回
}
