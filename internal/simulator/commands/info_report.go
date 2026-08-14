package commands

import (
	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
)

// InfoReportCommand CMDID=101 信息上报
type InfoReportCommand struct{}

func (c *InfoReportCommand) CmdID() uint32 { return protocol.CmdInfoReport }

func (c *InfoReportCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	virusLibs := make([]map[string]interface{}, 0)
	for _, lib := range station.VirusLibs {
		virusLibs = append(virusLibs, map[string]interface{}{
			"type":        lib.Type,
			"version":     lib.Version,
			"upgradeTime": lib.UpgradeTime,
		})
	}

	body := map[string]interface{}{
		"sn":        station.SN,
		"model":     station.Model,
		"version":   station.Version,
		"virusLibs": virusLibs,
		"cabinet":   station.GetCabinet().ToReportMap(),
	}

	// 支持通过 params 覆盖
	for k, v := range params {
		body[k] = v
	}

	return body, nil
}

func (c *InfoReportCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	// 信息上报应答通常为空或包含确认码
	var resp map[string]interface{}
	if err := frame.DecodeJSONBody(&resp); err != nil {
		// 数组格式：[{...}]
		var arr []map[string]interface{}
		if e2 := frame.DecodeJSONBody(&arr); e2 == nil && len(arr) > 0 {
			_ = extractCMDContent(arr[0])
		}
		return nil
	}
	// 有应答时从 CMDContent 提取（兼容扁平格式）
	_ = extractCMDContent(resp)
	return nil
}
