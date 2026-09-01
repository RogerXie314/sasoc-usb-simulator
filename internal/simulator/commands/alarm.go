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

// ===== 旧版告警类型（设备状态类，保留兼容） =====
const (
	AlarmTypeUSBIllegalAccess = "USB_ILLEGAL_ACCESS" // 安全U盘非法接入（重要）
	AlarmTypeCPUAbnormal      = "CPU_ABNORMAL"       // CPU使用率异常（重要）
	AlarmTypeMemoryAbnormal   = "MEMORY_ABNORMAL"    // 内存使用率异常（重要）
	AlarmTypeDiskAbnormal     = "DISK_ABNORMAL"      // 硬盘使用率异常（重要）
	AlarmTypeDeviceFault      = "DEVICE_FAULT"       // U盘管控柜故障（重要）
	AlarmTypeMalwareDetected  = "MALWARE_DETECTED"   // 发现病毒（紧急）
)

// ===== 新版安全U盘告警类型（对齐《安检站安全U盘告警协议》） =====
const (
	AlarmTypeDoorFault             = "SAFE_UDISK_DOOR_FAULT"               // 柜门故障（紧急）
	AlarmTypeNoReturnSlot          = "SAFE_UDISK_NO_AVAILABLE_RETURN_SLOT" // 当前无可用归还柜位（一般）
	AlarmTypeIllegalDevice         = "SAFE_UDISK_ILLEGAL_DEVICE"           // 非法设备插入（重要）
	AlarmTypeUnexpectedRemoval     = "SAFE_UDISK_UNEXPECTED_REMOVAL"       // U盘异常拔出（重要）
	AlarmTypeCabinetFatalFault     = "SAFE_UDISK_CABINET_FATAL_FAULT"      // 整机故障（紧急）
	AlarmTypeFormatFailed          = "SAFE_UDISK_FORMAT_FAILED"            // U盘格式化失败（重要）
	AlarmTypeMetadataWriteFailed   = "SAFE_UDISK_METADATA_WRITE_FAILED"    // U盘业务信息操作失败（重要）
	AlarmTypeInitFailed            = "SAFE_UDISK_INIT_FAILED"              // U盘初始化失败（重要）
	AlarmTypeDoorCloseTimeout      = "SAFE_UDISK_DOOR_CLOSE_TIMEOUT"       // 柜门关闭超时（重要）
	AlarmTypeUsageViolation        = "SAFE_UDISK_USAGE_VIOLATION"          // 安全U盘使用违规（重要）
)

