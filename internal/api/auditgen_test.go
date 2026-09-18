package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/simulator"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// resetAuditGenMgr 重置全局审计生成管理器，避免测试间相互污染
func resetAuditGenMgr() {
	globalAuditGenMgr.mu.Lock()
	defer globalAuditGenMgr.mu.Unlock()
	for _, task := range globalAuditGenMgr.tasks {
		task.running.Store(false)
		if task.stopCh != nil {
			close(task.stopCh)
			task.stopCh = nil
		}
	}
	globalAuditGenMgr.tasks = make(map[string]*auditGenTask)
}

// newTestRouter 构造带 auditgen 路由的测试引擎
func newTestRouter() (*gin.Engine, *hub.Hub) {
	resetAuditGenMgr()
	h := hub.NewHub(nil, hub.NewEventBus(64), &config.Config{})
	cfg := &config.Config{}
	router := gin.New()

	handler := newAuditGenHandler(h, cfg)
	router.POST("/api/v1/auditgen/start", handler.startAuditGen)
	router.POST("/api/v1/auditgen/stop", handler.stopAuditGen)
	router.GET("/api/v1/auditgen/stats", handler.getAuditGenStats)

	return router, h
}

// addOnlineStation 添加一个处于在线状态的模拟安检站
func addOnlineStation(h *hub.Hub, id, sn string) {
	st := simulator.NewSimStation(id, sn, "X86-TEST", "v1.0", "测试站-"+id)
	st.SetState(simulator.StateOnline)
	_ = h.AddStation(st)
}

// postJSON 发起 POST JSON 请求并返回 recorder
func postJSON(t *testing.T, router *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, reader)
	r.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, r)
	return w
}

