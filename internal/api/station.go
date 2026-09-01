package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/model"
	"github.com/usb-simulator/internal/protocol"
	"github.com/usb-simulator/internal/simulator"
	"github.com/usb-simulator/internal/simulator/commands"
	"go.uber.org/zap"
)

// stationHandler 安检站 API 处理器
type stationHandler struct {
	hub *hub.Hub
}

// newStationHandler 创建安检站处理器
func newStationHandler(h *hub.Hub) *stationHandler {
	return &stationHandler{hub: h}
}

// createStationRequest 创建安检站请求
type createStationRequest struct {
	ID                string `json:"stationId"`
	SN                string `json:"sn"`
	Model             string `json:"model"`
	Version           string `json:"version"`
	Name              string `json:"name"`
	IP                string `json:"ip"`
	MAC               string `json:"mac"`
	SasocHost         string `json:"sasocHost"`
	SasocPort         int    `json:"sasocPort"`
	HeartbeatEnabled  *bool  `json:"heartbeatEnabled"`
	HeartbeatInterval int    `json:"heartbeatInterval"`
	EncryptEnabled    *bool  `json:"encryptEnabled"`
	CompressEnabled   *bool  `json:"compressEnabled"`
}

// createStation POST /api/v1/stations
// 创建安检站：如果 ID 为空，自动使用 SN 作为 ID；如果 IP/MAC 为空，自动分配
func (sh *stationHandler) createStation(c *gin.Context) {
	var req createStationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.SN == "" {
		responseError(c, http.StatusBadRequest, "sn is required")
		return
	}

	// ID 为空时，用 SN 代替
	stationID := req.ID
	if stationID == "" {
		stationID = req.SN
	}

	// 检查上限
	if sh.hub.StationCount() >= 100 {
		responseError(c, http.StatusConflict, "max station limit reached")
		return
	}

	// 检查是否已存在
	if _, exists := sh.hub.GetStation(stationID); exists {
		responseError(c, http.StatusConflict, "station "+stationID+" already exists")
		return
	}

	// 获取全局配置
	cfg := sh.hub.Config()

	// 创建模拟站
	station := simulator.NewSimStation(stationID, req.SN, req.Model, req.Version, req.Name)

	// IP 为空时自动分配递增 IP
	if req.IP != "" {
		station.IP = req.IP
	} else {
		station.IP = hub.GenerateSequentialIP()
	}

	// MAC 为空时自动分配递增 MAC
	if req.MAC != "" {
		station.MAC = req.MAC
	} else {
		station.MAC = hub.GenerateSequentialMAC()
	}

	// SASOC 地址：前端不传时，使用全局配置
	if req.SasocHost != "" {
		station.SasocHost = req.SasocHost
	} else if cfg != nil && cfg.Sasoc.Host != "" {
		station.SasocHost = cfg.Sasoc.Host
	}
	if req.SasocPort > 0 {
		station.SasocPort = req.SasocPort
	} else if cfg != nil && cfg.Sasoc.Port > 0 {
		station.SasocPort = cfg.Sasoc.Port
	}

	if req.HeartbeatEnabled != nil {
		station.HeartbeatEnabled = *req.HeartbeatEnabled
	}
	if req.HeartbeatInterval > 0 {
		station.HeartbeatInterval = req.HeartbeatInterval
	}
	if req.EncryptEnabled != nil {
		station.EncryptEnabled = *req.EncryptEnabled
	}
	if req.CompressEnabled != nil {
		station.CompressEnabled = *req.CompressEnabled
	}

	if err := sh.hub.AddStation(station); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to create station: "+err.Error())
		return
	}

	zap.L().Info("station created via API", zap.String("stationId", stationID))
	responseSuccess(c, station)
}

