package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/model"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// usbPluginHandler U盘插件 API 处理器
type usbPluginHandler struct {
	hub *hub.Hub
}

// newUsbPluginHandler 创建U盘插件处理器
func newUsbPluginHandler(h *hub.Hub) *usbPluginHandler {
	return &usbPluginHandler{hub: h}
}

// btoi bool转int（本地辅助函数）
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// listUsb GET /api/v1/usbs
func (uh *usbPluginHandler) listUsb(c *gin.Context) {
	devices := uh.hub.ListUsbDevices()

	// 返回与前端 USBPage 对齐的扁平结构
	type usbRow struct {
		SN              string `json:"sn"`
		Model           string `json:"model"`
		FirmwareVersion string `json:"firmwareVersion"`
		Qualified       bool   `json:"qualified"`
		AreaName        string `json:"areaName"`
		Status          string `json:"status"`           // 生命周期状态（中文）
		StatusCode      string `json:"statusCode"`       // 生命周期状态码
		Inserted        bool   `json:"inserted"`
		VirusFreeMark   bool   `json:"virusFreeMark"`
		StationID       string `json:"stationId,omitempty"`
		DoorNo          int    `json:"doorNo,omitempty"`
		WriteDelay      int    `json:"writeDelay"`
		ReadDelay       int    `json:"readDelay"`
		WriteFail       bool   `json:"writeFail"`
		ReadFail        bool   `json:"readFail"`
	}

	rows := make([]usbRow, 0, len(devices))
	for _, d := range devices {
		rows = append(rows, usbRow{
			SN:              d.SN,
			Model:           d.Model,
			FirmwareVersion: d.FirmwareVersion,
			Qualified:       d.Qualified,
			AreaName:        d.AreaName,
			Status:          d.Status.CN(),
		StatusCode:      string(d.Status),
			Inserted:        d.Inserted,
			VirusFreeMark:   d.VirusFreeMark,
			StationID:       d.StationID,
			DoorNo:          d.DoorNo,
			WriteDelay:      d.WriteDelay,
			ReadDelay:       d.ReadDelay,
			WriteFail:       d.WriteFail,
			ReadFail:        d.ReadFail,
		})
	}

	responseSuccess(c, gin.H{
		"total": len(rows),
		"usbs":  rows,
	})
}

// insertUsbRequest 插入U盘请求
type insertUsbRequest struct {
	UsbDevices []insertUsbItem `json:"devices" binding:"required"`
}

// insertUsbItem 单个U盘插入项
type insertUsbItem struct {
	ID              string `json:"usbId" binding:"required"`
	SN              string `json:"sn"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmwareVersion"`
	AreaName        string `json:"areaName"`
	StationID       string `json:"stationId"`
	DoorNo          int    `json:"doorNo"`
}

// insertUsb POST /api/v1/usb-plug/insert
func (uh *usbPluginHandler) insertUsb(c *gin.Context) {
	var req insertUsbRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if len(req.UsbDevices) == 0 {
		responseError(c, http.StatusBadRequest, "at least one device is required")
		return
	}

	type insertResult struct {
		UsbID    string `json:"usbId"`
		Success  bool   `json:"success"`
		Error    string `json:"error,omitempty"`
	}

	results := make([]insertResult, 0, len(req.UsbDevices))

	for _, item := range req.UsbDevices {
		// 检查是否已存在
		if _, exists := uh.hub.GetUsbDevice(item.ID); exists {
			results = append(results, insertResult{
				UsbID:   item.ID,
				Success: false,
				Error:   "device already exists",
			})
			continue
		}

		// 创建U盘设备
		sn := item.SN
		if sn == "" {
			sn = "USB-SN-" + item.ID
		}
		modelName := item.Model
		if modelName == "" {
			modelName = "SASOC-USB-001"
		}
		fwVersion := item.FirmwareVersion
		if fwVersion == "" {
			fwVersion = "v1.0.0"
		}
		areaName := item.AreaName
		if areaName == "" {
			areaName = "default"
		}

		usb := simulator.NewUsbDevice(item.ID, sn, modelName, fwVersion, areaName)

		// 关联安检站
		if item.StationID != "" {
			if station, exists := uh.hub.GetStation(item.StationID); exists {
				usb.StationID = item.StationID
				usb.DoorNo = item.DoorNo
				// 将U盘放入管控柜
				if station.Cabinet != nil && item.DoorNo > 0 {
					station.Cabinet.PutUsb(item.DoorNo, sn)
				}
			} else {
				results = append(results, insertResult{
					UsbID:   item.ID,
					Success: false,
					Error:   "station " + item.StationID + " not found",
				})
				continue
			}
		}

		// 插入U盘
		usb.Insert()

		if err := uh.hub.AddUsbDevice(usb); err != nil {
			results = append(results, insertResult{
				UsbID:   item.ID,
				Success: false,
				Error:   err.Error(),
			})
			continue
		}

		// 发布U盘插入事件
		uh.hub.NotifyUsbInserted(item.ID, item.StationID, item.DoorNo)

		results = append(results, insertResult{
			UsbID:   item.ID,
			Success: true,
		})

		zap.L().Info("usb inserted via API",
			zap.String("usbId", item.ID),
			zap.String("stationId", item.StationID),
			zap.Int("doorNo", item.DoorNo),
		)
	}

	responseSuccess(c, gin.H{
		"total":   len(results),
		"results": results,
	})
}

