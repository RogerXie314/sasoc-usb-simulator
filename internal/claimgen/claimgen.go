package claimgen

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Config 批量生成配置
type Config struct {
	PlatformURL   string         `json:"platformUrl"`
	Token         string         `json:"token"`
	SessionID     string         `json:"sessionId"`
	Jar           http.CookieJar `json:"-"`
	Client        *http.Client   `json:"-"`
	Total         int            `json:"total"`
	Concurrent    int            `json:"concurrent"`
	ApplicantName string         `json:"applicantName"`
	ApplicantCode string         `json:"applicantCode"`
	FactoryIds    string         `json:"factoryIds"`
	Capacity      string         `json:"capacity"`
	Format        string         `json:"format"`
	StartTime     string         `json:"startTime"`
	EndTime       string         `json:"endTime"`
	DurationHours int            `json:"durationHours"`
	Phone         string         `json:"phone"`
}

// Task 批量任务
type Task struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Total     int       `json:"total"`
	Success   int       `json:"success"`
	Failed    int       `json:"failed"`
	Rate      float64   `json:"rate"`
	Elapsed   float64   `json:"elapsed"`
	Codes     []string  `json:"-"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	mu         sync.Mutex
	cancelCh   chan struct{}
	cancelOnce sync.Once
}

var (
	activeTask        *Task
	taskMu            sync.Mutex
	globalLoginCred   *LoginResult
	globalLoginCredMu sync.RWMutex
)

func SetLoginCredential(cred *LoginResult) {
	globalLoginCredMu.Lock()
	defer globalLoginCredMu.Unlock()
	globalLoginCred = cred
}

func GetLoginCredential() *LoginResult {
	globalLoginCredMu.RLock()
	defer globalLoginCredMu.RUnlock()
	return globalLoginCred
}

func ClearLoginCredential() {
	globalLoginCredMu.Lock()
	defer globalLoginCredMu.Unlock()
	globalLoginCred = nil
}

func GetActiveTask() *Task {
	taskMu.Lock()
	defer taskMu.Unlock()
	return activeTask
}

func StartTask(cfg Config, db *sql.DB) (*Task, error) {
	taskMu.Lock()
	if activeTask != nil && activeTask.Status == "running" {
		taskMu.Unlock()
		return nil, fmt.Errorf("task already running")
	}
	if cfg.Total <= 0 {
		cfg.Total = 100000
	}
	if cfg.Concurrent <= 0 {
		cfg.Concurrent = 5
	}
	if cfg.Concurrent > 20 {
		cfg.Concurrent = 20
	}
	if cfg.PlatformURL == "" {
		cfg.PlatformURL = "https://192.168.123.24:8440"
	}
	task := &Task{
		ID:        fmt.Sprintf("claim-%d", time.Now().UnixMilli()),
		Status:    "running",
		Total:     cfg.Total,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		cancelCh:  make(chan struct{}),
	}
	activeTask = task
	taskMu.Unlock()
	go runTask(task, cfg, db)
	return task, nil
}

func CancelTask() {
	taskMu.Lock()
	defer taskMu.Unlock()
	if activeTask != nil && activeTask.Status == "running" {
		activeTask.cancelOnce.Do(func() { close(activeTask.cancelCh) })
		activeTask.Status = "cancelled"
	}
}

func applyDefaults(cfg *Config) {
	if cfg.ApplicantName == "" {
		cfg.ApplicantName = "batch"
	}
	if cfg.ApplicantCode == "" {
		cfg.ApplicantCode = "00001"
	}
	if cfg.FactoryIds == "" {
		cfg.FactoryIds = "3"
	}
	if cfg.Capacity == "" {
		cfg.Capacity = "16G"
	}
	if cfg.Format == "" {
		cfg.Format = "FAT32"
	}
	if cfg.DurationHours <= 0 {
		cfg.DurationHours = 24
	}
	if cfg.Phone == "" {
		cfg.Phone = "138"
	}
}

func buildClaimBody(idx int, cfg *Config) map[string]interface{} {
	startTime := time.Now()
	endTime := startTime.Add(time.Duration(cfg.DurationHours) * time.Hour)
	if cfg.StartTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", cfg.StartTime); err == nil {
			startTime = t
		}
	}
	if cfg.EndTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", cfg.EndTime); err == nil {
			endTime = t
		}
	}
	code := cfg.ApplicantCode
	if idx > 0 {
		code = fmt.Sprintf("%s%05d", cfg.ApplicantCode, idx)
	}
	return map[string]interface{}{
		"applicantName": cfg.ApplicantName,
		"applicantCode": code,
		"phone":         buildPhone(cfg.Phone, idx),
		"startTime":     startTime.Format("2006-01-02 15:04:05"),
		"endTime":       endTime.Format("2006-01-02 15:04:05"),
		"factoryIds":    cfg.FactoryIds,
		"capacity":      cfg.Capacity,
		"format":        cfg.Format,
	}
}

// buildPhone 生成11位手机号：
// - 前缀（1-3位）：前缀 + 8位序号
// - 完整11位：保留前3位 + 后8位用序号替换
// 始终保证11位
func buildPhone(phone string, idx int) string {
	if len(phone) == 11 {
		return phone[:3] + fmt.Sprintf("%08d", idx%100000000)
	}
	return fmt.Sprintf("%s%08d", phone, idx%100000000)
}

func runTask(task *Task, cfg Config, db *sql.DB) {
	applyDefaults(&cfg)

	client := buildClient(&cfg)
	claimURL := cfg.PlatformURL + "/USM/ieg/usbApply/add"

	var success, failed int64
	var codes []string
	var codesMu sync.Mutex
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
				if idx >= cfg.Total {
					return
				}
				body := buildClaimBody(idx, &cfg)
				jsonBody, _ := json.Marshal(body)
				req, _ := http.NewRequest("POST", claimURL, bytes.NewReader(jsonBody))
				standardHeaders(req)
				req.Header.Set("Content-Type", "application/json;charset=UTF-8")
				req.Header.Set("Referer", cfg.PlatformURL+"/USM/")
				req.Header.Set("Authorization", cfg.Token)
				req.Header.Set("Origin", cfg.PlatformURL)

				resp, err := client.Do(req)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if resp.StatusCode == 200 {
					var result struct {
						Content struct {
							ApplyCode string `json:"applyCode"`
						} `json:"content"`
					}
					if json.Unmarshal(respBody, &result) == nil && result.Content.ApplyCode != "" {
						codesMu.Lock()
						codes = append(codes, result.Content.ApplyCode)
						codesMu.Unlock()
						atomic.AddInt64(&success, 1)
					} else {
						atomic.AddInt64(&failed, 1)
					}
				} else {
					atomic.AddInt64(&failed, 1)
					if resp.StatusCode == 403 && bytes.Contains(respBody, []byte("KICKOUT")) {
						task.cancelOnce.Do(func() { close(task.cancelCh) })
						return
					}
				}
				now := time.Now()
				task.mu.Lock()
				task.Success = int(atomic.LoadInt64(&success))
				task.Failed = int(atomic.LoadInt64(&failed))
				task.Rate = float64(task.Success) / now.Sub(task.StartedAt).Seconds()
				task.UpdatedAt = now
				task.mu.Unlock()
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(startTime).Seconds()
	task.mu.Lock()
	task.Codes = codes
	task.Success = int(success)
	task.Failed = int(failed)
	task.Elapsed = elapsed
	task.Rate = float64(task.Success) / elapsed
	if task.Status == "running" {
		task.Status = "completed"
	}
	task.mu.Unlock()

	if len(codes) > 0 {
		dir, _ := os.UserHomeDir()
		if dir == "" {
			dir = "."
		}
		dir = filepath.Join(dir, "claim-codes")
		os.MkdirAll(dir, 0755)
		fname := filepath.Join(dir, fmt.Sprintf("codes-%s.txt", task.ID))
		f, err := os.Create(fname)
		if err == nil {
			for _, code := range codes {
				f.WriteString(code + "\n")
			}
			f.Close()
		}
	}
}

func buildClient(cfg *Config) *http.Client {
	if cfg.Client != nil {
		return cfg.Client
	}
	var jar http.CookieJar
	if cfg.Jar != nil {
		jar = cfg.Jar
	} else {
		jar, _ = cookiejar.New(nil)
		if cfg.SessionID != "" {
			baseURL, _ := url.Parse(cfg.PlatformURL)
			if baseURL != nil {
				jar.SetCookies(baseURL, []*http.Cookie{
					{Name: "JSESSIONID", Value: cfg.SessionID, Path: "/"},
				})
			}
		}
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 30 * time.Second,
		Jar:     jar,
	}
}