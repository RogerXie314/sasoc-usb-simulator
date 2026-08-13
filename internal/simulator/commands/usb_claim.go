package commands

import (
	"fmt"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// UsbClaimCommand CMDID=104 U盘领取上报
type UsbClaimCommand struct{}

func (c *UsbClaimCommand) CmdID() uint32 { return protocol.CmdUsbClaim }

func (c *UsbClaimCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	sn, _ := params["sn"].(string)
	applyCode, _ := params["applyCode"].(string)
	result, _ := params["result"].(string) // success / fail

	if sn == "" {
		return nil, fmt.Errorf("sn is required")
	}
	if applyCode == "" {
		return nil, fmt.Errorf("applyCode is required")
	}
	if result == "" {
		result = "success"
	}

	body := map[string]interface{}{
		"applyCode": applyCode,
		"sn":        sn,
		"result":    result,
	}

	return body, nil
}

func (c *UsbClaimCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	var resp map[string]interface{}
	if err := frame.DecodeJSONBody(&resp); err != nil {
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
		station.GetLogger().Info("usb claim success",
			zap.String("station", station.ID),
		)
		// 领取成功后自动信息上报刷新
		go func() {
			SendCommand(station, protocol.CmdInfoReport, nil)
		}()
	default:
		station.GetLogger().Error("usb claim failed",
			zap.String("station", station.ID),
			zap.Float64("code", code),
		)
	}

	return nil
}
