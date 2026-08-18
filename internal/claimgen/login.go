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
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// standardHeaders 设置标准 Chrome 浏览器请求头，确保登录和业务API的浏览器指纹完全一致
func standardHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
}

// LoginResult 登录结果
type LoginResult struct {
	SessionID   string         `json:"sessionId"`
	Token       string         `json:"token"`
	PlatformURL string         `json:"platformUrl,omitempty"`
	Error       string         `json:"error,omitempty"`
	Jar         http.CookieJar `json:"-"` // 保留登录时的完整 CookieJar，避免 SessionID 提取丢失
	Client      *http.Client   `json:"-"` // 保留登录时的 HTTP Client（复用 TCP 连接池，避免后端因连接变化触发 KICKOUT）
}

// Login 模拟登录 SASOC Web 平台
// 参考 AutoTest(EDR) 测试项目实现：Session + CookieJar + RSA PKCS1_v1_5 + JSON
// 流程:
//  1. 创建带 CookieJar 的 HTTP Client（保持会话Cookie）
//  2. GET /login/getPublicKey 获取公钥
//  3. RSA EncryptPKCS1v15 加密用户名和密码
//  4. POST /login/userLogin JSON {userName, userPassword}
//  5. 解析 JSON 获取 accessToken + JSESSIONID
func Login(platformURL, username, password string) (*LoginResult, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("创建 CookieJar 失败: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	logger := zap.L()
	baseURL := strings.TrimRight(platformURL, "/")

	// Step 0: 先访问平台首页获取初始 JSESSIONID Session Cookie
	homeURL := baseURL + "/USM/"
	homeReq, _ := http.NewRequest("GET", homeURL, nil)
	standardHeaders(homeReq)
	homeResp, err := client.Do(homeReq)
	if err == nil && homeResp != nil {
		homeResp.Body.Close()
		logger.Info("claimgen login visited home",
			zap.Int("status", homeResp.StatusCode),
			zap.String("cookies", fmtCookies(jar.Cookies(homeReq.URL))),
		)
	}

	// Step 1: 获取 RSA 公钥（CookieJar 会自动保存 JSESSIONID）
	pubKeyURL := baseURL + "/login/getPublicKey"
	req, err := http.NewRequest("GET", pubKeyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建公钥请求失败: %w", err)
	}
	standardHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取公钥请求失败: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// 从公钥响应中提取 JSESSIONID
	var sessionID string
	for _, c := range resp.Cookies() {
		if c.Name == "JSESSIONID" {
			sessionID = c.Value
			break
		}
	}
	if sessionID == "" {
		for _, c := range jar.Cookies(req.URL) {
			if c.Name == "JSESSIONID" {
				sessionID = c.Value
				break
			}
		}
	}

	logger.Info("claimgen login getPublicKey",
		zap.Int("status", resp.StatusCode),
		zap.String("sessionId", sessionID),
		zap.String("cookies", fmtCookies(jar.Cookies(req.URL))),
		zap.String("body", string(body[:min(500, len(body))])),
	)

	var pubKeyResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &pubKeyResp); err != nil {
		return nil, fmt.Errorf("公钥响应解析失败: %w | body=%s", err, string(body[:min(300, len(body))]))
	}
	if !pubKeyResp.Status {
		return nil, fmt.Errorf("获取公钥失败: %s", pubKeyResp.Message)
	}

	pubKeyPEM := "-----BEGIN PUBLIC KEY-----\n" + pubKeyResp.Message + "\n-----END PUBLIC KEY-----"

	// Step 2: 解析 RSA 公钥
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("解析公钥 PEM 失败")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		pub, err = x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析公钥失败: %w", err)
		}
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("公钥不是 RSA 类型")
	}

	// Step 3: RSA 加密用户名和密码（等价于 pycryptodome PKCS1_v1_5）
	// 注意：用户名和密码都需要 RSA 加密，测试项目 conftest.py 第264行同时加密了两个字段
	encUser, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(username))
	if err != nil {
		return nil, fmt.Errorf("用户名 RSA 加密失败: %w", err)
	}
	encPwd, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(password))
	if err != nil {
		return nil, fmt.Errorf("密码 RSA 加密失败: %w", err)
	}
	encUserB64 := base64.StdEncoding.EncodeToString(encUser)
	encPwdB64 := base64.StdEncoding.EncodeToString(encPwd)

	// Step 4: 发送登录请求（JSON 格式，与测试项目 conftest.py 一致）
	loginURL := baseURL + "/login/userLogin"
	loginBody := map[string]string{
		"userName":     encUserB64,
		"userPassword": encPwdB64,
	}
	jsonBody, _ := json.Marshal(loginBody)

	req, err = http.NewRequest("POST", loginURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建登录请求失败: %w", err)
	}
	standardHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ = io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(body))
	bodyStr = strings.TrimPrefix(bodyStr, "\uFEFF")

	logger.Info("claimgen login response",
		zap.Int("status", resp.StatusCode),
		zap.Int("body_len", len(bodyStr)),
		zap.String("body_first_300", bodyStr[:min(300, len(bodyStr))]),
		zap.String("resp_set_cookie", fmtCookies(resp.Cookies())),
		zap.String("jar_cookies_login_url", fmtCookies(jar.Cookies(req.URL))),
	)

	// Step 5: 提取/更新 JSESSIONID
	// 优先从登录响应的 Set-Cookie 提取
	for _, c := range resp.Cookies() {
		if c.Name == "JSESSIONID" {
			sessionID = c.Value
			break
		}
	}
	// 其次从 CookieJar 的 login URL 提取
	if sessionID == "" {
		for _, c := range jar.Cookies(req.URL) {
			if c.Name == "JSESSIONID" {
				sessionID = c.Value
				break
			}
		}
	}
	// 最后从 CookieJar 的 home URL 提取（cookie 可能限定在 /USM/ 路径）
	if sessionID == "" {
		homeURL, _ := url.Parse(homeURL)
		if homeURL != nil {
			for _, c := range jar.Cookies(homeURL) {
				if c.Name == "JSESSIONID" {
					sessionID = c.Value
					break
				}
			}
		}
	}
	logger.Info("claimgen login session extracted",
		zap.String("sessionId", sessionID),
		zap.String("jar_cookies_home", fmtCookies(jar.Cookies(homeReq.URL))),
	)

	// Step 6: 解析响应 JSON
	var loginResp struct {
		Status  bool            `json:"status"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &loginResp); err != nil {
		logger.Warn("claimgen login JSON parse failed",
			zap.Error(err),
			zap.String("body", bodyStr[:min(500, len(bodyStr))]),
		)
		token := extractTokenFromHTML(bodyStr)
		if token != "" {
			return &LoginResult{SessionID: sessionID, Token: token, PlatformURL: baseURL, Jar: jar}, nil
		}
		return &LoginResult{SessionID: sessionID, PlatformURL: baseURL, Error: fmt.Sprintf("登录响应解析失败: %s", bodyStr[:min(500, len(bodyStr))]), Jar: jar}, nil
	}

	if !loginResp.Status {
		errMsg := loginResp.Message
		if errMsg == "" {
			errMsg = "登录失败（未知原因）"
		}
		return &LoginResult{SessionID: sessionID, PlatformURL: baseURL, Error: errMsg, Jar: jar}, nil
	}

	token := extractTokenFromRaw(loginResp.Data)
	if token == "" {
		token = extractTokenFromRaw(loginResp.Content)
	}

	if token == "" {
		return &LoginResult{SessionID: sessionID, PlatformURL: baseURL, Error: "登录成功但未获取到 accessToken", Jar: jar}, nil
	}

	return &LoginResult{SessionID: sessionID, Token: token, PlatformURL: baseURL, Jar: jar, Client: client}, nil
}

// fmtCookies 格式化 cookie 列表用于日志
func fmtCookies(cookies []*http.Cookie) string {
	var parts []string
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// extractTokenFromRaw 从 json.RawMessage 中提取 accessToken
func extractTokenFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == `""` {
		return ""
	}
	var obj struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return obj.AccessToken
}

// extractTokenFromHTML 从 HTML 中尝试提取 token（兜底）
func extractTokenFromHTML(html string) string {
	idx := strings.Index(html, `"accessToken":`)
	if idx == -1 {
		return ""
	}
	rest := html[idx+len(`"accessToken":`):]
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, `"`)
	end := strings.Index(rest, `"`)
	if end == -1 {
		return ""
	}
	return rest[:end]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
