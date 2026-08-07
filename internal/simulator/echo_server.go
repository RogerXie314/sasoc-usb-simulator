package simulator

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/usb-simulator/internal/protocol"
	"go.uber.org/zap"
)

// EchoServer 模拟 SASOC 服务端，用于功能验证
// 接收安检站的 TCP 连接，解析协议帧，返回应答
type EchoServer struct {
	listener net.Listener
	port     int
	running  atomic.Bool

	mu         sync.RWMutex
	connections map[string]net.Conn // key = remote addr
	deviceMap   map[uint32]string   // deviceId → station SN
	nextDevID   uint32

	logger *zap.Logger
}

// NewEchoServer 创建模拟服务端
func NewEchoServer(port int) *EchoServer {
	return &EchoServer{
		port:        port,
		connections: make(map[string]net.Conn),
		deviceMap:   make(map[uint32]string),
		nextDevID:   10001,
		logger:      zap.L(),
	}
}

// Start 启动监听
func (s *EchoServer) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("echo server listen on %s: %w", addr, err)
	}
	s.listener = ln
	s.running.Store(true)

	s.logger.Info("echo server started", zap.Int("port", s.port))

	go s.acceptLoop()
	return nil
}

// Stop 停止监听
func (s *EchoServer) Stop() {
	s.running.Store(false)
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Lock()
	for addr, conn := range s.connections {
		conn.Close()
		delete(s.connections, addr)
	}
	s.mu.Unlock()
	s.logger.Info("echo server stopped")
}

// ConnectionCount 返回当前连接数
func (s *EchoServer) ConnectionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections)
}

// acceptLoop 接受连接循环
func (s *EchoServer) acceptLoop() {
	for s.running.Load() {
		conn, err := s.listener.Accept()
		if err != nil {
			if !s.running.Load() {
				return
			}
			s.logger.Error("accept failed", zap.Error(err))
			continue
		}

		remoteAddr := conn.RemoteAddr().String()
		s.mu.Lock()
		s.connections[remoteAddr] = conn
		s.mu.Unlock()

		s.logger.Info("new connection", zap.String("remote", remoteAddr))

		go s.handleConnection(conn, remoteAddr)
	}
}

// handleConnection 处理单个连接
func (s *EchoServer) handleConnection(conn net.Conn, remoteAddr string) {
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.connections, remoteAddr)
		s.mu.Unlock()
		s.logger.Info("connection closed", zap.String("remote", remoteAddr))
	}()

	for s.running.Load() {
		// 读取包头
		headerBuf := make([]byte, protocol.HeaderSize)
		if _, err := readExact(conn, headerBuf); err != nil {
			return
		}

		header, err := protocol.DecodeHeader(headerBuf)
		if err != nil {
			s.logger.Error("decode header failed", zap.Error(err))
			continue
		}

		if err := header.Validate(); err != nil {
			s.logger.Error("header validation failed", zap.Error(err))
			continue
		}

		// 读取包体
		var body []byte
		if header.BodyLen > 0 {
			body = make([]byte, header.BodyLen)
			if _, err := readExact(conn, body); err != nil {
				return
			}
		} else {
			body = []byte{}
		}

		// 解码 JSON
		frame := &protocol.Frame{Header: header, Body: body}
		var jsonBody map[string]interface{}
		_ = frame.DecodeJSONBody(&jsonBody)

		s.logger.Info("received message",
			zap.String("remote", remoteAddr),
			zap.Uint32("cmdID", header.CmdID),
			zap.Uint32("devID", header.DevID),
			zap.Any("body", jsonBody),
		)

		// 构造应答
		s.handleCommand(conn, header, jsonBody)
	}
}

// handleCommand 处理命令并返回应答
func (s *EchoServer) handleCommand(conn net.Conn, reqHeader *protocol.Header, body map[string]interface{}) {
	var respBody map[string]interface{}
	var respCmdID uint32 = reqHeader.CmdID // 应答使用相同 CmdID

	// 从请求体中提取 CMDContent（如果有的话）
	cmdContent, _ := body["CMDContent"].(map[string]interface{})
	if cmdContent == nil {
		cmdContent = body // 兼容扁平格式
	}

	// 确保 cmdContent 不为 nil
	if cmdContent == nil {
		cmdContent = make(map[string]interface{})
	}

	switch reqHeader.CmdID {
	case protocol.CmdRegister:
		// 提取 msgId 用于透传到响应
		msgId, _ := body["msgId"].(string)
		if msgId == "" {
			msgId, _ = cmdContent["msgId"].(string)
		}
		respBody = s.handleRegister(reqHeader, cmdContent, msgId)
	case protocol.CmdHeartbeat:
		// 心跳应答为空
		respBody = nil
	case protocol.CmdInfoReport:
		innerResp := map[string]interface{}{"code": 0}
		respBody = wrapCMDContent(innerResp, body)
	case protocol.CmdClaimVerify:
		respBody = s.handleClaimVerify(cmdContent)
	case protocol.CmdUsbClaim:
		innerResp := map[string]interface{}{"code": 0}
		respBody = wrapCMDContent(innerResp, body)
	case protocol.CmdUsbReturn:
		respBody = s.handleUsbReturn(cmdContent)
	case protocol.CmdAlarm:
		// 告警为单向上报，无应答
		return
	case protocol.CmdOperationLog:
		// 操作日志为单向上报，无应答
		return
	case protocol.CmdUpgradeResult:
		// 升级结果为上报，无需应答
		s.logger.Info("upgrade result received", zap.Any("body", body))
		return
	default:
		innerResp := map[string]interface{}{"code": 1, "msg": "unknown command"}
		respBody = wrapCMDContent(innerResp, body)
	}

	// 发送应答
	// 每次加密时生成新的 RandomValue（与主机卫士 CProtocal::GetRandomKey 一致）
	opts := protocol.EncodeOptions{
		Encrypt:     reqHeader.IsEncrypted(),
		Compress:    reqHeader.IsCompressed(),
		RandomValue: 0,
		SerialNo:    reqHeader.SerialNo,
	}
	if opts.Encrypt {
		opts.RandomValue = uint16(rand.Intn(65535))
	}

	var devID uint32 = reqHeader.DevID
	if respBody == nil {
		// 空应答（如心跳）
		frameBytes, err := protocol.EncodeFrameRaw(respCmdID, devID, []byte{}, opts)
		if err != nil {
			s.logger.Error("encode empty response failed", zap.Error(err))
			return
		}
		if _, err := conn.Write(frameBytes); err != nil {
			s.logger.Error("write response failed", zap.Error(err))
		}
		return
	}

	frameBytes, err := protocol.EncodeFrame(respCmdID, devID, respBody, opts)
	if err != nil {
		s.logger.Error("encode response failed", zap.Error(err))
		return
	}
	if _, err := conn.Write(frameBytes); err != nil {
		s.logger.Error("write response failed", zap.Error(err))
	}
}

