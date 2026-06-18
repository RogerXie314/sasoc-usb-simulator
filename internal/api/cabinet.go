package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/hub"
)

// cabinetHandler 管控柜 API 处理器
type cabinetHandler struct {
	hub *hub.Hub
}

// newCabinetHandler 创建管控柜处理器
func newCabinetHandler(h *hub.Hub) *cabinetHandler {
	return &cabinetHandler{hub: h}
}

// --- 故障注入 ---

// injectSlotFault POST /api/v1/cabinet/:stationId/fault
// 对指定站点的单个插槽注入故障
func (ch *cabinetHandler) injectSlotFault(c *gin.Context) {
	stationID := c.Param("stationId")
	var req struct {
		DoorNo int    `json:"doorNo" binding:"required,min=1"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if err := ch.hub.InjectCabinetSlotFault(stationID, req.DoorNo, true, req.Reason); err != nil {
		responseError(c, http.StatusBadRequest, err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"stationId": stationID,
		"doorNo":    req.DoorNo,
		"status":    4,
		"message":   "fault injected",
	})
}

// injectBatchFault POST /api/v1/cabinet/:stationId/fault-batch
// 批量注入故障（支持整柜注入或指定 doorNo 列表）
func (ch *cabinetHandler) injectBatchFault(c *gin.Context) {
	stationID := c.Param("stationId")
	var req struct {
		DoorNos []int  `json:"doorNos"`         // 为空表示整柜注入
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// 若未指定 doorNos，则注入整柜
	if len(req.DoorNos) == 0 {
		if err := ch.hub.InjectCabinetAllSlotsFault(stationID, true, req.Reason); err != nil {
			responseError(c, http.StatusBadRequest, err.Error())
			return
		}
		responseSuccess(c, gin.H{
			"stationId": stationID,
			"scope":     "all",
			"message":   "all slots fault injected",
		})
		return
	}

	// 批量单槽注入
	results := make([]gin.H, 0, len(req.DoorNos))
	for _, doorNo := range req.DoorNos {
		if err := ch.hub.InjectCabinetSlotFault(stationID, doorNo, true, req.Reason); err != nil {
			results = append(results, gin.H{"doorNo": doorNo, "success": false, "error": err.Error()})
		} else {
			results = append(results, gin.H{"doorNo": doorNo, "success": true, "status": 4})
		}
	}
	responseSuccess(c, gin.H{"stationId": stationID, "results": results})
}

// restoreSlot POST /api/v1/cabinet/:stationId/restore
// 恢复单个插槽或整柜（不传 doorNo 表示整柜恢复）
func (ch *cabinetHandler) restoreSlot(c *gin.Context) {
	stationID := c.Param("stationId")
	var req struct {
		DoorNo int `json:"doorNo"` // 0 或不传表示整柜恢复
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// 允许空 body，默认整柜恢复
		req.DoorNo = 0
	}

	if req.DoorNo == 0 {
		if err := ch.hub.InjectCabinetAllSlotsFault(stationID, false, ""); err != nil {
			responseError(c, http.StatusBadRequest, err.Error())
			return
		}
		responseSuccess(c, gin.H{
			"stationId": stationID,
			"scope":     "all",
			"status":    1,
			"message":   "all slots restored",
		})
		return
	}

	if err := ch.hub.InjectCabinetSlotFault(stationID, req.DoorNo, false, ""); err != nil {
		responseError(c, http.StatusBadRequest, err.Error())
		return
	}
	responseSuccess(c, gin.H{
		"stationId": stationID,
		"doorNo":    req.DoorNo,
		"status":    1,
		"message":   "slot restored",
	})
}

// listSlots GET /api/v1/cabinet/:stationId/slots
// 查询站点管控柜全部插槽状态
func (ch *cabinetHandler) listSlots(c *gin.Context) {
	stationID := c.Param("stationId")
	station, exists := ch.hub.GetStation(stationID)
	if !exists {
		responseError(c, http.StatusNotFound, "station not found")
		return
	}

	cabinet := station.GetCabinet()
	if cabinet == nil {
		responseError(c, http.StatusNotFound, "cabinet not found for station")
		return
	}

	// 构造响应，包含 DoorStatus
	type slotInfo struct {
		DoorNo      int    `json:"doorNo"`
		Status      int    `json:"status"`
		Occupied    bool   `json:"occupied"`
		SN          string `json:"sn,omitempty"`
		Fault       bool   `json:"fault"`
		FaultReason string `json:"faultReason,omitempty"`
	}

	slots := make([]slotInfo, 0, cabinet.TotalPorts)
	for i := 0; i < cabinet.TotalPorts; i++ {
		s := cabinet.Slots[i]
		slots = append(slots, slotInfo{
			DoorNo:      s.DoorNo,
			Status:      cabinet.DoorStatus[i],
			Occupied:    s.Occupied,
			SN:          s.SN,
			Fault:       s.Fault,
			FaultReason: s.FaultReason,
		})
	}

	responseSuccess(c, gin.H{
		"stationId":  stationID,
		"totalPorts": cabinet.TotalPorts,
		"usedPorts":  cabinet.UsedPorts,
		"slots":      slots,
	})
}