// getStats 发起 GET stats 请求并解析返回的 map
func getStats(t *testing.T, router *gin.Engine) map[string]AuditGenStats {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auditgen/stats", nil)
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("stats expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data map[string]AuditGenStats `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	return resp.Data
}

// TestStartAuditGenNoStation 无在线站点时拒绝启动
func TestStartAuditGenNoStation(t *testing.T) {
	router, _ := newTestRouter()

	w := postJSON(t, router, "/api/v1/auditgen/start", AuditGenConfig{})
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
	w := postJSON(t, router, "/api/v1/auditgen/stop", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for stop with no running task, got %d", w.Code)
	}

	// stats 返回按模式分组的空统计
	stats := getStats(t, router)
	if len(stats) != 0 {
		t.Fatalf("expected empty stats map, got %v", stats)
	}
}

// TestAuditGenDualModeConcurrent 双任务槽位并发：不同模式可同时运行、同模式冲突、按模式独立停止
func TestAuditGenDualModeConcurrent(t *testing.T) {
	router, h := newTestRouter()
	addOnlineStation(h, "st-1", "SN-TEST-001")

	// 1. 启动本地模式（真实任务，站点在线但未连接，发送计数为 0、保持运行）
	w := postJSON(t, router, "/api/v1/auditgen/start", AuditGenConfig{
		Mode:  "local",
		Total: 1000000,
		Rate:  100,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("start local expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 2. 同模式重复启动 → 409
	w = postJSON(t, router, "/api/v1/auditgen/start", AuditGenConfig{
		Mode:  "local",
		Total: 100,
		Rate:  100,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate local start expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// 3. 白盒注入一个运行中的平台模式任务（模拟已经并行跑着的另一槽位）
	globalAuditGenMgr.mu.Lock()
	plat := &auditGenTask{
		stopCh:    make(chan struct{}),
		startTime: time.Now(),
		startStr:  time.Now().Format("2006-01-02 15:04:05"),
		mode:      "platform",
	}
	plat.running.Store(true)
	plat.total.Store(1000000)
	globalAuditGenMgr.tasks["platform"] = plat
	globalAuditGenMgr.mu.Unlock()

	// 4. stats 应同时包含两个模式且都 running（双任务并发）
	stats := getStats(t, router)
	if !stats["local"].Running {
		t.Fatal("local task should be running")
	}
	if !stats["platform"].Running {
		t.Fatal("platform task should be running")
	}
	if stats["local"].Mode != "local" || stats["platform"].Mode != "platform" {
		t.Fatalf("unexpected modes: %v", stats)
	}

	// 5. 平台模式已运行时再次启动平台模式 → 409
	w = postJSON(t, router, "/api/v1/auditgen/start", AuditGenConfig{
		Mode:  "platform",
		Total: 100,
		Rate:  100,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate platform start expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// 6. 按模式停止 platform（不传 mode 也会停止全部；此处验证指定模式独立停止）
	w = postJSON(t, router, "/api/v1/auditgen/stop", map[string]string{"mode": "platform"})
	if w.Code != http.StatusOK {
		t.Fatalf("stop platform expected 200, got %d: %s", w.Code, w.Body.String())
	}
	stats = getStats(t, router)
	if stats["platform"].Running {
		t.Fatal("platform task should be stopped")
	}
	if !stats["local"].Running {
		t.Fatal("local task should keep running after stopping platform")
	}

	// 7. 再按模式停止 local
	w = postJSON(t, router, "/api/v1/auditgen/stop", map[string]string{"mode": "local"})
	if w.Code != http.StatusOK {
		t.Fatalf("stop local expected 200, got %d: %s", w.Code, w.Body.String())
	}
	stats = getStats(t, router)
	if stats["local"].Running {
		t.Fatal("local task should be stopped")
	}

	// 8. 全部停止后再 stop → 400
	w = postJSON(t, router, "/api/v1/auditgen/stop", map[string]string{"mode": "local"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("stop stopped task expected 400, got %d", w.Code)
	}
}

// TestAuditGenStopAllModes 不传 mode 时停止所有运行中任务
func TestAuditGenStopAllModes(t *testing.T) {
	router, h := newTestRouter()
	addOnlineStation(h, "st-1", "SN-TEST-001")

	// 启动 local
	w := postJSON(t, router, "/api/v1/auditgen/start", AuditGenConfig{
		Mode:  "local",
		Total: 1000000,
		Rate:  100,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("start local expected 200, got %d", w.Code)
	}

	// 注入 platform 运行中任务
	globalAuditGenMgr.mu.Lock()
	plat := &auditGenTask{
		stopCh:    make(chan struct{}),
		startTime: time.Now(),
		startStr:  time.Now().Format("2006-01-02 15:04:05"),
		mode:      "platform",
	}
	plat.running.Store(true)
	plat.total.Store(1000000)
	globalAuditGenMgr.tasks["platform"] = plat
	globalAuditGenMgr.mu.Unlock()

	// 不传 mode 停止全部
	w = postJSON(t, router, "/api/v1/auditgen/stop", map[string]string{})
	if w.Code != http.StatusOK {
		t.Fatalf("stop all expected 200, got %d: %s", w.Code, w.Body.String())
	}
	stats := getStats(t, router)
	if stats["local"].Running || stats["platform"].Running {
		t.Fatalf("all tasks should be stopped: %v", stats)
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

// TestBuildSnPool 验证 buildSnPool 生成格式
func TestBuildSnPool(t *testing.T) {
	pool := buildSnPool("USB-", 3)
	if len(pool) != 3 {
		t.Fatalf("expected 3, got %d", len(pool))
	}
	if pool[0] != "USB-000001" || pool[2] != "USB-000003" {
		t.Fatalf("unexpected pool: %v", pool)
	}
}

// TestResolveSnPool 验证 SN 池解析：优先 deviceSNs，否则回退到前缀自动生成
func TestResolveSnPool(t *testing.T) {
	// 1. deviceSNs 非空时直接返回
	cfg1 := AuditGenConfig{DeviceSNs: []string{"disk-16-0050", "disk-16-0008"}}
	pool1 := resolveSnPool(cfg1)
	if len(pool1) != 2 || pool1[0] != "disk-16-0050" || pool1[1] != "disk-16-0008" {
		t.Fatalf("expected user SN list, got %v", pool1)
	}

	// 2. deviceSNs 为空时按前缀生成
	cfg2 := AuditGenConfig{SnPrefix: "TEST-", SnPoolSize: 5}
	pool2 := resolveSnPool(cfg2)
	if len(pool2) != 5 || pool2[0] != "TEST-000001" || pool2[4] != "TEST-000005" {
		t.Fatalf("expected auto pool, got %v", pool2)
	}

	// 3. deviceSNs 为空且参数缺失时使用默认值
	cfg3 := AuditGenConfig{}
	pool3 := resolveSnPool(cfg3)
	if len(pool3) != 1000 || pool3[0] != "USB-000001" {
		t.Fatalf("expected default pool, got len=%d first=%s", len(pool3), pool3[0])
	}
}