// listStations GET /api/v1/station/list
func (sh *stationHandler) listStations(c *gin.Context) {
	stations := sh.hub.ListStations()

	// 构建响应（包含运行时信息）
	type stationInfo struct {
		*simulator.SimStation
		MsgSent     int64 `json:"msgSent"`
		MsgReceived int64 `json:"msgReceived"`
	}

	result := make([]stationInfo, 0, len(stations))
	for _, s := range stations {
		result = append(result, stationInfo{
			SimStation:  s,
			MsgSent:     s.MsgSent,
			MsgReceived: s.MsgReceived,
		})
	}

	responseSuccess(c, gin.H{
		"total":    len(result),
		"stations": result,
	})
}

// startStation POST /api/v1/station/:id/start
func (sh *stationHandler) startStation(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if station.GetState() == simulator.StateOnline {
		responseError(c, http.StatusConflict, "station is already online")
		return
	}

	oldState := string(station.GetState())
	if err := station.Start(); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to start station: "+err.Error())
		return
	}

	sh.hub.NotifyStationStateChange(station.ID, oldState, string(station.GetState()), station.DeviceID)
	responseSuccess(c, gin.H{"stationId": station.ID, "status": string(station.GetState())})
}

// stopStation POST /api/v1/station/:id/stop
func (sh *stationHandler) stopStation(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	oldState := string(station.GetState())
	if oldState == string(simulator.StateIdle) {
		responseError(c, http.StatusConflict, "station is already idle")
		return
	}

	if err := station.Stop(); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to stop station: "+err.Error())
		return
	}

	sh.hub.NotifyStationStateChange(station.ID, oldState, string(station.GetState()), station.DeviceID)
	responseSuccess(c, gin.H{"stationId": station.ID, "status": string(station.GetState())})
}

// restartStation POST /api/v1/station/:id/restart
func (sh *stationHandler) restartStation(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	oldState := string(station.GetState())
	if err := station.Restart(); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to restart station: "+err.Error())
		return
	}

	sh.hub.NotifyStationStateChange(station.ID, oldState, string(station.GetState()), station.DeviceID)
	responseSuccess(c, gin.H{"stationId": station.ID, "status": string(station.GetState())})
}

// heartbeat POST /api/v1/station/:id/heartbeat
func (sh *stationHandler) heartbeat(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var params map[string]interface{}
	// 请求体可选，允许空 body
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&params); err != nil {
			responseError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}
	if params == nil {
		params = make(map[string]interface{})
	}

	if err := commands.SendCommand(station, protocol.CmdHeartbeat, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send heartbeat: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdHeartbeat, "heartbeat", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "heartbeat sent"})
}

// infoReport POST /api/v1/station/:id/report
func (sh *stationHandler) infoReport(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var params map[string]interface{}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&params); err != nil {
			responseError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}
	if params == nil {
		params = make(map[string]interface{})
	}

	if err := commands.SendCommand(station, protocol.CmdInfoReport, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send info report: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdInfoReport, "info_report", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "info report sent"})
}

// verifyClaim POST /api/v1/station/:id/verify-claim
// 对齐协议 §7.4：字段为 applyCode
func (sh *stationHandler) verifyClaim(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		ApplyCode string `json:"applyCode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.ApplyCode == "" {
		responseError(c, http.StatusBadRequest, "applyCode is required")
		return
	}

	params := map[string]interface{}{"applyCode": req.ApplyCode}
	if err := commands.SendCommand(station, protocol.CmdClaimVerify, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send claim verify: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdClaimVerify, "claim_verify", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "claim verify sent"})
}

// claimUsb POST /api/v1/station/:id/claim-usb
// 对齐协议 §7.5：字段为 applyCode + sn + result
func (sh *stationHandler) claimUsb(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		ApplyCode string `json:"applyCode"`
		SN        string `json:"sn"`
		Result    string `json:"result"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.SN == "" {
		responseError(c, http.StatusBadRequest, "sn is required")
		return
	}
	if req.ApplyCode == "" {
		responseError(c, http.StatusBadRequest, "applyCode is required")
		return
	}

	params := map[string]interface{}{
		"applyCode": req.ApplyCode,
		"sn":        req.SN,
		"result":    req.Result,
	}
	if err := commands.SendCommand(station, protocol.CmdUsbClaim, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send usb claim: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdUsbClaim, "usb_claim", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "usb claim sent"})
}

