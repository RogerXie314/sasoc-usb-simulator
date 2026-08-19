package claimgen

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"sync/atomic"
	"time"
)

// LifecycleConfig 申领码生命周期配置
type LifecycleConfig struct {
	PlatformURL  string         `json:"platformUrl"`
	Token        string         `json:"token"`
	SessionID    string         `json:"sessionId"`
	Client       *http.Client   `json:"-"`
	Jar          http.CookieJar `json:"-"`
	Concurrent   int            `json:"concurrent"`
	// 各状态比例（0-100）
	ClaimedPct   int `json:"claimedPct"`   // 已申领(默认)
	BorrowedPct  int `json:"borrowedPct"`  // 已领取
	ReturnedPct  int `json:"returnedPct"`  // 已归还
	ExpiredPct   int `json:"expiredPct"`   // 已超时
	// 输入：申领码列表
	Codes []string `json:"codes"`
	// 绑定到哪个安检站
	StationSN string `json:"stationSn"`
}

// LifecycleTask 生命周期任务
type LifecycleTask struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Total     int       `json:"total"`
	Claimed   int       `json:"claimed"`
	Borrowed  int       `json:"borrowed"`
	Returned  int       `json:"returned"`
	Expired   int       `json:"expired"`
	Failed    int       `json:"failed"`
	Rate      float64   `json:"rate"`
	Elapsed   float64   `json:"elapsed"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	mu         sync.Mutex
	cancelCh   chan struct{}
	cancelOnce sync.Once
}

var (
	lifecycleTask   *LifecycleTask
	lifecycleTaskMu sync.Mutex
)

func GetLifecycleTask() *LifecycleTask {
	lifecycleTaskMu.Lock()
	defer lifecycleTaskMu.Unlock()
	return lifecycleTask
}

func StartLifecycle(cfg LifecycleConfig) (*LifecycleTask, error) {
	lifecycleTaskMu.Lock()
	if lifecycleTask != nil && lifecycleTask.Status == "running" {
		lifecycleTaskMu.Unlock()
		return nil, fmt.Errorf("lifecycle task already running")
	}
	if len(cfg.Codes) == 0 {
		lifecycleTaskMu.Unlock()
		return nil, fmt.Errorf("no codes provided")
	}
	if cfg.Concurrent <= 0 {
		cfg.Concurrent = 5
	}
	if cfg.PlatformURL == "" {
		cfg.PlatformURL = "https://192.168.123.24:8440"
	}
	// normalize percentages
	total := cfg.ClaimedPct + cfg.BorrowedPct + cfg.ReturnedPct + cfg.ExpiredPct
	if total == 0 {
		cfg.ClaimedPct = 100
	} else if total != 100 {
		cfg.ClaimedPct = cfg.ClaimedPct * 100 / total
		cfg.BorrowedPct = cfg.BorrowedPct * 100 / total
		cfg.ReturnedPct = cfg.ReturnedPct * 100 / total
		cfg.ExpiredPct = cfg.ExpiredPct * 100 / total
	}

	task := &LifecycleTask{
		ID:        fmt.Sprintf("lifecycle-%d", time.Now().UnixMilli()),
		Status:    "running",
		Total:     len(cfg.Codes),
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		cancelCh:  make(chan struct{}),
	}
	lifecycleTask = task
	lifecycleTaskMu.Unlock()

	go runLifecycle(task, cfg)
	return task, nil
}

func CancelLifecycle() {
	lifecycleTaskMu.Lock()
	defer lifecycleTaskMu.Unlock()
	if lifecycleTask != nil && lifecycleTask.Status == "running" {
		lifecycleTask.cancelOnce.Do(func() { close(lifecycleTask.cancelCh) })
		lifecycleTask.Status = "cancelled"
	}
}

func runLifecycle(task *LifecycleTask, cfg LifecycleConfig) {
	client := buildLifecycleClient(&cfg)
	n := len(cfg.Codes)

	// 计算各状态数量
	borrowedEnd := n * cfg.BorrowedPct / 100
	returnedEnd := borrowedEnd + n*cfg.ReturnedPct/100

	var claimed, borrowed, returned, expired, failed int64
	startTime := time.Now()
	var wg sync.WaitGroup
	counter := int64(0)

	for i := 0; i < cfg.Concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-task.cancelCh:
					return
				default:
				}
				idx := int(atomic.AddInt64(&counter, 1) - 1)
				if idx >= n {
					return
				}
				code := cfg.Codes[idx]

				// 已申领：所有码默认都是已申领
				atomic.AddInt64(&claimed, 1)

				// 已超时：通过 ExpiredPct 模拟，实际上超时由平台根据 endTime 判断
				// 这里不做额外操作，申领码生成时已设置时间
				if idx < n*cfg.ExpiredPct/100 {
					atomic.AddInt64(&expired, 1)
				}

				// 已领取：CMD103 验证 + CMD104 领取
				if idx < borrowedEnd {
					if verifyAndClaim(client, &cfg, code) {
						atomic.AddInt64(&borrowed, 1)
						// 已归还：CMD105 归还
						if idx < returnedEnd {
							if returnUSB(client, &cfg, code) {
								atomic.AddInt64(&returned, 1)
							} else {
								atomic.AddInt64(&failed, 1)
							}
						}
					} else {
						atomic.AddInt64(&failed, 1)
					}
				}

				// 更新进度
				now := time.Now()
				task.mu.Lock()
				task.Claimed = int(atomic.LoadInt64(&claimed))
				task.Borrowed = int(atomic.LoadInt64(&borrowed))
				task.Returned = int(atomic.LoadInt64(&returned))
				task.Expired = int(atomic.LoadInt64(&expired))
				task.Failed = int(atomic.LoadInt64(&failed))
				task.UpdatedAt = now
				task.mu.Unlock()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(startTime).Seconds()
	task.mu.Lock()
	task.Claimed = int(claimed)
	task.Borrowed = int(borrowed)
	task.Returned = int(returned)
	task.Expired = int(expired)
	task.Failed = int(failed)
	task.Elapsed = elapsed
	if task.Status == "running" {
		task.Status = "completed"
	}
	task.mu.Unlock()
}

// verifyAndClaim CMD103 验证 + CMD104 领取
func verifyAndClaim(client *http.Client, cfg *LifecycleConfig, code string) bool {
	apiURL := cfg.PlatformURL + "/USM/ieg/usbApply/add"
	_ = apiURL // 备用

	// CMD103: 申领码验证
	verifyBody := map[string]string{"applyCode": code}
	ok := sendClaimAPI(client, cfg, "/USM/ieg/usbApply/verify", verifyBody)
	if !ok {
		return false
	}

	// CMD104: 领取上报
	claimBody := map[string]string{
		"applyCode": code,
		"sn":        cfg.StationSN,
		"result":    "success",
	}
	return sendClaimAPI(client, cfg, "/USM/ieg/usbApply/claim", claimBody)
}

// returnUSB CMD105 归还
func returnUSB(client *http.Client, cfg *LifecycleConfig, code string) bool {
	body := map[string]string{"sn": cfg.StationSN}
	return sendClaimAPI(client, cfg, "/USM/ieg/usbApply/return", body)
}

func sendClaimAPI(client *http.Client, cfg *LifecycleConfig, path string, body map[string]string) bool {
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", cfg.PlatformURL+path, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", cfg.Token)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", cfg.PlatformURL+"/USM/")
	req.Header.Set("Origin", cfg.PlatformURL)

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode == 200
}

func buildLifecycleClient(cfg *LifecycleConfig) *http.Client {
	if cfg.Client != nil {
		return cfg.Client
	}
	jar := cfg.Jar
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 30 * time.Second,
		Jar:     jar,
	}
}