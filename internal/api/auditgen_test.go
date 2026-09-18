package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/hub"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestRouter 构造带 auditgen 路由的测试引擎
func newTestRouter() (*gin.Engine, *hub.Hub) {
	h := hub.NewHub(nil, hub.NewEventBus(64), &config.Config{})
	cfg := &config.Config{}
	router := gin.New()

	handler := newAuditGenHandler(h, cfg)
	router.POST("/api/v1/auditgen/start", handler.startAuditGen)
	router.POST("/api/v1/auditgen/stop", handler.stopAuditGen)
	router.GET("/api/v1/auditgen/stats", handler.getAuditGenStats)

	return router, h
}

// TestStartAuditGenNoStation 无在线站点时拒绝启动
func TestStartAuditGenNoStation(t *testing.T) {
	router, _ := newTestRouter()

	var req AuditGenConfig
	body, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auditgen/start", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no online stations") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

// TestAuditGenStatsAndStop 无运行任务时 stop 和 stats 行为
func TestAuditGenStatsAndStop(t *testing.T) {
	router, _ := newTestRouter()

	// stop 无运行任务应 400
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auditgen/stop", nil)
	router.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for stop with no running task, got %d", w.Code)
	}

	// stats 返回默认值
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/v1/auditgen/stats", nil)
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data AuditGenStats `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if resp.Data.Running {
		t.Fatal("expected not running")
	}
}

// TestIsProtectedSasocHost 生产环境黑名单
func TestIsProtectedSasocHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"192.168.60.162", true},
		{"192.168.60.162:4567", true},
		{"192.168.123.124", false},
		{"192.168.123.124:4567", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isProtectedSasocHost(c.host); got != c.want {
			t.Errorf("isProtectedSasocHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

// TestApplyCodePool 验证 buildSnPool 生成格式
func TestBuildSnPool(t *testing.T) {
	pool := buildSnPool("USB-", 3)
	if len(pool) != 3 {
		t.Fatalf("expected 3, got %d", len(pool))
	}
	if pool[0] != "USB-000001" || pool[2] != "USB-000003" {
		t.Fatalf("unexpected pool: %v", pool)
	}
}
