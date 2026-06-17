package commands

import (
	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
)

// HeartbeatCommand CMDID=100 心跳
type HeartbeatCommand struct{}

func (c *HeartbeatCommand) CmdID() uint32 { return protocol.CmdHeartbeat }

func (c *HeartbeatCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	resources := station.GetResources()
	cabinet := station.GetCabinet()

	body := map[string]interface{}{
		"cpu":        resources.CPU,
		"memory":     resources.Memory,
		"disk":       resources.Disk,
		"doorStatus": cabinet.DoorStatus,
	}

	// 支持通过 params 覆盖
	if v, ok := params["cpu"]; ok {
		body["cpu"] = v
	}
	if v, ok := params["memory"]; ok {
		body["memory"] = v
	}
	if v, ok := params["disk"]; ok {
		body["disk"] = v
	}
	if v, ok := params["doorStatus"]; ok {
		body["doorStatus"] = v
	}

	return body, nil
}

func (c *HeartbeatCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	// 心跳应答为空包体，仅更新时间戳
	station.UpdateLastHeartbeat()
	return nil
}
