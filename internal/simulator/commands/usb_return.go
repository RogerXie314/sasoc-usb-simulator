package commands

import (
	"fmt"
	"time"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// UsbReturnCommand CMDID=105 U盘归还上报
type UsbReturnCommand struct{}

func (c *UsbReturnCommand) CmdID() uint32 { return protocol.CmdUsbReturn }

func (c *UsbReturnCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	sn, _ := params["sn"].(string)

	if sn == "" {
		return nil, fmt.Errorf("sn is required")
	}

	body := map[string]interface{}{
		"sn": sn,
	}

	return body, nil
}

func (c *UsbReturnCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
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
		station.GetLogger().Info("usb return success",
			zap.String("station", station.ID),
		)
		// 归还成功后自动信息上报刷新（SASOC会根据slots更新U盘所在安检站/柜门信息）
		go func() {
			time.Sleep(1 * time.Second)
			SendCommand(station, protocol.CmdInfoReport, nil)
		}()
	case protocol.CodeUsbNotCollected:
		station.GetLogger().Warn("usb not collected",
			zap.String("station", station.ID),
		)
	case protocol.CodeUsbScrapped:
		station.GetLogger().Warn("usb scrapped",
			zap.String("station", station.ID),
		)
	default:
		station.GetLogger().Error("usb return failed",
			zap.String("station", station.ID),
			zap.Float64("code", code),
		)
	}

	return nil
}