// wrapCMDContent 将内部响应包装为协议格式 {CMDID, msgId, CMDContent:{...}}
// SASOC 响应格式：{CMDID, msgId, CMDContent:{code, message, data:{...}}}
func wrapCMDContent(innerResp map[string]interface{}, reqBody map[string]interface{}) map[string]interface{} {
	// 透传请求中的 msgId（如果有的话）
	msgId, _ := reqBody["msgId"].(string)
	result := map[string]interface{}{
		"CMDContent": innerResp,
	}
	if msgId != "" {
		result["msgId"] = msgId
	}
	return result
}

// wrapRegisterResponse 注册应答专用包装
// SASOC 格式：{CMDID:102, msgId:"...", CMDContent:{code:0, message:"success", data:{deviceId:xxx}}}
func (s *EchoServer) wrapRegisterResponse(devID uint32, msgId string) map[string]interface{} {
	result := map[string]interface{}{
		"CMDContent": map[string]interface{}{
			"code":    0,
			"message": "success",
			"data": map[string]interface{}{
				"deviceId": devID,
			},
		},
	}
	if msgId != "" {
		result["msgId"] = msgId
	}
	return result
}

// handleRegister 处理注册请求
func (s *EchoServer) handleRegister(header *protocol.Header, cmdContent map[string]interface{}, msgId string) map[string]interface{} {
	sn, _ := cmdContent["sn"].(string)
	if sn == "" {
		sn, _ = cmdContent["ComputerID"].(string)
	}
	s.logger.Info("register request", zap.String("sn", sn))

	// 检查容量
	s.mu.Lock()
	if len(s.connections) > 100 {
		s.mu.Unlock()
		return wrapCMDContent(map[string]interface{}{"code": protocol.CodeOverCapacity}, nil)
	}

	// 分配 deviceId
	devID := s.nextDevID
	s.nextDevID++
	s.deviceMap[devID] = sn // 记录映射：deviceId → station SN
	s.mu.Unlock()

	// 注册应答使用 SASOC 标准格式
	// 需要从 cmdContent 或 body 中提取 msgId
	return s.wrapRegisterResponse(devID, "")
}

// handleClaimVerify 处理申领码验证
// 对齐协议 §7.4：请求字段为 applyCode，响应字段为 data.applicantName/applicantNo/applicantPhone/startTime/endTime/areaCodes
func (s *EchoServer) handleClaimVerify(cmdContent map[string]interface{}) map[string]interface{} {
	applyCode, _ := cmdContent["applyCode"].(string)

	// 模拟：非空申领码视为有效
	if applyCode == "" {
		return wrapCMDContent(map[string]interface{}{"code": protocol.CodeClaimNotExist}, nil)
	}
	if applyCode == "LOCKED" {
		return wrapCMDContent(map[string]interface{}{"code": protocol.CodeClaimNotAvailable}, nil)
	}
	if applyCode == "EXPIRED" {
		return wrapCMDContent(map[string]interface{}{"code": protocol.CodeOutOfTimeRange}, nil)
	}

	now := time.Now().UnixMilli()
	innerResp := map[string]interface{}{
		"code":    0,
		"message": "success",
		"data": map[string]interface{}{
			"applicantName":  "张三",
			"applicantNo":    "EMP001",
			"applicantPhone": "13800138000",
			"startTime":      now - 3600000,
			"endTime":        now + 86400000,
			"areaCodes":      []string{"AREA001", "AREA002"},
		},
	}
	return wrapCMDContent(innerResp, nil)
}

// handleUsbReturn 处理U盘归还
// 对齐协议 §7.6：请求字段为 sn
func (s *EchoServer) handleUsbReturn(cmdContent map[string]interface{}) map[string]interface{} {
	sn, _ := cmdContent["sn"].(string)

	// 模拟：特定 SN 触发错误
	switch sn {
	case "NOT_COLLECTED":
		return wrapCMDContent(map[string]interface{}{"code": protocol.CodeUsbNotCollected}, nil)
	case "SCRAPPED":
		return wrapCMDContent(map[string]interface{}{"code": protocol.CodeUsbScrapped}, nil)
	}

	return wrapCMDContent(map[string]interface{}{"code": 0, "message": "success"}, nil)
}

// readExact 精确读取 n 字节
func readExact(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