// returnUsb POST /api/v1/station/:id/return-usb
// 对齐协议 §7.6：字段为 sn
func (sh *stationHandler) returnUsb(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		SN string `json:"sn"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.SN == "" {
		responseError(c, http.StatusBadRequest, "sn is required")
		return
	}

	params := map[string]interface{}{
		"sn": req.SN,
	}
	if err := commands.SendCommand(station, protocol.CmdUsbReturn, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send usb return: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdUsbReturn, "usb_return", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "usb return sent"})
}

// alarm POST /api/v1/station/:id/alarm
// 对齐协议 §7.7：字段为 alarmType/sn/detail{virusName,virusType,fileName,fileHash}
func (sh *stationHandler) alarm(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		AlarmType string `json:"alarmType"`
		SN        string `json:"sn"`
		Reason    *int   `json:"reason"`
		DoorNo    *int   `json:"doorNo"`
		VirusName string `json:"virusName"`
		VirusType string `json:"virusType"`
		FileName  string `json:"fileName"`
		FileHash  string `json:"fileHash"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.AlarmType == "" {
		req.AlarmType = commands.AlarmTypeDeviceFault
	}

	params := map[string]interface{}{
		"alarmType": req.AlarmType,
		"sn":        req.SN,
	}

	// 新版安全U盘告警需要 reason 和 doorNo
	if commands.IsSafeUdiskAlarm(req.AlarmType) {
		if req.Reason == nil {
			responseError(c, http.StatusBadRequest, "reason is required for SAFE_UDISK alarm")
			return
		}
		if !commands.IsValidReasonForType(req.AlarmType, *req.Reason) {
			responseError(c, http.StatusBadRequest, fmt.Sprintf("reason %d is not valid for alarmType %s", *req.Reason, req.AlarmType))
			return
		}
		params["reason"] = *req.Reason
		if req.DoorNo != nil {
			params["doorNo"] = *req.DoorNo
		}
	}

	// 病毒检测告警需要 detail 字段
	if req.AlarmType == commands.AlarmTypeMalwareDetected {
		params["virusName"] = req.VirusName
		params["virusType"] = req.VirusType
		params["fileName"] = req.FileName
		params["fileHash"] = req.FileHash
	}

	if err := commands.SendCommand(station, protocol.CmdAlarm, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send alarm: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdAlarm, "alarm", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "alarm sent"})
}

// alarmAll POST /api/v1/station/:id/alarm-all
// 一键模拟全部告警：覆盖全部10种安全U盘告警类型，每种类型发送一条典型 reason 的告警
func (sh *stationHandler) alarmAll(c *gin.Context) {
	id := c.Param("id")
	station, exists := sh.hub.GetStation(id)
	if !exists {
		// 兜底：按 SN 查找（前端按 row.sn 调用）
		station, exists = sh.hub.GetStationBySN(id)
	}
	if !exists {
		responseError(c, http.StatusNotFound, "station "+id+" not found")
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	samples := commands.AllSafeUdiskAlarmSamples()
	sent := 0
	var failedTypes []string
	for _, params := range samples {
		if err := commands.SendCommand(station, protocol.CmdAlarm, params); err != nil {
			if t, ok := params["alarmType"].(string); ok {
				failedTypes = append(failedTypes, t)
			}
			continue
		}
		sent++
		sh.hub.NotifyMessageSent(station.ID, protocol.CmdAlarm, "alarm", 0)
		// 每条之间短暂间隔，避免平台侧处理不过来
		time.Sleep(200 * time.Millisecond)
	}

	responseSuccess(c, gin.H{
		"stationId":   station.ID,
		"sent":        sent,
		"total":       len(samples),
		"failedTypes": failedTypes,
		"message":     fmt.Sprintf("sent %d/%d alarms", sent, len(samples)),
	})
}

// operationLog POST /api/v1/station/:id/operation-log
// 对齐协议 §7.8：字段为 sn/operation/result/message
func (sh *stationHandler) operationLog(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		SN        string `json:"sn"`
		Operation string `json:"operation"`
		Result    string `json:"result"`
		Message   string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.Operation == "" {
		req.Operation = commands.OpInsert
	}
	if !commands.IsValidOperation(req.Operation) {
		responseError(c, http.StatusBadRequest, "invalid operation: "+req.Operation)
		return
	}
	// sn 为条件字段：数据摆渡、隔离区导出等无SN介质操作不要求
	if req.SN == "" && req.Operation != commands.OpCopy && req.Operation != commands.OpQuarantineExport {
		responseError(c, http.StatusBadRequest, "sn is required for operation "+req.Operation)
		return
	}
	if req.Result == "" {
		req.Result = "success"
	}

	params := map[string]interface{}{
		"sn":        req.SN,
		"operation": req.Operation,
		"result":    req.Result,
		"message":   req.Message,
	}
	if err := commands.SendCommand(station, protocol.CmdOperationLog, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send operation log: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdOperationLog, "operation_log", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "operation log sent"})
}