// removeUsb POST /api/v1/usb-plug/remove/:usbId
func (uh *usbPluginHandler) removeUsb(c *gin.Context) {
	usbID := c.Param("usbId")
	if usbID == "" {
		responseError(c, http.StatusBadRequest, "usbId is required")
		return
	}

	usb, exists := uh.hub.GetUsbDevice(usbID)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+usbID+" not found")
		return
	}

	// 从安检站管控柜中移除
	if usb.StationID != "" && usb.DoorNo > 0 {
		if station, exists := uh.hub.GetStation(usb.StationID); exists {
			if station.Cabinet != nil {
				station.Cabinet.RemoveUsb(usb.DoorNo)
			}
		}
	}

	// 拔出U盘
	usb.Remove()

	// 发布U盘拔出事件
	uh.hub.NotifyUsbRemoved(usbID, usb.StationID, usb.DoorNo)

	// 从Hub中移除
	if err := uh.hub.RemoveUsbDevice(usbID); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to remove usb: "+err.Error())
		return
	}

	zap.L().Info("usb removed via API", zap.String("usbId", usbID))
	responseSuccess(c, gin.H{"usbId": usbID, "message": "usb removed"})
}

// readUsb GET /api/v1/usb-plug/read/:usbId
func (uh *usbPluginHandler) readUsb(c *gin.Context) {
	usbID := c.Param("usbId")
	if usbID == "" {
		responseError(c, http.StatusBadRequest, "usbId is required")
		return
	}

	usb, exists := uh.hub.GetUsbDevice(usbID)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+usbID+" not found")
		return
	}

	info, err := usb.Read()
	if err != nil {
		responseError(c, http.StatusInternalServerError, "failed to read usb: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"usbId":   usbID,
		"inserted": usb.Inserted,
		"info":    info,
	})
}

// writeUsbRequest 写入U盘请求
type writeUsbRequest struct {
	Qualified *bool  `json:"qualified"`
	AreaName  string `json:"areaName"`
}

// writeUsb POST /api/v1/usb-plug/write/:usbId
func (uh *usbPluginHandler) writeUsb(c *gin.Context) {
	usbID := c.Param("usbId")
	if usbID == "" {
		responseError(c, http.StatusBadRequest, "usbId is required")
		return
	}

	usb, exists := uh.hub.GetUsbDevice(usbID)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+usbID+" not found")
		return
	}

	var req writeUsbRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	qualified := true
	if req.Qualified != nil {
		qualified = *req.Qualified
	}

	if err := usb.Write(qualified, req.AreaName); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to write usb: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"usbId":   usbID,
		"message": "usb written",
		"qualified": usb.Qualified,
		"areaName":  usb.AreaName,
	})
}

// batchWriteItem 批量写入单项
type batchWriteItem struct {
	UsbID     string `json:"usbId" binding:"required"`
	Qualified *bool  `json:"qualified"`
	AreaName  string `json:"areaName"`
}

