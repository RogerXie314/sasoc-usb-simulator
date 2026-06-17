package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator/commands"
	"go.uber.org/zap"
)

// protocolDebugHandler 协议调试 API 处理器
type protocolDebugHandler struct {
	hub *hub.Hub
}

// newProtocolDebugHandler 创建协议调试处理器
func newProtocolDebugHandler(h *hub.Hub) *protocolDebugHandler {
	return &protocolDebugHandler{hub: h}
}

// sendRawRequest 发送原始命令请求
type sendRawRequest struct {
	StationID string                 `json:"stationId" binding:"required"`
	CmdID     uint32                 `json:"cmdId" binding:"required"`
	Params    map[string]interface{} `json:"params"`
}

// sendRaw POST /api/v1/debug/send
// 向指定安检站发送原始协议命令
func (dh *protocolDebugHandler) sendRaw(c *gin.Context) {
	var req sendRawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	station, exists := dh.hub.GetStation(req.StationID)
	if !exists {
		responseError(c, http.StatusNotFound, "station "+req.StationID+" not found")
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	// 验证命令是否存在
	cmd, ok := commands.GetCommand(req.CmdID)
	if !ok {
		responseError(c, http.StatusBadRequest, "unknown command ID")
		return
	}

	// 构建请求体
	params := req.Params
	if params == nil {
		params = make(map[string]interface{})
	}

	body, err := cmd.BuildBody(station, params)
	if err != nil {
		responseError(c, http.StatusBadRequest, "failed to build command body: "+err.Error())
		return
	}

	// 发送命令
	if err := commands.SendCommand(station, req.CmdID, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send command: "+err.Error())
		return
	}

	dh.hub.NotifyMessageSent(station.ID, req.CmdID, "debug_send", 0)

	zap.L().Info("debug raw command sent",
		zap.String("stationId", req.StationID),
		zap.Uint32("cmdId", req.CmdID),
	)

	responseSuccess(c, gin.H{
		"stationId": req.StationID,
		"cmdId":     req.CmdID,
		"body":      body,
		"message":   "command sent",
	})
}

// encodeFrameRequest 帧编码请求
type encodeFrameRequest struct {
	CmdID      uint32      `json:"cmdId" binding:"required"`
	DeviceID   uint32      `json:"deviceId"`
	Body       interface{} `json:"body"`
	Encrypt    bool        `json:"encrypt"`
	Compress   bool        `json:"compress"`
	RandomValue uint16     `json:"randomValue"`
	SerialNo   uint32      `json:"serialNo"`
}

// encodeFrame POST /api/v1/debug/encode
// 编码协议帧，返回十六进制字符串
func (dh *protocolDebugHandler) encodeFrame(c *gin.Context) {
	var req encodeFrameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	opts := protocol.EncodeOptions{
		Encrypt:     req.Encrypt,
		Compress:    req.Compress,
		RandomValue: req.RandomValue,
		SerialNo:    req.SerialNo,
	}

	frameBytes, err := protocol.EncodeFrame(req.CmdID, req.DeviceID, req.Body, opts)
	if err != nil {
		responseError(c, http.StatusBadRequest, "failed to encode frame: "+err.Error())
		return
	}

	hexStr := hex.EncodeToString(frameBytes)

	// 返回详细信息
	responseSuccess(c, gin.H{
		"hex":       hexStr,
		"length":    len(frameBytes),
		"headerLen": protocol.HeaderSize,
		"bodyLen":   len(frameBytes) - protocol.HeaderSize,
		"cmdId":     req.CmdID,
		"deviceId":  req.DeviceID,
		"encrypt":   req.Encrypt,
		"compress":  req.Compress,
	})
}

// decodeFrameRequest 帧解码请求
type decodeFrameRequest struct {
	Hex string `json:"hex" binding:"required"`
}

// decodeFrame POST /api/v1/debug/decode
// 从十六进制字符串解码协议帧
func (dh *protocolDebugHandler) decodeFrame(c *gin.Context) {
	var req decodeFrameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	data, err := hex.DecodeString(req.Hex)
	if err != nil {
		responseError(c, http.StatusBadRequest, "invalid hex string: "+err.Error())
		return
	}

	frame, err := protocol.DecodeFrame(data)
	if err != nil {
		responseError(c, http.StatusBadRequest, "failed to decode frame: "+err.Error())
		return
	}

	// 尝试解析包体
	var bodyJSON string
	bodyResult := gin.H{}

	if len(frame.Body) > 0 {
		// 尝试获取明文 JSON
		bodyStr, err := frame.BodyJSON()
		if err == nil {
			bodyJSON = bodyStr
			// 尝试解析为 map
			var bodyMap map[string]interface{}
			if json.Unmarshal([]byte(bodyStr), &bodyMap) == nil {
				bodyResult = bodyMap
			} else {
				bodyResult = gin.H{"raw": bodyStr}
			}
		} else {
			// 解密/解压失败，返回原始 hex
			bodyResult = gin.H{
				"rawHex":  hex.EncodeToString(frame.Body),
				"decodeError": err.Error(),
			}
		}
	}

	// 构建包头信息
	header := frame.Header
	headerInfo := gin.H{
		"headFlag":    fmt.Sprintf("0x%02x%02x", header.HeadFlag[0], header.HeadFlag[1]),
		"version":     header.Version,
		"srcType":     header.SrcType,
		"bodyLen":     header.BodyLen,
		"decFlag":     header.DecFlag,
		"zipFlag":     header.ZipFlag,
		"fillLen":     header.FillLen,
		"randomValue": header.RandomValue,
		"serialNo":    header.SerialNo,
		"checkSum":    fmt.Sprintf("0x%08x", header.CheckSum),
		"sid":         header.Sid,
		"timeFlag":    header.TimeFlag,
		"cmdId":       header.CmdID,
		"devId":       header.DevID,
		"srcLen":      header.SrcLen,
		"encrypted":   header.IsEncrypted(),
		"compressed":  header.IsCompressed(),
	}

	// 命令名称映射
	cmdName := protocolCmdName(header.CmdID)

	responseSuccess(c, gin.H{
		"header":     headerInfo,
		"body":       bodyResult,
		"bodyJSON":   bodyJSON,
		"totalLen":   len(data),
		"cmdName":    cmdName,
	})
}

// protocolCmdName 返回命令ID对应的名称
func protocolCmdName(cmdID uint32) string {
	names := map[uint32]string{
		protocol.CmdHeartbeat:     "Heartbeat",
		protocol.CmdInfoReport:    "InfoReport",
		protocol.CmdRegister:      "Register",
		protocol.CmdClaimVerify:   "ClaimVerify",
		protocol.CmdUsbClaim:      "UsbClaim",
		protocol.CmdUsbReturn:     "UsbReturn",
		protocol.CmdAlarm:         "Alarm",
		protocol.CmdOperationLog:  "OperationLog",
		protocol.CmdUpgradeIssue:  "UpgradeIssue",
		protocol.CmdUpgradeResult: "UpgradeResult",
	}
	if name, ok := names[cmdID]; ok {
		return name
	}
	return "Unknown"
}
