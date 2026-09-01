package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/claimgen"
	"github.com/usb-simulator/internal/hub"
)

var (
	factoryIdsRe = regexp.MustCompile(`^\d+(,\d+)*$`)
	fullPhoneRe  = regexp.MustCompile(`^1\d{10}$`)
	phonePreRe   = regexp.MustCompile(`^\d{1,3}$`)
	workerNoRe   = regexp.MustCompile(`^[A-Za-z0-9]+$`)
)

type claimHandler struct {
	hub *hub.Hub
}

func newClaimHandler(h *hub.Hub) *claimHandler {
	return &claimHandler{hub: h}
}

// startClaim POST /api/v1/claim/start
func (ch *claimHandler) startClaim(c *gin.Context) {
	var req struct {
		Token         string `json:"token" binding:"required"`
		SessionID     string `json:"sessionId" binding:"required"`
		Total         int    `json:"total"`
		Concurrent    int    `json:"concurrent"`
		ApplicantName string `json:"applicantName"`
		ApplicantCode string `json:"applicantCode"`
		FactoryIds    string `json:"factoryIds"`
		Capacity      string `json:"capacity"`
		Format        string `json:"format"`
		StartTime     string `json:"startTime"`
		EndTime       string `json:"endTime"`
		DurationHours int    `json:"durationHours"`
		Phone         string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := validateClaimParams(req.FactoryIds, req.Phone, req.ApplicantCode); err != nil {
		responseError(c, http.StatusBadRequest, err.Error())
		return
	}
	// 从登录凭证获取平台地址，避免申领请求发向默认地址
	cred := claimgen.GetLoginCredential()
	platformURL := ""
	if cred != nil {
		platformURL = cred.PlatformURL
	}
	task, err := claimgen.StartTask(claimgen.Config{
		Token: req.Token, SessionID: req.SessionID, PlatformURL: platformURL,
		Total: req.Total, Concurrent: req.Concurrent,
		ApplicantName: req.ApplicantName, ApplicantCode: req.ApplicantCode,
		FactoryIds: req.FactoryIds, Capacity: req.Capacity, Format: req.Format,
		StartTime: req.StartTime, EndTime: req.EndTime, DurationHours: req.DurationHours, Phone: req.Phone,
	}, ch.hub.DB())
	if err != nil {
		responseError(c, http.StatusConflict, err.Error())
		return
	}
	responseSuccess(c, gin.H{"taskId": task.ID, "status": task.Status, "total": task.Total, "message": "OK"})
}

// statusClaim GET /api/v1/claim/status
func (ch *claimHandler) statusClaim(c *gin.Context) {
	task := claimgen.GetActiveTask()
	if task == nil {
		responseSuccess(c, gin.H{"status": "none"})
		return
	}
	responseSuccess(c, gin.H{
		"taskId": task.ID, "status": task.Status, "total": task.Total,
		"success": task.Success, "failed": task.Failed, "rate": task.Rate, "elapsed": task.Elapsed,
		"startedAt": task.StartedAt, "updatedAt": task.UpdatedAt,
	})
}

// cancelClaim POST /api/v1/claim/cancel
func (ch *claimHandler) cancelClaim(c *gin.Context) {
	claimgen.CancelTask()
	responseSuccess(c, gin.H{"message": "OK"})
}

// exportClaim GET /api/v1/claim/export
func (ch *claimHandler) exportClaim(c *gin.Context) {
	task := claimgen.GetActiveTask()
	if task == nil || len(task.Codes) == 0 {
		responseError(c, http.StatusNotFound, "no data")
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
	responseSuccess(c, gin.H{"codes": task.Codes[start:end], "count": end - start, "total": len(task.Codes)})
}

// loginClaim POST /api/v1/claim/login
func (ch *claimHandler) loginClaim(c *gin.Context) {
	var req struct {
		PlatformURL string `json:"platformUrl"`
		Username    string `json:"username" binding:"required"`
		Password    string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	result, err := claimgen.Login(req.PlatformURL, req.Username, req.Password)
	if err != nil {
		responseError(c, http.StatusUnauthorized, fmt.Sprintf("login failed: %s", err.Error()))
		return
	}
	if result.Error != "" {
		responseError(c, http.StatusUnauthorized, fmt.Sprintf("login failed: %s", result.Error))
		return
	}
	if result.Token == "" {
		responseError(c, http.StatusUnauthorized, "no token")
		return
	}
	claimgen.SetLoginCredential(result)
	responseSuccess(c, gin.H{"loggedIn": true, "token": result.Token, "sessionId": result.SessionID, "message": "OK"})
}

// startLifecycle POST /api/v1/claim/lifecycle/start
func (ch *claimHandler) startLifecycle(c *gin.Context) {
	var req struct {
		Token         string   `json:"token" binding:"required"`
		SessionID     string   `json:"sessionId" binding:"required"`
		Concurrent    int      `json:"concurrent"`
		ClaimedPct    int      `json:"claimedPct"`
		BorrowedPct   int      `json:"borrowedPct"`
		ReturnedPct   int      `json:"returnedPct"`
		ExpiredPct    int      `json:"expiredPct"`
		Codes         []string `json:"codes" binding:"required"`
		StationSN     string   `json:"stationSn"`
		ApplicantName string   `json:"applicantName"`
		ApplicantCode string   `json:"applicantCode"`
		FactoryIds    string   `json:"factoryIds"`
		Capacity      string   `json:"capacity"`
		Format        string   `json:"format"`
		StartTime     string   `json:"startTime"`
		EndTime       string   `json:"endTime"`
		DurationHours int      `json:"durationHours"`
		Phone         string   `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responseError(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	task, err := claimgen.StartLifecycle(claimgen.LifecycleConfig{
		Token: req.Token, SessionID: req.SessionID, Concurrent: req.Concurrent,
		ClaimedPct: req.ClaimedPct, BorrowedPct: req.BorrowedPct,
		ReturnedPct: req.ReturnedPct, ExpiredPct: req.ExpiredPct,
		Codes: req.Codes, StationSN: req.StationSN,
	})
	if err != nil {
		responseError(c, http.StatusConflict, err.Error())
		return
	}
	responseSuccess(c, gin.H{"taskId": task.ID, "status": task.Status, "total": task.Total, "message": "OK"})
}

// statusLifecycle GET /api/v1/claim/lifecycle/status
func (ch *claimHandler) statusLifecycle(c *gin.Context) {
	task := claimgen.GetLifecycleTask()
	if task == nil {
		responseSuccess(c, gin.H{"status": "none"})
		return
	}
	responseSuccess(c, gin.H{
		"taskId": task.ID, "status": task.Status, "total": task.Total,
		"claimed": task.Claimed, "borrowed": task.Borrowed, "returned": task.Returned,
		"expired": task.Expired, "failed": task.Failed, "elapsed": task.Elapsed,
		"startedAt": task.StartedAt, "updatedAt": task.UpdatedAt,
	})
}

// cancelLifecycle POST /api/v1/claim/lifecycle/cancel
func (ch *claimHandler) cancelLifecycle(c *gin.Context) {
	claimgen.CancelLifecycle()
	responseSuccess(c, gin.H{"message": "OK"})
}

// validateClaimParams 发送前校验，避免无效输入导致平台逐条拒绝
func validateClaimParams(factoryIds, phone, workerNo string) error {
	// 归一化：中文逗号 → 英文逗号，去空格
	fids := regexp.MustCompile(`\s+`).ReplaceAllString(factoryIds, "")
	fids = regexp.MustCompile(`，`).ReplaceAllString(fids, ",")

	if fids != "" && !factoryIdsRe.MatchString(fids) {
		return fmt.Errorf("区域必须填写数字区域ID（逗号分隔，如 3,4,5），不能填写区域名称；请在平台“U盘申领-区域”中查看对应ID")
	}

	if phone != "" {
		if !fullPhoneRe.MatchString(phone) && !phonePreRe.MatchString(phone) {
			return fmt.Errorf("手机号必须为11位完整号码（1开头）或1-3位前缀（如 138），当前输入：%s", phone)
		}
	}

	if workerNo != "" && !workerNoRe.MatchString(workerNo) {
		return fmt.Errorf("工号只能包含字母和数字，当前输入：%s", workerNo)
	}
	return nil
}