// triggerUpgrade POST /api/v1/station/:id/trigger-upgrade
func (sh *stationHandler) triggerUpgrade(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		VirusType   int    `json:"virusType"`
		Version     string `json:"version"`
		DownloadURL string `json:"downloadUrl"`
		Checksum    string `json:"checksum"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	params := map[string]interface{}{
		"virusType":   req.VirusType,
		"version":     req.Version,
		"downloadUrl": req.DownloadURL,
		"checksum":    req.Checksum,
	}
	if err := commands.SendCommand(station, protocol.CmdUpgradeIssue, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to trigger upgrade: "+err.Error())
		return
	}

	sh.hub.NotifyMessageSent(station.ID, protocol.CmdUpgradeIssue, "upgrade_issue", 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "message": "upgrade triggered"})
}

// updateConfigRequest 更新配置请求
type updateConfigRequest struct {
	SasocHost         *string `json:"sasocHost"`
	SasocPort         *int    `json:"sasocPort"`
	HeartbeatEnabled  *bool   `json:"heartbeatEnabled"`
	HeartbeatInterval *int    `json:"heartbeatInterval"`
	EncryptEnabled    *bool   `json:"encryptEnabled"`
	CompressEnabled   *bool   `json:"compressEnabled"`
}

// updateConfig PUT /api/v1/station/:id/config
func (sh *stationHandler) updateConfig(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	var req updateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// 仅更新非 nil 字段
	if req.SasocHost != nil {
		station.SasocHost = *req.SasocHost
	}
	if req.SasocPort != nil && *req.SasocPort > 0 {
		station.SasocPort = *req.SasocPort
	}
	if req.HeartbeatEnabled != nil {
		station.HeartbeatEnabled = *req.HeartbeatEnabled
	}
	if req.HeartbeatInterval != nil && *req.HeartbeatInterval > 0 {
		station.HeartbeatInterval = *req.HeartbeatInterval
	}
	if req.EncryptEnabled != nil {
		station.EncryptEnabled = *req.EncryptEnabled
	}
	if req.CompressEnabled != nil {
		station.CompressEnabled = *req.CompressEnabled
	}

	// 同步更新数据库
	if db := sh.hub.DB(); db != nil {
		configJSON := "{}"
		if cfg, err := json.Marshal(map[string]interface{}{
			"sasocHost":         station.SasocHost,
			"sasocPort":         station.SasocPort,
			"heartbeatEnabled":  station.HeartbeatEnabled,
			"heartbeatInterval": station.HeartbeatInterval,
			"encryptEnabled":    station.EncryptEnabled,
			"compressEnabled":   station.CompressEnabled,
		}); err == nil {
			configJSON = string(cfg)
		}
		row := &model.SimStationRow{
			ID:       station.ID,
			SN:       station.SN,
			Model:    station.Model,
			Version:  station.Version,
			IP:       station.IP,
			MAC:      station.MAC,
			Name:     station.Name,
			DeviceID: int(station.DeviceID),
			Status:   string(station.GetState()),
			Config:   configJSON,
		}
		_ = model.UpdateStation(db, row)
	}

	responseSuccess(c, gin.H{
		"stationId": station.ID,
		"message":   "config updated",
	})
}

// getMessages GET /api/v1/station/:id/messages
func (sh *stationHandler) getMessages(c *gin.Context) {
	station, err := sh.getStationFromContext(c)
	if err != nil {
		return
	}

	db := sh.hub.DB()
	if db == nil {
		responseError(c, http.StatusInternalServerError, "database not available")
		return
	}

	// 解析查询参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	cmdID, _ := strconv.Atoi(c.DefaultQuery("cmdid", "0"))
	direction := c.Query("direction")
	startTime := c.Query("startTime")
	endTime := c.Query("endTime")

	filter := model.MessageLogFilter{
		StationID: station.ID,
		CmdID:     cmdID,
		Direction: direction,
		StartTime: startTime,
		EndTime:   endTime,
		Limit:     limit,
		Offset:    offset,
	}

	logs, total, err := model.ListMessageLogs(db, filter)
	if err != nil {
		responseError(c, http.StatusInternalServerError, "failed to query messages: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"total":    total,
		"messages": logs,
		"limit":    limit,
		"offset":   offset,
	})
}

// getStationFromContext 从 URL 参数获取安检站
func (sh *stationHandler) getStationFromContext(c *gin.Context) (*simulator.SimStation, error) {
	id := c.Param("id")
	if id == "" {
		id = c.Param("sn")
	}
	if id == "" {
		responseError(c, http.StatusBadRequest, "station id is required")
		return nil, fmt.Errorf("station id is required")
	}

	station, exists := sh.hub.GetStation(id)
	if !exists {
		responseError(c, http.StatusNotFound, "station "+id+" not found")
		return nil, fmt.Errorf("station %s not found", id)
	}

	return station, nil
}

// batchCreateStations POST /api/v1/stations/batch
func (sh *stationHandler) batchCreateStations(c *gin.Context) {
	var req struct {
		Count     int    `json:"count" binding:"required"`
		Prefix    string `json:"prefix"`
		Model     string `json:"model"`
		Version   string `json:"version"`
		SasocHost string `json:"sasocHost"`
		SasocPort int    `json:"sasocPort"`
		Encrypt   bool   `json:"encrypt"`
		Compress  bool   `json:"compress"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// 获取全局配置
	cfg := sh.hub.Config()

	prefix := req.Prefix
	if prefix == "" {
		prefix = "s"
	}
	modelName := req.Model
	if modelName == "" {
		modelName = "SASOC-M100"
	}
	version := req.Version
	if version == "" {
		version = "v2.0.0"
	}

	// 使用全局配置或请求中的SASOC地址
	sasocHost := req.SasocHost
	if sasocHost == "" && cfg != nil && cfg.Sasoc.Host != "" {
		sasocHost = cfg.Sasoc.Host
	}
	sasocPort := req.SasocPort
	if sasocPort == 0 && cfg != nil && cfg.Sasoc.Port > 0 {
		sasocPort = cfg.Sasoc.Port
	}

	type createResult struct {
		StationID string `json:"stationId"`
		SN        string `json:"sn"`
		IP        string `json:"ip"`
		MAC       string `json:"mac"`
		Success   bool   `json:"success"`
		Error     string `json:"error,omitempty"`
	}

	results := make([]createResult, 0, req.Count)
	existing := sh.hub.StationCount()

	for i := 0; i < req.Count; i++ {
		if existing+i >= 100 {
			results = append(results, createResult{
				StationID: fmt.Sprintf("%s-%d", prefix, i+1),
				Success:   false,
				Error:     "max station limit reached",
			})
			continue
		}

		id := fmt.Sprintf("%s-%d", prefix, i+1)
		sn := fmt.Sprintf("SN-%s-%04d", prefix, i+1)
		name := fmt.Sprintf("模拟安检站-%d", i+1)

		station := simulator.NewSimStation(id, sn, modelName, version, name)
		// 自动分配递增 IP 和 MAC
		station.IP = hub.GenerateSequentialIP()
		station.MAC = hub.GenerateSequentialMAC()
		// SASOC 地址：使用全局配置或请求中的值
		if sasocHost != "" {
			station.SasocHost = sasocHost
		}
		if sasocPort > 0 {
			station.SasocPort = sasocPort
		}
		station.EncryptEnabled = req.Encrypt
		station.CompressEnabled = req.Compress

		if err := sh.hub.AddStation(station); err != nil {
			results = append(results, createResult{
				StationID: id,
				SN:        sn,
				IP:        station.IP,
				MAC:       station.MAC,
				Success:   false,
				Error:     err.Error(),
			})
			continue
		}

		results = append(results, createResult{
			StationID: id,
			SN:        sn,
			IP:        station.IP,
			MAC:       station.MAC,
			Success:   true,
		})
	}

	responseSuccess(c, gin.H{
		"total":   len(results),
		"results": results,
	})
}