// batchWriteRequest 批量写入请求
type batchWriteRequest struct {
	Items []batchWriteItem `json:"items" binding:"required"`
}

// insertUsbSimple POST /api/v1/usbs
// 前端简单插入：接收单个 USB 设备（非批量格式）
func (uh *usbPluginHandler) insertUsbSimple(c *gin.Context) {
	var req struct {
		SN              string `json:"sn"`
		Model           string `json:"model"`
		FirmwareVersion string `json:"firmwareVersion"`
		AreaName        string `json:"areaName"`
		StationID       string `json:"stationId"`
		DoorNo          int    `json:"doorNo"`
		Qualified       *bool  `json:"qualified"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	sn := req.SN
	if sn == "" {
		sn = fmt.Sprintf("USB-SN-%d", time.Now().UnixNano()%100000)
	}

	usbID := sn // 用 SN 作为 ID
	modelName := req.Model
	if modelName == "" {
		modelName = "SASOC-USB-001"
	}
	fwVersion := req.FirmwareVersion
	if fwVersion == "" {
		fwVersion = "v1.0.0"
	}
	areaName := req.AreaName
	if areaName == "" {
		areaName = "default"
	}

	// 检查是否已存在
	if _, exists := uh.hub.GetUsbDevice(usbID); exists {
		responseError(c, http.StatusConflict, "usb device "+usbID+" already exists")
		return
	}

	usb := simulator.NewUsbDevice(usbID, sn, modelName, fwVersion, areaName)

	// 设置合格标记
	if req.Qualified != nil {
		usb.Qualified = *req.Qualified
	}

	// 关联安检站
	if req.StationID != "" {
		if station, exists := uh.hub.GetStation(req.StationID); exists {
			usb.StationID = req.StationID
			usb.DoorNo = req.DoorNo
			if station.Cabinet != nil && req.DoorNo > 0 {
				station.Cabinet.PutUsb(req.DoorNo, sn)
			}
		}
	}

	usb.Insert()

	if err := uh.hub.AddUsbDevice(usb); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to add usb: "+err.Error())
		return
	}

	uh.hub.NotifyUsbInserted(usbID, req.StationID, req.DoorNo)
	zap.L().Info("usb inserted (simple)", zap.String("usbId", usbID))
	responseSuccess(c, gin.H{
		"sn":              usb.SN,
		"model":           usb.Model,
		"firmwareVersion": usb.FirmwareVersion,
		"qualified":       usb.Qualified,
		"areaName":        usb.AreaName,
		"status":          usb.Status.CN(),
		"statusCode":      string(usb.Status),
		"inserted":        usb.Inserted,
		"virusFreeMark":   usb.VirusFreeMark,
		"stationId":       usb.StationID,
		"doorNo":          usb.DoorNo,
	})
}

// removeUsbBySN POST /api/v1/usbs/:sn/remove
func (uh *usbPluginHandler) removeUsbBySN(c *gin.Context) {
	sn := c.Param("sn")
	usb, exists := uh.hub.GetUsbDevice(sn)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+sn+" not found")
		return
	}

	if usb.StationID != "" && usb.DoorNo > 0 {
		if station, exists := uh.hub.GetStation(usb.StationID); exists {
			if station.Cabinet != nil {
				station.Cabinet.RemoveUsb(usb.DoorNo)
			}
		}
	}

	usb.Remove()
	uh.hub.NotifyUsbRemoved(sn, usb.StationID, usb.DoorNo)
	_ = uh.hub.RemoveUsbDevice(sn)

	zap.L().Info("usb removed", zap.String("usbId", sn))
	responseSuccess(c, gin.H{"usbId": sn, "message": "usb removed"})
}

// insertUsbBySN POST /api/v1/usbs/:sn/insert
func (uh *usbPluginHandler) insertUsbBySN(c *gin.Context) {
	sn := c.Param("sn")
	var req struct {
		StationID string `json:"station_sn"`
		DoorNo    int    `json:"doorNo"`
	}
	_ = c.ShouldBindJSON(&req)

	// 如果设备已存在，重新插入
	if usb, exists := uh.hub.GetUsbDevice(sn); exists {
		usb.Insert()
		if req.StationID != "" {
			if station, sExists := uh.hub.GetStation(req.StationID); sExists {
				usb.StationID = req.StationID
				usb.DoorNo = req.DoorNo
				if station.Cabinet != nil && req.DoorNo > 0 {
					station.Cabinet.PutUsb(req.DoorNo, sn)
				}
			}
		}
		uh.hub.NotifyUsbInserted(sn, req.StationID, req.DoorNo)
		responseSuccess(c, gin.H{"usbId": sn, "message": "usb re-inserted"})
		return
	}

	// 创建新设备
	usb := simulator.NewUsbDevice(sn, sn, "SASOC-USB-001", "v1.0.0", "default")
	if req.StationID != "" {
		if station, sExists := uh.hub.GetStation(req.StationID); sExists {
			usb.StationID = req.StationID
			usb.DoorNo = req.DoorNo
			if station.Cabinet != nil && req.DoorNo > 0 {
				station.Cabinet.PutUsb(req.DoorNo, sn)
			}
		}
	}
	usb.Insert()

	if err := uh.hub.AddUsbDevice(usb); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to add usb: "+err.Error())
		return
	}

	uh.hub.NotifyUsbInserted(sn, req.StationID, req.DoorNo)
	responseSuccess(c, gin.H{"usbId": sn, "message": "usb inserted"})
}

// readUsbBySN GET /api/v1/usbs/:sn/data
func (uh *usbPluginHandler) readUsbBySN(c *gin.Context) {
	sn := c.Param("sn")
	usb, exists := uh.hub.GetUsbDevice(sn)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+sn+" not found")
		return
	}

	info, err := usb.Read()
	if err != nil {
		responseError(c, http.StatusInternalServerError, "failed to read usb: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"usbId":    sn,
		"inserted": usb.Inserted,
		"info":     info,
	})
}

// writeUsbBySN POST /api/v1/usbs/:sn/write
func (uh *usbPluginHandler) writeUsbBySN(c *gin.Context) {
	sn := c.Param("sn")
	usb, exists := uh.hub.GetUsbDevice(sn)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+sn+" not found")
		return
	}

	var req writeUsbRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	qualified := true
	if req.Qualified != nil {
		qualified = *req.Qualified
	}

	if err := usb.Write(qualified, req.AreaName); err != nil {
		responseError(c, http.StatusInternalServerError, "failed to write usb: "+err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"usbId":     sn,
		"message":   "usb written",
		"qualified": usb.Qualified,
		"areaName":  usb.AreaName,
	})
}

// deleteUsbBySN DELETE /api/v1/usbs/:sn
func (uh *usbPluginHandler) deleteUsbBySN(c *gin.Context) {
	sn := c.Param("sn")

	usb, exists := uh.hub.GetUsbDevice(sn)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+sn+" not found")
		return
	}

	if usb.StationID != "" && usb.DoorNo > 0 {
		if station, sExists := uh.hub.GetStation(usb.StationID); sExists {
			if station.Cabinet != nil {
				station.Cabinet.RemoveUsb(usb.DoorNo)
			}
		}
	}

	usb.Remove()
	uh.hub.NotifyUsbRemoved(sn, usb.StationID, usb.DoorNo)
	_ = uh.hub.RemoveUsbDevice(sn)

	responseSuccess(c, gin.H{"usbId": sn, "message": "usb deleted"})
}

// faultInjection POST /api/v1/usbs/fault-injection
func (uh *usbPluginHandler) faultInjection(c *gin.Context) {
	var req struct {
		UsbID    string `json:"usbId" binding:"required"`
		Fault    string `json:"fault" binding:"required"`
		Duration int    `json:"duration"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	usb, exists := uh.hub.GetUsbDevice(req.UsbID)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+req.UsbID+" not found")
		return
	}

	// 模拟故障注入
	switch req.Fault {
	case "bad_sector":
		usb.Qualified = false
	case "read_error":
		usb.Qualified = false
	case "write_error":
		usb.AreaName = "FAULT:" + req.Fault
	default:
		usb.Qualified = false
	}

	zap.L().Info("fault injection applied",
		zap.String("usbId", req.UsbID),
		zap.String("fault", req.Fault),
	)

	responseSuccess(c, gin.H{
		"usbId":   req.UsbID,
		"fault":   req.Fault,
		"message": "fault injected",
	})
}