// ===== 告警原因枚举（reason 0~29，对齐协议 §1.5） =====
const (
	ReasonUnknown                            = 0  // 未知原因（保留值）
	ReasonDoorHardwareFault                  = 1  // 柜门硬件故障
	ReasonDoorOpenFailed                     = 2  // 柜门打开失败
	ReasonUserReportedDoorOpenFailed         = 3  // 人工反馈柜门打开失败
	ReasonUserReportedDoorCloseFailed        = 4  // 人工反馈柜门关闭失败
	ReasonNoEmptyReturnSlot                  = 5  // 没有空闲归还柜位
	ReasonAllReturnDoorsOpenFailed           = 6  // 所有归还柜门打开失败
	ReasonIllegalDeviceInserted              = 7  // 插入非法设备
	ReasonIllegalDeviceFoundAfterDoorClosed  = 8  // 关闭柜门后发现非法设备
	ReasonIllegalDeviceFoundDuringReturn     = 9  // 归还过程中发现非法设备
	ReasonIllegalDeviceFoundDuringInventory  = 10 // 柜位巡检时发现非法设备
	ReasonUdiskRemovedWithoutActiveFlow      = 11 // 非借还流程中U盘异常拔出
	ReasonDoorOpenFailedRepeatedly           = 12 // 柜门连续多次打开失败，整机进入故障状态
	ReasonUdiskOperationFailedRepeatedly     = 13 // U盘操作连续多次失败，整机进入故障状态
	ReasonBorrowFormatFailed                 = 14 // 借用前格式化失败
	ReasonReturnRecoveryFormatFailed         = 15 // 归还后恢复U盘时格式化失败
	ReasonMaintenanceFormatFailed            = 16 // 维护初始化时格式化失败
	ReasonBorrowTimeoutRecoveryFormatFailed  = 17 // 借用取盘超时后恢复U盘时格式化失败
	ReasonNonflowRecoveryFormatFailed        = 18 // 非流程放入U盘后恢复格式化失败
	ReasonBorrowInfoWriteFailed              = 19 // 借用信息写入失败
	ReasonBorrowInfoVerifyReadFailed         = 20 // 写入后读取借用信息失败
	ReasonBorrowInfoVerifyMismatch           = 21 // 借用信息写入与回读内容不一致
	ReasonAntivirusFlagWriteFailed           = 22 // 防病毒标志写入失败
	ReasonBorrowInfoClearFailed              = 23 // 借用信息清除失败
	ReasonInitClearBorrowInfoFailed          = 24 // 初始化时清除借用信息失败
	ReasonInitClearVerifyReadFailed          = 25 // 初始化清除后读取借用信息失败
	ReasonInitClearVerifyMismatch            = 26 // 初始化清除后借用信息仍然存在
	ReasonBorrowDoorCloseTimeout             = 27 // 借用流程柜门关闭超时
	ReasonReturnDoorCloseTimeout             = 28 // 归还流程柜门关闭超时
	ReasonUdiskUsedOutsideBorrowPeriod       = 29 // 未在借用有效期内使用安全U盘
)

// ReasonNames 告警原因中文名映射
var ReasonNames = map[int]string{
	0: "未知原因", 1: "柜门硬件故障", 2: "柜门打开失败", 3: "人工反馈柜门打开失败", 4: "人工反馈柜门关闭失败",
	5: "没有空闲归还柜位", 6: "所有归还柜门打开失败", 7: "插入非法设备", 8: "关闭柜门后发现非法设备",
	9: "归还过程中发现非法设备", 10: "柜位巡检时发现非法设备", 11: "非借还流程中U盘异常拔出",
	12: "柜门连续多次打开失败，整机进入故障状态", 13: "U盘操作连续多次失败，整机进入故障状态",
	14: "借用前格式化失败", 15: "归还后恢复U盘时格式化失败", 16: "维护初始化时格式化失败",
	17: "借用取盘超时后恢复U盘时格式化失败", 18: "非流程放入U盘后恢复格式化失败",
	19: "借用信息写入失败", 20: "写入后读取借用信息失败", 21: "借用信息写入与回读内容不一致",
	22: "防病毒标志写入失败", 23: "借用信息清除失败", 24: "初始化时清除借用信息失败",
	25: "初始化清除后读取借用信息失败", 26: "初始化清除后借用信息仍然存在",
	27: "借用流程柜门关闭超时", 28: "归还流程柜门关闭超时", 29: "未在借用有效期内使用安全U盘",
}

// AlarmTypeReasons 告警类型 → 合法 reason 集合（协议一致性约束）
var AlarmTypeReasons = map[string][]int{
	AlarmTypeDoorFault:           {1, 2, 3, 4},
	AlarmTypeNoReturnSlot:        {5, 6},
	AlarmTypeIllegalDevice:       {7, 8, 9, 10},
	AlarmTypeUnexpectedRemoval:   {11},
	AlarmTypeCabinetFatalFault:   {12, 13},
	AlarmTypeFormatFailed:        {14, 15, 16, 17, 18},
	AlarmTypeMetadataWriteFailed: {19, 20, 21, 22, 23},
	AlarmTypeInitFailed:          {24, 25, 26},
	AlarmTypeDoorCloseTimeout:    {27, 28},
	AlarmTypeUsageViolation:      {29},
}

// IsSafeUdiskAlarm 判断是否为新版安全U盘告警类型
func IsSafeUdiskAlarm(alarmType string) bool {
	_, ok := AlarmTypeReasons[alarmType]
	return ok
}