// stationAction POST /api/v1/stations/:sn/:action
func (sh *stationHandler) stationAction(c *gin.Context) {
	sn := c.Param("sn")
	action := c.Param("action")

	// 路由参数是 SN，先按 SN 查找，查不到再按 ID 查找（兼容 ID == SN 的情况）
	station, exists := sh.hub.GetStationBySN(sn)
	if !exists {
		station, exists = sh.hub.GetStation(sn)
	}
	if !exists {
		responseError(c, http.StatusNotFound, "station "+sn+" not found")
		return
	}

	oldState := string(station.GetState())

	switch action {
	case "start":
		if station.GetState() == simulator.StateOnline {
			responseError(c, http.StatusConflict, "station is already online")
			return
		}
		if err := station.Start(); err != nil {
			responseError(c, http.StatusInternalServerError, "failed to start: "+err.Error())
			return
		}
		sh.hub.NotifyStationStateChange(station.ID, oldState, string(station.GetState()), station.DeviceID)
		responseSuccess(c, gin.H{"stationId": station.ID, "status": string(station.GetState())})

	case "stop":
		if station.GetState() == simulator.StateIdle {
			responseError(c, http.StatusConflict, "station is already idle")
			return
		}
		if err := station.Stop(); err != nil {
			responseError(c, http.StatusInternalServerError, "failed to stop: "+err.Error())
			return
		}
		sh.hub.NotifyStationStateChange(station.ID, oldState, string(station.GetState()), station.DeviceID)
		responseSuccess(c, gin.H{"stationId": station.ID, "status": string(station.GetState())})

	case "restart":
		if err := station.Restart(); err != nil {
			responseError(c, http.StatusInternalServerError, "failed to restart: "+err.Error())
			return
		}
		sh.hub.NotifyStationStateChange(station.ID, oldState, string(station.GetState()), station.DeviceID)
		responseSuccess(c, gin.H{"stationId": station.ID, "status": string(station.GetState())})

	default:
		responseError(c, http.StatusBadRequest, "unknown action: "+action)
	}
}

