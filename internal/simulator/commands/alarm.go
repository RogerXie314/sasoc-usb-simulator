package commands

import (
	"fmt"
	"time"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
)

// AlarmCommand CMDID=106 告警上报
type AlarmCommand struct{}

func (c *AlarmCommand) CmdID() uint32 { return protocol.CmdAlarm }

// 告警类型常量（对齐协议文档 §7.7）
const (
	AlarmTypeUSBIllegalAccess = "USB_ILLEGAL_ACCESS" // 安全U盘非法接入（重要）
	AlarmTypeCPUAbnormal      = "CPU_ABNORMAL"       // CPU使用率异常（重要）
	AlarmTypeMemoryAbnormal   = "MEMORY_ABNORMAL"    // 内存使用率异常（重要）
	AlarmTypeDiskAbnormal     = "DISK_ABNORMAL"      // 硬盘使用率异常（重要）
	AlarmTypeDeviceFault      = "DEVICE_FAULT"       // U盘管控柜故障（重要）
	AlarmTypeMalwareDetected  = "MALWARE_DETECTED"   // 发现病毒（紧急）
)

func (c *AlarmCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	alarmType, _ := params["alarmType"].(string)
	if alarmType == "" {
		alarmType = AlarmTypeDeviceFault
	}

	sn, _ := params["sn"].(string)

	body := map[string]interface{}{
		"timestamp":  time.Now().UnixMilli(),
		"alarmType":  alarmType,
		"sn":         sn,
	}

	// 病毒告警特有字段（MALWARE_DETECTED 时必填 detail）
	if alarmType == AlarmTypeMalwareDetected {
		detail := map[string]interface{}{
			"virusName": params["virusName"],
			"virusType": params["virusType"],
			"fileName":  params["fileName"],
			"fileHash":  params["fileHash"],
		}
		body["detail"] = detail
	} else if detailVal, ok := params["detail"]; ok && detailVal != nil {
		body["detail"] = detailVal
	}

	return body, nil
}

func (c *AlarmCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	// 告警上报为单向上报，无业务应答
	// 但协议中可能收到 code=1000（设备未注册）等通用错误
	return nil
}

// GetAlarmLevel 根据告警类型返回告警级别
func GetAlarmLevel(alarmType string) string {
	switch alarmType {
	case AlarmTypeMalwareDetected:
		return "紧急"
	case AlarmTypeUSBIllegalAccess, AlarmTypeCPUAbnormal,
		AlarmTypeMemoryAbnormal, AlarmTypeDiskAbnormal, AlarmTypeDeviceFault:
		return "重要"
	default:
		return "重要"
	}
}

// ValidateAlarmParams 校验告警参数
func ValidateAlarmParams(params map[string]interface{}) error {
	alarmType, _ := params["alarmType"].(string)
	if alarmType == "" {
		return fmt.Errorf("alarmType is required")
	}

	// 病毒检测告警需要 detail 字段
	if alarmType == AlarmTypeMalwareDetected {
		if _, ok := params["virusName"]; !ok {
			return fmt.Errorf("virusName is required for MALWARE_DETECTED alarm")
		}
		if _, ok := params["virusType"]; !ok {
			return fmt.Errorf("virusType is required for MALWARE_DETECTED alarm")
		}
		if _, ok := params["fileName"]; !ok {
			return fmt.Errorf("fileName is required for MALWARE_DETECTED alarm")
		}
		if _, ok := params["fileHash"]; !ok {
			return fmt.Errorf("fileHash is required for MALWARE_DETECTED alarm")
		}
	}

	return nil
}