// IsValidReasonForType 校验 reason 是否属于该告警类型的合法集合
func IsValidReasonForType(alarmType string, reason int) bool {
	reasons, ok := AlarmTypeReasons[alarmType]
	if !ok {
		return false
	}
	for _, r := range reasons {
		if r == reason {
			return true
		}
	}
	return false
}

func (c *AlarmCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	alarmType, _ := params["alarmType"].(string)
	if alarmType == "" {
		alarmType = AlarmTypeDeviceFault
	}

	sn, _ := params["sn"].(string)

	body := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"alarmType": alarmType,
		"sn":        sn,
	}

	// 新版安全U盘告警：生成 detail{doorNo, reason}，并按协议携带规则处理
	if IsSafeUdiskAlarm(alarmType) {
		reason := 0
		switch v := params["reason"].(type) {
		case int:
			reason = v
		case float64:
			reason = int(v)
		}
		doorNo := params["doorNo"]

		// 携带规则：
		// reason 5,6（无可用归还柜位）: doorNo="", sn=""
		// reason 29（使用违规）: doorNo=""
		if reason == 5 || reason == 6 {
			doorNo = ""
			sn = ""
			body["sn"] = ""
		}
		if reason == 29 {
			doorNo = ""
		}

		detail := map[string]interface{}{
			"reason": reason,
		}
		if doorNo != nil {
			detail["doorNo"] = doorNo
		} else {
			detail["doorNo"] = ""
		}
		body["detail"] = detail
		return body, nil
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
	case AlarmTypeMalwareDetected, AlarmTypeDoorFault, AlarmTypeCabinetFatalFault:
		return "紧急"
	case AlarmTypeNoReturnSlot:
		return "一般"
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

	// 新版安全U盘告警：校验 reason 合法性
	if IsSafeUdiskAlarm(alarmType) {
		reasonVal, ok := params["reason"]
		if !ok {
			return fmt.Errorf("reason is required for %s alarm", alarmType)
		}
		reason, ok := reasonVal.(int)
		if !ok {
			// 兼容 float64（JSON 反序列化）
			if f, fok := reasonVal.(float64); fok {
				reason = int(f)
			} else {
				return fmt.Errorf("reason must be a number")
			}
		}
		if !IsValidReasonForType(alarmType, reason) {
			return fmt.Errorf("reason %d is not valid for alarmType %s", reason, alarmType)
		}
		return nil
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

// AllSafeUdiskAlarmSamples 返回覆盖全部10种安全U盘告警类型的样例参数
// 用于"一键模拟全部告警"，每种类型取第一个合法 reason
func AllSafeUdiskAlarmSamples() []map[string]interface{} {
	samples := []map[string]interface{}{}
	// 按类型顺序各取第一个合法 reason
	order := []string{
		AlarmTypeDoorFault,
		AlarmTypeNoReturnSlot,
		AlarmTypeIllegalDevice,
		AlarmTypeUnexpectedRemoval,
		AlarmTypeCabinetFatalFault,
		AlarmTypeFormatFailed,
		AlarmTypeMetadataWriteFailed,
		AlarmTypeInitFailed,
		AlarmTypeDoorCloseTimeout,
		AlarmTypeUsageViolation,
	}
	doorNo := 1
	for _, alarmType := range order {
		reasons := AlarmTypeReasons[alarmType]
		reason := reasons[0]
		p := map[string]interface{}{
			"alarmType": alarmType,
			"reason":    reason,
		}
		// 按携带规则填充 sn / doorNo
		switch {
		case reason == 5 || reason == 6:
			p["sn"] = ""
			p["doorNo"] = ""
		case reason == 29:
			p["sn"] = "disk-0001"
			p["doorNo"] = ""
		default:
			p["sn"] = "disk-0001"
			p["doorNo"] = doorNo
			doorNo++
			if doorNo > 24 {
				doorNo = 1
			}
		}
		samples = append(samples, p)
	}
	return samples
}