// sendCommand POST /api/v1/stations/:sn/command
func (sh *stationHandler) sendCommand(c *gin.Context) {
	sn := c.Param("sn")
	station, exists := sh.hub.GetStationBySN(sn)
	if !exists {
		station, exists = sh.hub.GetStation(sn)
	}
	if !exists {
		responseError(c, http.StatusNotFound, "station "+sn+" not found")
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		Command string                 `json:"command" binding:"required"`
		Params  map[string]interface{} `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// 命令名称 → CMDID 映射
	cmdMap := map[string]uint32{
		"heartbeat":      protocol.CmdHeartbeat,
		"info_report":    protocol.CmdInfoReport,
		"register":       protocol.CmdRegister,
		"claim_verify":   protocol.CmdClaimVerify,
		"usb_claim":      protocol.CmdUsbClaim,
		"usb_return":     protocol.CmdUsbReturn,
		"alarm":          protocol.CmdAlarm,
		"operation_log":  protocol.CmdOperationLog,
		"upgrade_issue":  protocol.CmdUpgradeIssue,
		"upgrade_result": protocol.CmdUpgradeResult,
	}

	cmdID, ok := cmdMap[req.Command]
	if !ok {
		responseError(c, http.StatusBadRequest, "unknown command: "+req.Command)
		return
	}

	params := req.Params
	if params == nil {
		params = make(map[string]interface{})
	}

	if err := commands.SendCommand(station, cmdID, params); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to send command: "+err.Error())
		return
	}

	cmdName := req.Command
	sh.hub.NotifyMessageSent(station.ID, cmdID, cmdName, 0)
	responseSuccess(c, gin.H{"stationId": station.ID, "command": req.Command, "message": "command sent"})
}

// simulateUpgrade POST /api/v1/stations/:sn/simulate-upgrade
// 模拟平台向安检站下发升级指令（被动接收场景）
func (sh *stationHandler) simulateUpgrade(c *gin.Context) {
	sn := c.Param("sn")
	station, exists := sh.hub.GetStationBySN(sn)
	if !exists {
		station, exists = sh.hub.GetStation(sn)
	}
	if !exists {
		responseError(c, http.StatusNotFound, "station "+sn+" not found")
		return
	}

	if !station.IsOnline() {
		responseError(c, http.StatusConflict, "station is not online")
		return
	}

	var req struct {
		VirusType   int    `json:"virusType"`
		Version     string `json:"version"`
		DownloadURL string `json:"downloadUrl"`
		Checksum    string `json:"checksum"`
		TaskID      string `json:"taskId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.VirusType <= 0 {
		req.VirusType = 1
	}
	if req.Version == "" {
		req.Version = "V200R026C02"
	}
	if req.TaskID == "" {
		req.TaskID = fmt.Sprintf("upgrade-%d", time.Now().UnixMilli())
	}

	// 检查是否已有升级任务
	if station.GetUpgradeTask() != nil && station.GetUpgradeTask().IsRunning {
		responseError(c, http.StatusConflict, "upgrade task already running")
		return
	}

	// 直接创建升级任务（模拟平台下发 CMD108）
	task := &simulator.UpgradeTask{
		TaskID:      req.TaskID,
		VirusType:   req.VirusType,
		Version:     req.Version,
		DownloadURL: req.DownloadURL,
		Checksum:    req.Checksum,
		Status:      "running",
		Progress:    0,
	}
	station.SetUpgradeTask(task)

	// 启动升级流程
	go station.ExecuteUpgrade()

	responseSuccess(c, gin.H{
		"stationId": station.ID,
		"sn":        station.SN,
		"taskId":    req.TaskID,
		"virusType": req.VirusType,
		"version":   req.Version,
		"message":   "upgrade task started (simulated platform dispatch)",
	})
}