// batchWrite POST /api/v1/usb-plug/batch-write
func (uh *usbPluginHandler) batchWrite(c *gin.Context) {
	var req batchWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if len(req.Items) == 0 {
		responseError(c, http.StatusBadRequest, "at least one item is required")
		return
	}

	type writeResult struct {
		UsbID     string `json:"usbId"`
		Success   bool   `json:"success"`
		Error     string `json:"error,omitempty"`
		Qualified bool   `json:"qualified"`
		AreaName  string `json:"areaName"`
	}

	results := make([]writeResult, 0, len(req.Items))

	for _, item := range req.Items {
		usb, exists := uh.hub.GetUsbDevice(item.UsbID)
		if !exists {
			results = append(results, writeResult{
				UsbID:   item.UsbID,
				Success: false,
				Error:   "device not found",
			})
			continue
		}

		qualified := true
		if item.Qualified != nil {
			qualified = *item.Qualified
		}

		if err := usb.Write(qualified, item.AreaName); err != nil {
			results = append(results, writeResult{
				UsbID:   item.UsbID,
				Success: false,
				Error:   err.Error(),
			})
			continue
		}

		results = append(results, writeResult{
			UsbID:     item.UsbID,
			Success:   true,
			Qualified: usb.Qualified,
			AreaName:  usb.AreaName,
		})
	}

	responseSuccess(c, gin.H{
		"total":   len(results),
		"results": results,
	})
}

