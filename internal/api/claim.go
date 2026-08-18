package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/claimgen"
	"github.com/usb-simulator/internal/hub"
)

// claimHandler 申领码生成 API
type claimHandler struct {
	hub *hub.Hub
}

func newClaimHandler(h *hub.Hub) *claimHandler {
	return &claimHandler{hub: h}
}

// startClaim POST /api/v1/claim/start
func (ch *claimHandler) startClaim(c *gin.Context) {
	var req struct {
		Token      string `json:"token" binding:"required"`
		SessionID  string `json:"sessionId" binding:"required"`
		Total      int    `json:"total"`
		Concurrent int    `json:"concurrent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	task, err := claimgen.StartTask(claimgen.Config{
		Token:      req.Token,
		SessionID:  req.SessionID,
		Total:      req.Total,
		Concurrent: req.Concurrent,
	}, ch.hub.DB())
	if err != nil {
		responseError(c, http.StatusConflict, err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"taskId":  task.ID,
		"status":  task.Status,
		"total":   task.Total,
		"message": "任务已启动",
	})
}

// statusClaim GET /api/v1/claim/status
func (ch *claimHandler) statusClaim(c *gin.Context) {
	task := claimgen.GetActiveTask()
	if task == nil {
		responseSuccess(c, gin.H{"status": "none"})
		return
	}
	responseSuccess(c, gin.H{
		"taskId":    task.ID,
		"status":    task.Status,
		"total":     task.Total,
		"success":   task.Success,
		"failed":    task.Failed,
		"rate":      task.Rate,
		"elapsed":   task.Elapsed,
		"startedAt": task.StartedAt,
		"updatedAt": task.UpdatedAt,
	})
}

// cancelClaim POST /api/v1/claim/cancel
func (ch *claimHandler) cancelClaim(c *gin.Context) {
	claimgen.CancelTask()
	responseSuccess(c, gin.H{"message": "已取消"})
}

// exportClaim GET /api/v1/claim/export
func (ch *claimHandler) exportClaim(c *gin.Context) {
	task := claimgen.GetActiveTask()
	if task == nil || len(task.Codes) == 0 {
		responseError(c, http.StatusNotFound, "无可用数据")
		return
	}

	format := c.DefaultQuery("format", "json")
	if format == "txt" {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename=apply_codes.txt")
		for _, code := range task.Codes {
			c.Writer.Write([]byte(code + "\n"))
		}
		return
	}

	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=apply_codes.json")
	c.JSON(http.StatusOK, task.Codes)
}

// countsClaim GET /api/v1/claim/counts
func (ch *claimHandler) countsClaim(c *gin.Context) {
	count, _ := strconv.Atoi(c.DefaultQuery("n", "1000"))
	if count > 10000 {
		count = 10000
	}
	task := claimgen.GetActiveTask()
	if task == nil || len(task.Codes) == 0 {
		responseSuccess(c, gin.H{"codes": []string{}, "count": 0, "total": 0})
		return
	}
	start := 0
	if s, err := strconv.Atoi(c.Query("start")); err == nil && s >= 0 && s < len(task.Codes) {
		start = s
	}
	end := start + count
	if end > len(task.Codes) {
		end = len(task.Codes)
	}
	responseSuccess(c, gin.H{
		"codes": task.Codes[start:end],
		"count": end - start,
		"total": len(task.Codes),
	})
}

// loginAndStartClaim POST /api/v1/claim/login-and-start
// 自动登录SASOC平台并启动批量申领码生成任务
func (ch *claimHandler) loginAndStartClaim(c *gin.Context) {
	var req struct {
		PlatformURL string `json:"platformUrl"` // 平台地址，如 https://192.168.123.24:8440
		Username    string `json:"username" binding:"required"`
		Password    string `json:"password" binding:"required"`
		Total       int    `json:"total"`      // 目标数量，默认 100000
		Concurrent  int    `json:"concurrent"` // 并发数，默认 10
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// 如果已有任务在运行，先检查
	if claimgen.GetActiveTask() != nil && claimgen.GetActiveTask().Status == "running" {
		responseError(c, http.StatusConflict, "已有申领码生成任务在运行中")
		return
	}

	// 自动登录
	result, err := claimgen.Login(req.PlatformURL, req.Username, req.Password)
	if err != nil {
		responseError(c, http.StatusUnauthorized, fmt.Sprintf("登录失败: %s", err.Error()))
		return
	}
	if result.Error != "" {
		responseError(c, http.StatusUnauthorized, fmt.Sprintf("登录失败: %s", result.Error))
		return
	}
	if result.Token == "" {
		responseError(c, http.StatusUnauthorized, "登录成功但未获取到Token")
		return
	}

	// 保存登录凭证供状态查询
	claimgen.SetLoginCredential(result)

	// 启动任务
	task, err := claimgen.StartTask(claimgen.Config{
		PlatformURL: req.PlatformURL,
		Token:       result.Token,
		SessionID:   result.SessionID,
		Total:       req.Total,
		Concurrent:  req.Concurrent,
	}, ch.hub.DB())
	if err != nil {
		responseError(c, http.StatusConflict, err.Error())
		return
	}

	responseSuccess(c, gin.H{
		"taskId":    task.ID,
		"status":    task.Status,
		"total":     task.Total,
		"sessionId": result.SessionID,
		"message":   "登录成功，任务已启动",
	})
}

// loginStatusClaim GET /api/v1/claim/login-status
// 查询当前登录状态
func (ch *claimHandler) loginStatusClaim(c *gin.Context) {
	cred := claimgen.GetLoginCredential()
	if cred == nil {
		responseSuccess(c, gin.H{"loggedIn": false, "message": "未登录"})
		return
	}

	responseSuccess(c, gin.H{
		"loggedIn":  true,
		"sessionId": cred.SessionID,
		"token":     cred.Token[:min(20, len(cred.Token))] + "...", // 脱敏展示
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