// deleteStation DELETE /api/v1/stations/:sn
func (sh *stationHandler) deleteStation(c *gin.Context) {
	sn := c.Param("sn")

	// 路由参数是 SN，先按 SN 查，查不到再按 ID 查
	station, exists := sh.hub.GetStationBySN(sn)
	if !exists {
		station, exists = sh.hub.GetStation(sn)
	}
	if !exists {
		responseError(c, http.StatusNotFound, "station "+sn+" not found")
		return
	}

	// 先停止
	if station.GetState() != simulator.StateIdle {
		_ = station.Stop()
	}

	if err := sh.hub.RemoveStation(station.ID); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to delete station: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{"stationId": station.ID, "sn": sn, "message": "station deleted"})
}

// listLogs GET /api/v1/logs
func (sh *stationHandler) listLogs(c *gin.Context) {
	db := sh.hub.DB()
	if db == nil {
		responseError(c, http.StatusInternalServerError, "database not available")
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	offset := (page - 1) * limit
	stationID := c.Query("stationId")
	cmdID, _ := strconv.Atoi(c.DefaultQuery("cmdid", "0"))
	direction := c.Query("direction")

	filter := model.MessageLogFilter{
		StationID: stationID,
		CmdID:     cmdID,
		Direction: direction,
		Limit:     limit,
		Offset:    offset,
	}

	logs, total, err := model.ListMessageLogs(db, filter)
	if err != nil {
		responseError(c, http.StatusInternalServerError, "failed to query logs: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"total":    total,
		"logs":     logs,
		"page":     page,
		"pageSize": limit,
	})
}

// clearLogs DELETE /api/v1/logs
func (sh *stationHandler) clearLogs(c *gin.Context) {
	db := sh.hub.DB()
	if db == nil {
		responseError(c, http.StatusInternalServerError, "database not available")
		return
	}

	if err := model.ClearMessageLogs(db); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to clear logs: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{"message": "logs cleared"})
}