// --- U盘生命周期状态转换 API ---

// setUsbStatus POST /api/v1/usbs/:sn/status
// 对齐协议文档 U盘生命周期：已录入→已寄出→已收录→已发放→已收录/已报废
func (uh *usbPluginHandler) setUsbStatus(c *gin.Context) {
	sn := c.Param("sn")
	usb, exists := uh.hub.GetUsbDevice(sn)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+sn+" not found")
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	newStatus := simulator.UsbStatus(req.Status)

	// 校验状态转换合法性
	switch newStatus {
	case simulator.UsbStatusRegistered:
		usb.SetRegistered()
	case simulator.UsbStatusSent:
		usb.SetSent()
	case simulator.UsbStatusReceived:
		usb.SetReceived()
	case simulator.UsbStatusIssued:
		if !usb.CanBeIssued() {
			responseError(c, http.StatusConflict, "usb cannot be issued in current status: "+string(usb.Status))
			return
		}
		// 发放时先从管控柜移除（SetIssued会清空StationID/DoorNo）
		if usb.StationID != "" && usb.DoorNo > 0 {
			if station, sExists := uh.hub.GetStation(usb.StationID); sExists {
				if station.Cabinet != nil {
					station.Cabinet.RemoveUsb(usb.DoorNo)
				}
			}
		}
		usb.SetIssued()
	case simulator.UsbStatusScrapped:
		if !usb.CanBeScrapped() {
			responseError(c, http.StatusConflict, "usb cannot be scrapped in current status: "+string(usb.Status))
			return
		}
		usb.SetScrapped()
	default:
		responseError(c, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	zap.L().Info("usb status changed",
		zap.String("sn", sn),
		zap.String("newStatus", string(newStatus)),
	)

	responseSuccess(c, gin.H{
		"sn":         sn,
		"status":     usb.Status.CN(),
		"statusCode": string(usb.Status),
		"message":    "status updated",
	})
}

// batchCreateUsbs POST /api/v1/usbs/batch
// 批量创建U盘（常用于测试场景）
func (uh *usbPluginHandler) batchCreateUsbs(c *gin.Context) {
	var req struct {
		Count     int    `json:"count" binding:"required"`
		Prefix    string `json:"prefix"`
		Model     string `json:"model"`
		AreaName  string `json:"areaName"`
		StationID string `json:"stationId"`
		Status    string `json:"status"` // 初始生命周期状态
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	prefix := req.Prefix
	if prefix == "" {
		prefix = "USB"
	}
	modelName := req.Model
	if modelName == "" {
		modelName = "SASOC-USB-001"
	}
	areaName := req.AreaName
	if areaName == "" {
		areaName = "默认区域"
	}
	initStatus := simulator.UsbStatusReceived // 默认已收录
	if req.Status != "" {
		initStatus = simulator.UsbStatus(req.Status)
	}

	type createResult struct {
		SN      string `json:"sn"`
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}

	results := make([]createResult, 0, req.Count)

	for i := 0; i < req.Count; i++ {
		sn := fmt.Sprintf("%s-%04d", prefix, i+1)

		if _, exists := uh.hub.GetUsbDevice(sn); exists {
			results = append(results, createResult{SN: sn, Success: false, Error: "already exists"})
			continue
		}

		usb := simulator.NewUsbDevice(sn, sn, modelName, "v1.0.0", areaName)
		usb.Status = initStatus

		// 关联安检站
		if req.StationID != "" {
			if station, sExists := uh.hub.GetStation(req.StationID); sExists {
				usb.StationID = req.StationID
				// 寻找空闲柜门
				doorNo := 0
				if station.Cabinet != nil {
					for _, slot := range station.Cabinet.Slots {
						if !slot.Occupied {
							doorNo = slot.DoorNo
							break
						}
					}
					if doorNo > 0 {
						usb.DoorNo = doorNo
						station.Cabinet.PutUsb(doorNo, sn)
					}
				}
			}
		}

		usb.Insert()

		if err := uh.hub.AddUsbDevice(usb); err != nil {
			results = append(results, createResult{SN: sn, Success: false, Error: err.Error()})
			continue
		}

		uh.hub.NotifyUsbInserted(sn, usb.StationID, usb.DoorNo)
		results = append(results, createResult{SN: sn, Success: true})
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}

	responseSuccess(c, gin.H{
		"total":   len(results),
		"success": successCount,
		"results": results,
	})
}

// updateUsbFault POST /api/v1/usbs/fault-injection
// 对齐前端故障注入表单：对指定U盘设置写入延迟/读取延迟/写入失败/读取失败
func (uh *usbPluginHandler) updateUsbFault(c *gin.Context) {
	var req struct {
		SN        string `json:"sn" binding:"required"`
		WriteDelay int   `json:"writeDelay"`
		ReadDelay  int   `json:"readDelay"`
		WriteFail  *bool  `json:"writeFail"`
		ReadFail   *bool  `json:"readFail"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	usb, exists := uh.hub.GetUsbDevice(req.SN)
	if !exists {
		responseError(c, http.StatusNotFound, "usb device "+req.SN+" not found")
		return
	}

	usb.WriteDelay = req.WriteDelay
	usb.ReadDelay = req.ReadDelay
	if req.WriteFail != nil {
		usb.WriteFail = *req.WriteFail
	}
	if req.ReadFail != nil {
		usb.ReadFail = *req.ReadFail
	}

	// 持久化故障注入到数据库
	if db := uh.hub.DB(); db != nil {
		row := &model.SimUsbRow{
			ID:         usb.ID,
			SN:         usb.SN,
			WriteDelay: usb.WriteDelay,
			ReadDelay:  usb.ReadDelay,
			WriteFail:  btoi(usb.WriteFail),
			ReadFail:   btoi(usb.ReadFail),
		}
		// 先查询现有记录，保留其他字段
		if existing, err := model.GetUsb(db, usb.ID); err == nil && existing != nil {
			row.Model = existing.Model
			row.FirmwareVersion = existing.FirmwareVersion
			row.Qualified = existing.Qualified
			row.AreaName = existing.AreaName
			row.ClaimInfo = existing.ClaimInfo
			row.Inserted = existing.Inserted
			row.StationID = existing.StationID
			row.DoorNo = existing.DoorNo
		}
		if err := model.InsertUsb(db, row); err != nil {
			zap.L().Warn("failed to persist usb fault to DB", zap.String("usbID", usb.ID), zap.Error(err))
		}
	}

	responseSuccess(c, gin.H{
		"sn":         req.SN,
		"writeDelay": usb.WriteDelay,
		"readDelay":  usb.ReadDelay,
		"writeFail":  usb.WriteFail,
		"readFail":   usb.ReadFail,
		"message":    "fault config updated",
	})
}
