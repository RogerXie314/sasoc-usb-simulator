package claimgen

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Config 批量申领码生成配置
type Config struct {
	PlatformURL string `json:"platformUrl"` // 平台地址，如 https://192.168.123.24:8440
	Token       string `json:"token"`       // Authorization Token
	SessionID   string `json:"sessionId"`   // JSESSIONID
	Total       int    `json:"total"`       // 目标数量
	Concurrent  int    `json:"concurrent"`  // 并发数，默认 5
}

// Task 批量任务状态
type Task struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // running / completed / failed
	Total     int       `json:"total"`
	Success   int       `json:"success"`
	Failed    int       `json:"failed"`
	Rate      float64   `json:"rate"` // 条/秒
	Elapsed   float64   `json:"elapsed"`
	Codes     []string  `json:"-"` // 生成的申领码
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	mu       sync.Mutex
	cancelCh chan struct{}
}

var (
	activeTask *Task
	taskMu     sync.Mutex

	// globalLoginCred 当前登录凭证（用于登录状态查询）
	globalLoginCred   *LoginResult
	globalLoginCredMu sync.RWMutex
)

// SetLoginCredential 存储登录凭证
func SetLoginCredential(cred *LoginResult) {
	globalLoginCredMu.Lock()
	defer globalLoginCredMu.Unlock()
	globalLoginCred = cred
}

// GetLoginCredential 获取当前登录凭证
func GetLoginCredential() *LoginResult {
	globalLoginCredMu.RLock()
	defer globalLoginCredMu.RUnlock()
	return globalLoginCred
}

// ClearLoginCredential 清除登录凭证
func ClearLoginCredential() {
	globalLoginCredMu.Lock()
	defer globalLoginCredMu.Unlock()
	globalLoginCred = nil
}

// GetActiveTask 获取当前活跃任务
func GetActiveTask() *Task {
	taskMu.Lock()
	defer taskMu.Unlock()
	return activeTask
}

// StartTask 启动批量生成任务
func StartTask(cfg Config, db *sql.DB) (*Task, error) {
	taskMu.Lock()
	if activeTask != nil && activeTask.Status == "running" {
		taskMu.Unlock()
		return nil, fmt.Errorf("已有任务在运行中")
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

// CancelTask 取消当前任务
func CancelTask() {
	taskMu.Lock()
	defer taskMu.Unlock()
	if activeTask != nil && activeTask.Status == "running" {
		close(activeTask.cancelCh)
		activeTask.Status = "cancelled"
	}
}

func runTask(task *Task, cfg Config, db *sql.DB) {
	logger := zap.L()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 30 * time.Second,
	}

	url := cfg.PlatformURL + "/USM/ieg/usbApply/add"
	var success, failed int64
	var codes []string
	var codesMu sync.Mutex
	startTime := time.Now()

	// 并发 worker
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

				body := map[string]interface{}{
					"applicantName": "batch",
					"applicantCode": fmt.Sprintf("%05d", idx+1),
					"phone":         fmt.Sprintf("138%08d", idx%100000000),
					"startTime":     time.Now().Format("2006-01-02 10:00:00"),
					"endTime":       time.Now().Add(24 * time.Hour).Format("2006-01-02 10:00:00"),
					"factoryIds":    "3",
					"capacity":      "16G",
					"format":        "FAT32",
				}
				jsonBody, _ := json.Marshal(body)

				req, _ := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json;charset=UTF-8")
				req.Header.Set("Authorization", cfg.Token)
				req.Header.Set("User-Agent", "Mozilla/5.0")
				req.Header.Set("Referer", cfg.PlatformURL+"/")
				req.AddCookie(&http.Cookie{Name: "JSESSIONID", Value: cfg.SessionID})

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
					// 被踢出时停止
					if resp.StatusCode == 403 && bytes.Contains(respBody, []byte("KICKOUT")) {
						logger.Warn("claim gen: session kicked out, stopping")
						close(task.cancelCh)
						return
					}
				}

				// 更新进度
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
	task.UpdatedAt = time.Now()
	task.mu.Unlock()

	// 保存到文件
	if len(codes) > 0 {
		data, _ := json.MarshalIndent(codes, "", "  ")
		os.WriteFile(filepath.Join(os.TempDir(), task.ID+".json"), data, 0644)
	}

	logger.Info("claim gen completed",
		zap.Int("success", task.Success),
		zap.Int("failed", task.Failed),
		zap.Float64("elapsed", elapsed),
	)
}