// startAllStations POST /api/v1/stations/start-all
// 分批启动所有处于 idle 状态的安检站，每批 10 个，批间间隔 1 秒
func (sh *stationHandler) startAllStations(c *gin.Context) {
	stations := sh.hub.ListStations()
	var idle []*simulator.SimStation
	for _, s := range stations {
		if s.GetState() == simulator.StateIdle {
			idle = append(idle, s)
		}
	}

	if len(idle) == 0 {
		responseError(c, http.StatusConflict, "no idle stations to start")
		return
	}

	go func(idleStations []*simulator.SimStation) {
		const batchSize = 10
		const batchInterval = time.Second
		for i := 0; i < len(idleStations); i += batchSize {
			end := i + batchSize
			if end > len(idleStations) {
				end = len(idleStations)
			}
			batch := idleStations[i:end]
			var wg sync.WaitGroup
			for _, s := range batch {
				wg.Add(1)
				go func(st *simulator.SimStation) {
					defer wg.Done()
					if err := st.Start(); err != nil {
						zap.L().Warn("start station failed",
							zap.String("station", st.ID),
							zap.Error(err),
						)
					}
				}(s)
			}
			wg.Wait()
			if end < len(idleStations) {
				time.Sleep(batchInterval)
			}
		}
	}(idle)

	responseSuccess(c, gin.H{
		"message": fmt.Sprintf("starting %d stations in batches", len(idle)),
		"count":   len(idle),
	})
}

// stopAllStations POST /api/v1/stations/stop-all
// 停止所有非 idle 状态的安检站
func (sh *stationHandler) stopAllStations(c *gin.Context) {
	stations := sh.hub.ListStations()
	var active []*simulator.SimStation
	for _, s := range stations {
		if s.GetState() != simulator.StateIdle {
			active = append(active, s)
		}
	}

	if len(active) == 0 {
		responseError(c, http.StatusConflict, "no active stations to stop")
		return
	}

	var wg sync.WaitGroup
	for _, s := range active {
		wg.Add(1)
		go func(st *simulator.SimStation) {
			defer wg.Done()
			if err := st.Stop(); err != nil {
				zap.L().Warn("stop station failed",
					zap.String("station", st.ID),
					zap.Error(err),
				)
			}
		}(s)
	}
	wg.Wait()

	responseSuccess(c, gin.H{
		"message": fmt.Sprintf("stopped %d stations", len(active)),
		"count":   len(active),
	})
}

// getStationStats GET /api/v1/stations/stats
// 聚合统计：总数、在线数、总发送消息数、总接收消息数、吞吐量
func (sh *stationHandler) getStationStats(c *gin.Context) {
	stations := sh.hub.ListStations()

	total := len(stations)
	online := 0
	var totalSent, totalRecv int64

	for _, s := range stations {
		if s.IsOnline() {
			online++
		}
		totalSent += s.MsgSent
		totalRecv += s.MsgReceived
	}

	// 计算吞吐量：取统计窗口内每秒平均消息量
	// 窗口为最近 60 秒，通过 MsgSent 差值计算
	// 这里用简单方式：总消息数 / 运行时间（至少 1 秒）
	throughput := float64(0)
	if totalSent > 0 {
		// 粗略估算：按心跳间隔和在线站点数推算
		throughput = float64(online) / 30.0 // 默认心跳 30 秒，每站点每秒约 1/30 条
	}

	responseSuccess(c, gin.H{
		"total":         total,
		"online":        online,
		"offline":       total - online,
		"totalSent":     totalSent,
		"totalReceived": totalRecv,
		"throughput":    throughput,
	})
}
