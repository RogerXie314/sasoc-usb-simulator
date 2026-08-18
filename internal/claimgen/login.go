package claimgen

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// LoginResult 登录结果
type LoginResult struct {
	SessionID string `json:"sessionId"`
	Token     string `json:"token"`
	Error     string `json:"error,omitempty"`
}

// Login 模拟登录SASOC平台
// 流程: GET /login/getPublicKey -> RSA加密密码 -> POST /login/userLogin -> 获取token
func Login(platformURL, username, password string) (*LoginResult, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	logger := zap.L()

	// Step 1: 获取RSA公钥
	pubKeyURL := platformURL + "/login/getPublicKey"
	resp, err := client.Get(pubKeyURL)
	if err != nil {
		return nil, fmt.Errorf("获取公钥失败: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var pubKeyResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &pubKeyResp); err != nil || !pubKeyResp.Status {
		return nil, fmt.Errorf("公钥响应异常: %s", string(body[:200]))
	}

	// 解析公钥
	pubKeyPEM := "-----BEGIN PUBLIC KEY-----\n" + pubKeyResp.Message + "\n-----END PUBLIC KEY-----"
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("解析公钥PEM失败")
	}
	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		// 尝试PKCS1格式
		pubKey, err = x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析公钥失败: %w", err)
		}
	}
	rsaKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("公钥不是RSA类型")
	}

	// Step 2: RSA加密密码
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, rsaKey, []byte(password))
	if err != nil {
		return nil, fmt.Errorf("RSA加密失败: %w", err)
	}
	encPassword := base64.StdEncoding.EncodeToString(encrypted)

	// Step 3: 登录
	loginURL := platformURL + "/login/userLogin"
	loginBody := map[string]string{
		"userName":     username,
		"userPassword": encPassword,
	}
	jsonBody, _ := json.Marshal(loginBody)

	req, _ := http.NewRequest("POST", loginURL, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	logger.Info("claimgen login", zap.Int("status", resp.StatusCode), zap.String("body", string(body[:200])))

	// 提取JSESSIONID和Token
	var sessionID string
	for _, c := range resp.Cookies() {
		if c.Name == "JSESSIONID" {
			sessionID = c.Value
		}
	}

	var loginResp struct {
		Status bool `json:"status"`
		Data   struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
		Content struct {
			AccessToken string `json:"accessToken"`
		} `json:"content"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return &LoginResult{SessionID: sessionID, Error: fmt.Sprintf("登录响应解析失败: %s", string(body[:200]))}, nil
	}

	// 提取token
	token := loginResp.Data.AccessToken
	if token == "" {
		token = loginResp.Content.AccessToken
	}

	if !loginResp.Status && token == "" {
		return &LoginResult{SessionID: sessionID, Error: loginResp.Message}, nil
	}

	return &LoginResult{SessionID: sessionID, Token: token}, nil
}

// extractToken 从HTML中提取accessToken
func extractToken(html string) string {
	patterns := []string{`"accessToken":"`, `accessToken":"`, `token":"`, `"token":"`}
	for _, p := range patterns {
		idx := strings.Index(html, p)
		if idx >= 0 {
			start := idx + len(p)
			end := strings.IndexAny(html[start:], `"`)
			if end > 0 {
				return html[start : start+end]
			}
		}
	}
	return ""
}

// LoginAndGenerate 登录并批量生成申领码
func LoginAndGenerate(platformURL, username, password string, total, concurrent int) (*Task, error) {
	result, err := Login(platformURL, username, password)
	if err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf(result.Error)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("登录成功但未获取到Token，请手动提供")
	}

	return StartTask(Config{
		PlatformURL: platformURL,
		Token:       result.Token,
		SessionID:   result.SessionID,
		Total:       total,
		Concurrent:  concurrent,
	}, nil)
}

// 提取 JSON 响应中的申领码
func extractApplyCode(data []byte) string {
	var result struct {
		Content struct {
			ApplyCode string `json:"applyCode"`
		} `json:"content"`
	}
	if json.Unmarshal(data, &result) == nil {
		return result.Content.ApplyCode
	}
	return ""
}

// 检查响应是否表示被踢出
func isKickedOut(statusCode int, body []byte) bool {
	return statusCode == 403 && bytes.Contains(body, []byte("KICKOUT"))
}
