package commands

import (
	"fmt"

	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// ClaimVerifyCommand CMDID=103 申领码验证
type ClaimVerifyCommand struct{}

func (c *ClaimVerifyCommand) CmdID() uint32 { return protocol.CmdClaimVerify }

func (c *ClaimVerifyCommand) BuildBody(station *simulator.SimStation, params map[string]interface{}) (interface{}, error) {
	applyCode, _ := params["applyCode"].(string)
	if applyCode == "" {
		return nil, fmt.Errorf("applyCode is required")
	}

	body := map[string]interface{}{
		"applyCode": applyCode,
	}

	return body, nil
}

func (c *ClaimVerifyCommand) HandleResponse(station *simulator.SimStation, frame *protocol.Frame) error {
	var resp map[string]interface{}
	if err := frame.DecodeJSONBody(&resp); err != nil {
		return err
	}

	cmdContent := extractCMDContent(resp)
	code, _ := cmdContent["code"].(float64)

	switch int(code) {
	case protocol.CodeSuccess:
		// 验证成功，提取申领信息
		data, _ := cmdContent["data"].(map[string]interface{})
		if data != nil {
			station.GetLogger().Info("claim verify success",
				zap.String("station", station.ID),
				zap.Any("applicantName", data["applicantName"]),
				zap.Any("applicantNo", data["applicantNo"]),
				zap.Any("startTime", data["startTime"]),
				zap.Any("endTime", data["endTime"]),
				zap.Any("areaCodes", data["areaCodes"]),
			)
		} else {
			station.GetLogger().Info("claim verify success",
				zap.String("station", station.ID),
			)
		}
	case protocol.CodeClaimNotExist:
		station.GetLogger().Warn("claim code not exist or expired",
			zap.String("station", station.ID),
		)
	case protocol.CodeClaimNotAvailable:
		station.GetLogger().Warn("claim code not available for claiming",
			zap.String("station", station.ID),
		)
	case protocol.CodeOutOfTimeRange:
		station.GetLogger().Warn("out of time range for claiming",
			zap.String("station", station.ID),
		)
	default:
		station.GetLogger().Error("claim verify failed",
			zap.String("station", station.ID),
			zap.Float64("code", code),
		)
	}

	return nil
}
