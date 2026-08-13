package commands

import (
	"time"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// RegisterCmd CMDID=102 设备注册
type RegisterCmd struct{}

func (c *RegisterCmd) CmdID() uint32 { return protocol.CmdRegister }

func (c *RegisterCmd) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	body := map[string]interface{}{
		"model":   station.Model,
		"version": station.Version,
		"ip":      station.IP,
		"mac":     station.MAC,
		"name":    station.Name,
	}

	// 支持通过 params 覆盖
	for k, v := range params {
		body[k] = v
	}

	return body, nil
}

func (c *RegisterCmd) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	var resp map[string]interface{}
	if err := frame.DecodeJSONBody(&resp); err != nil {
		// 数组格式：[{...}]
		var arr []map[string]interface{}
		if e2 := frame.DecodeJSONBody(&arr); e2 != nil || len(arr) == 0 {
			return err
		}
		resp = arr[0]
	}

	cmdContent := extractCMDContent(resp)
	code, _ := cmdContent["code"].(float64)

	switch int(code) {
	case protocol.CodeSuccess:
		// 从 CMDContent.data.deviceId 获取设备ID
		data, _ := cmdContent["data"].(map[string]interface{})
		if data != nil {
			if devid, ok := data["deviceId"].(float64); ok {
				station.SetDeviceID(uint32(devid))
			}
		} else {
			// 兼容：deviceId 直接在 CMDContent 下
			if devid, ok := cmdContent["deviceId"].(float64); ok {
				station.SetDeviceID(uint32(devid))
			}
		}
		station.SetState(simulator.StateOnline)
		station.GetLogger().Info("register success",
			zap.String("station", station.ID),
			zap.Uint32("deviceId", station.DeviceID),
		)

		// 启动心跳
		if station.HeartbeatEnabled {
			go station.StartHeartbeatLoop()
		}

		// 自动发送信息上报
		go func() {
			if err := SendCommand(station, protocol.CmdInfoReport, nil); err != nil {
				station.GetLogger().Error("auto info report failed", zap.Error(err))
			}
		}()

	case protocol.CodeOverCapacity:
		station.GetLogger().Error("register rejected: over capacity",
			zap.String("station", station.ID),
		)
		station.SetState(simulator.StateIdle)

	case protocol.CodeNotRegistered:
		// 未注册被拒，重新注册
		station.GetLogger().Warn("not registered, re-registering",
			zap.String("station", station.ID),
		)
		go func() {
			time.Sleep(5 * time.Second)
			SendCommand(station, protocol.CmdRegister, nil)
		}()

	default:
		station.GetLogger().Error("register failed",
			zap.String("station", station.ID),
			zap.Float64("code", code),
		)
		station.SetState(simulator.StateReconnect)
	}

	return nil
}
