package simulator

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/usb-simulator/internal/protocol"
	"go.uber.org/zap"
)

// StationState 安检站状态
type StationState string

const (
	StateIdle      StationState = "idle"
	StateRegister  StationState = "registering"
	StateOnline    StationState = "online"
	StateReconnect StationState = "reconnecting"
)

// generateMsgID 生成消息ID（16字节随机数→32位十六进制，与协议示例格式一致）
func generateMsgID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

// generateUUID 生成模拟 UUID（8-4-4-4-12 格式），模拟真实机器 UUID
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Resources 资源使用率
type Resources struct {
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	Disk   float64 `json:"disk"`
}

// VirusLib 病毒库信息
type VirusLib struct {
	Type        int    `json:"type"`
	Version     string `json:"version"`
	UpgradeTime int64  `json:"upgradeTime"`
}

// UpgradeTask 升级任务
type UpgradeTask struct {
	TaskID      string `json:"taskId"`      // 升级任务标识（=下发请求108的msgId）
	UpgradeType string `json:"upgradeType"` // "virus" 或 "software"
	VirusType   int    `json:"virusType"`
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	Checksum    string `json:"checksum"`
	Status      string `json:"status"` // running / completed / failed
	Progress    int    `json:"progress"`
	IsRunning   bool   `json:"isRunning"`
	ErrorMsg    string `json:"errorMsg,omitempty"`
}

// SimStation 模拟安检站
type SimStation struct {
	ID       string `json:"stationId"`
	SN       string `json:"sn"`
	Model    string `json:"model"`
	Version  string `json:"version"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Name     string `json:"name"`
	DeviceID uint32 `json:"deviceId"`

	State StationState `json:"status"`

	// SASOC 连接配置
	SasocHost string `json:"sasocHost"`
	SasocPort int    `json:"sasocPort"`

	// 心跳配置
	HeartbeatEnabled  bool `json:"heartbeatEnabled"`
	HeartbeatInterval int  `json:"heartbeatInterval"` // 秒

	// 协议配置
	EncryptEnabled  bool `json:"encryptEnabled"`
	CompressEnabled bool `json:"compressEnabled"`

	// 业务数据
	VirusLibs   []VirusLib   `json:"virusLibs"`
	Cabinet     *Cabinet     `json:"cabinet"`
	Resources   Resources    `json:"resources"`
	UpgradeTask *UpgradeTask `json:"upgradeTask"`

	// 内部字段
	conn   net.Conn
	mu     sync.Mutex
	cancel context.CancelFunc
	ctx    context.Context
	msgCh  chan *protocol.Frame // 收到的 S→C 消息
	logger *zap.Logger
	uuid   string // 模拟机器 UUID，生成 ComputerID 用

	// 编码选项缓存
	encodeOpts protocol.EncodeOptions

	// 重连守卫：确保同一时刻只有一个 reconnectLoop 在运行
	reconnecting atomic.Bool

	// 消息统计
	MsgSent       int64     `json:"msgSent"`
	MsgReceived   int64     `json:"msgReceived"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`

	// 状态变更回调（由 Hub 注入，用于通知前端 WebSocket）
	OnStateChange func(stationID string, oldState, newState string, deviceID uint32) `json:"-"`

	// 消息接收回调（由 Hub 注入，用于记录消息日志）
	OnMessageReceived func(stationID string, cmdID uint32, body string, latencyMs int) `json:"-"`
}

// NewSimStation 创建模拟安检站
func NewSimStation(id, sn, model, version, name string) *SimStation {
	ctx, cancel := context.WithCancel(context.Background())
	return &SimStation{
		ID:                id,
		SN:                sn,
		Model:             model,
		Version:           version,
		Name:              name,
		IP:                "192.168.1.100",
		MAC:               "00:11:22:33:44:55",
		State:             StateIdle,
		SasocHost:         "192.168.1.1",
		SasocPort:         4567,
		HeartbeatEnabled:  true,
		HeartbeatInterval: 30,
		EncryptEnabled:    true,
		CompressEnabled:   true,
		VirusLibs: []VirusLib{
			{Type: 1, Version: "v3.2.1", UpgradeTime: time.Now().UnixMilli()},
			{Type: 2, Version: "v1.0.8", UpgradeTime: time.Now().UnixMilli()},
		},
		Cabinet:   NewCabinet(24),
		Resources: Resources{CPU: 11.12, Memory: 34.56, Disk: 56.78},
		ctx:       ctx,
		cancel:    cancel,
		msgCh:     make(chan *protocol.Frame, 100),
		logger:    zap.L(),
		uuid:      generateUUID(),
	}
}

// ComputerID 生成符合真实安检站格式的 ComputerID：MAC-UUID-Name
// 对齐真实客户端 GetComputerId()：snprintf("%s-%s-%s", szMacAddr, szUUID, szHostname)
func (s *SimStation) ComputerID() string {
	// 去掉 MAC 中的冒号转小写，对齐真实安检站格式
	mac := strings.ReplaceAll(strings.ToLower(s.MAC), ":", "-")
	// 取 Name 的最后一段（主机名），对齐真实格式
	name := s.Name
	if name == "" {
		name = s.ID
	}
	return fmt.Sprintf("%s-%s-%s", mac, s.uuid, name)
}

// RestoreSimStation 从持久化数据恢复模拟安检站
// 用于启动时从数据库恢复站点，保留已有的 SN/Model 等信息
func RestoreSimStation(id, sn, model, version, ip, mac, name string, deviceID uint32, sasocHost string, sasocPort int, heartbeatEnabled bool, heartbeatInterval int, encryptEnabled, compressEnabled bool) *SimStation {
	ctx, cancel := context.WithCancel(context.Background())
	return &SimStation{
		ID:                id,
		SN:                sn,
		Model:             model,
		Version:           version,
		IP:                ip,
		MAC:               mac,
		Name:              name,
		DeviceID:          deviceID,
		State:             StateIdle, // 恢复时状态为 idle
		SasocHost:         sasocHost,
		SasocPort:         sasocPort,
		HeartbeatEnabled:  heartbeatEnabled,
		HeartbeatInterval: heartbeatInterval,
		EncryptEnabled:    encryptEnabled,
		CompressEnabled:   compressEnabled,
		VirusLibs: []VirusLib{
			{Type: 1, Version: "v3.2.1", UpgradeTime: time.Now().UnixMilli()},
			{Type: 2, Version: "v1.0.8", UpgradeTime: time.Now().UnixMilli()},
		},
		Cabinet:   NewCabinet(24),
		Resources: Resources{CPU: 11.12, Memory: 34.56, Disk: 56.78},
		ctx:       ctx,
		cancel:    cancel,
		msgCh:     make(chan *protocol.Frame, 100),
		logger:    zap.L(),
		uuid:      generateUUID(),
	}
}

// Start 启动安检站（建立 TCP 连接 + 注册）
func (s *SimStation) Start() error {
	s.mu.Lock()
	if s.State != StateIdle && s.State != StateReconnect {
		s.mu.Unlock()
		return fmt.Errorf("station %s already running (state=%s)", s.ID, s.State)
	}
	s.mu.Unlock()

	return s.connectAndRegister()
}

// sleepOrCancel 可被取消的 sleep，升级过程中站点被停止时立即返回
func (s *SimStation) sleepOrCancel(d time.Duration) {
	select {
	case <-s.ctx.Done():
	case <-time.After(d):
	}
}

// Stop 停止安检站
func (s *SimStation) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldState := s.State
	s.cancel()

	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}

	s.State = StateIdle
	s.DeviceID = 0
	s.reconnecting.Store(false) // 重置重连守卫，确保 Restart 后可以重连
	if oldState != StateIdle {
		// 在锁外调用回调避免死锁
		go func() {
			if s.OnStateChange != nil {
				s.OnStateChange(s.ID, string(oldState), string(StateIdle), 0)
			}
		}()
	}
	return nil
}

// Restart 重启安检站
func (s *SimStation) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}

	// 重新创建 context
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.msgCh = make(chan *protocol.Frame, 100)

	time.Sleep(1 * time.Second)
	return s.Start()
}

// ReinitWithNewAddr 用新的SASOC地址重新初始化内部状态
func (s *SimStation) ReinitWithNewAddr(host string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SasocHost = host
	s.SasocPort = port
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.msgCh = make(chan *protocol.Frame, 100)
}

// connectAndRegister 建立 TCP 连接并发送注册请求
// SASOC 返回 PT 格式协议头（与老工具 WL_PORTOCAL_HEAD 一致）
func (s *SimStation) connectAndRegister() error {
	s.SetState(StateRegister)

	// 建立 TCP 连接
	addr := net.JoinHostPort(s.SasocHost, fmt.Sprintf("%d", s.SasocPort))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		s.SetState(StateReconnect)
		s.logger.Error("TCP connect failed",
			zap.String("station", s.ID),
			zap.Error(err),
		)
		// 启动重连 goroutine
		go s.reconnectLoop()
		return fmt.Errorf("connect to %s failed: %w", addr, err)
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	s.logger.Info("TCP connected",
		zap.String("station", s.ID),
		zap.String("remote", addr),
	)

	// 更新编码选项（每次发送时生成新的 RandomValue）
	s.mu.Lock()
	s.encodeOpts = protocol.EncodeOptions{
		Encrypt:     s.EncryptEnabled,
		Compress:    s.CompressEnabled,
		RandomValue: 0,
	}
	s.mu.Unlock()

	// 启动消息接收 goroutine（等待 SASOC 注册响应和后续推送）
	go s.receiveLoop()

	// 发送注册请求
	if err := s.sendRegister(); err != nil {
		s.SetState(StateReconnect)
		return fmt.Errorf("register failed: %w", err)
	}

	// 等待 SASOC 注册响应（由 receiveLoop -> handleFrame -> handleRegisterResponse 处理）
	// 超时兜底：如果 10 秒内未收到合法注册响应，自动标记 online 并启心跳
	go func() {
		time.Sleep(10 * time.Second)
		if s.GetState() == StateRegister {
			s.logger.Info("register response timeout, auto-marking online",
				zap.String("station", s.ID),
			)
			s.SetState(StateOnline)

			// 启动心跳
			if s.HeartbeatEnabled {
				go s.StartHeartbeatLoop()
			}
			// 自动信息上报
			go func() {
				time.Sleep(1 * time.Second)
				if err := s.sendInfoReport(); err != nil {
					s.logger.Warn("info report send failed", zap.String("station", s.ID), zap.Error(err))
				}
			}()
		}
	}()

	return nil
}

// reconnectLoop 重连循环（60 秒间隔）
// 使用 CAS 守卫确保每个站点同一时刻只有一个重连 goroutine 在运行
func (s *SimStation) reconnectLoop() {
	if !s.reconnecting.CompareAndSwap(false, true) {
		// 已有 reconnectLoop 在运行，直接退出
		return
	}
	defer s.reconnecting.Store(false)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.logger.Info("attempting reconnect", zap.String("station", s.ID))
			err := s.connectAndRegister()
			if err == nil {
				return // 重连成功
			}
			// 重连失败：connectAndRegister 内部会 go s.reconnectLoop()
			// 但由于 CAS 守卫，那个新 goroutine 会直接返回（因为当前还在运行）
			// 所以由当前循环继续重试，不会产生 goroutine 泄漏
		}
	}
}

// receiveLoop 消息接收循环
func (s *SimStation) receiveLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		if s.conn == nil {
			return
		}

		// 先读包头 48 字节
		headerBuf := make([]byte, protocol.HeaderSize)
		if _, err := s.readExact(headerBuf); err != nil {
			s.logger.Error("read header failed", zap.String("station", s.ID), zap.Error(err))
			s.handleDisconnect()
			return
		}

		header, err := protocol.DecodeHeader(headerBuf)
		if err != nil {
			s.logger.Error("decode header failed", zap.String("station", s.ID), zap.Error(err))
			continue
		}

		if err := header.Validate(); err != nil {
			// headFlag 不匹配，可能是帧偏移或 SASOC 格式差异
			s.logger.Warn("header validation failed",
				zap.String("station", s.ID),
				zap.Error(err),
				zap.String("rawHeader", fmt.Sprintf("%x", headerBuf)),
				zap.Uint8("headFlag0", headerBuf[0]),
				zap.Uint8("headFlag1", headerBuf[1]),
				zap.Uint16("bodyLen", header.BodyLen),
				zap.Uint32("cmdID", header.CmdID),
			)
			// 尝试跳过 bodyLen 字节以保持帧对齐，防止后续帧全部偏移
			if header.BodyLen > 0 && header.BodyLen <= protocol.MaxBodyLen {
				discard := make([]byte, header.BodyLen)
				if _, discErr := s.readExact(discard); discErr != nil {
					s.logger.Error("discard body failed after invalid header", zap.String("station", s.ID), zap.Error(discErr))
					s.handleDisconnect()
					return
				}
			}
			continue
		}

		// 读取包体
		var body []byte
		if header.BodyLen > 0 {
			body = make([]byte, header.BodyLen)
			if _, err := s.readExact(body); err != nil {
				s.logger.Error("read body failed", zap.String("station", s.ID), zap.Error(err))
				s.handleDisconnect()
				return
			}
		} else {
			body = []byte{}
		}

		frame := &protocol.Frame{Header: header, Body: body}
		s.MsgReceived++

		// 记录接收日志
		bodyJSON, _ := frame.BodyJSON()
		if bodyJSON == "" {
			bodyJSON = fmt.Sprintf("<raw:%d bytes>", len(body))
		}
		s.notifyMessageReceived(header.CmdID, bodyJSON)

		s.logger.Debug("received frame",
			zap.String("station", s.ID),
			zap.Uint32("cmdID", header.CmdID),
			zap.Uint16("bodyLen", header.BodyLen),
			zap.Bool("encrypted", header.IsEncrypted()),
			zap.Bool("compressed", header.IsCompressed()),
		)

		// 处理消息
		s.handleFrame(frame)
	}
}

// readExact 精确读取 n 字节
func (s *SimStation) readExact(buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := s.conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// handleDisconnect 处理断线
// 增加2秒防抖：网络瞬时抖动时如果2秒内恢复则不上报离线
func (s *SimStation) handleDisconnect() {
	// 快速重试一次，确认不是瞬时抖动
	time.Sleep(2 * time.Second)
	if s.IsOnline() {
		// 已自行恢复，无需通知
		return
	}

	s.mu.Lock()
	oldState := s.State

	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}

	s.State = StateReconnect
	s.DeviceID = 0
	s.mu.Unlock()

	if oldState == StateOnline {
		s.notifyStateChange(oldState)
	}

	s.logger.Warn("connection lost, starting reconnect", zap.String("station", s.ID))
	go s.reconnectLoop()
}

// handleFrame 处理收到的帧
func (s *SimStation) handleFrame(frame *protocol.Frame) {
	switch frame.Header.CmdID {
	case protocol.CmdRegister:
		s.handleRegisterResponse(frame)
	case protocol.CmdHeartbeat:
		// 心跳应答（可能是空包体或仅含确认信息）
		s.LastHeartbeat = time.Now()
		s.logger.Debug("heartbeat response received", zap.String("station", s.ID))
	case protocol.CmdUpgradeIssue:
		s.handleUpgradeIssue(frame)
	default:
		// 通用应答处理
		s.logger.Info("received frame",
			zap.String("station", s.ID),
			zap.Uint32("cmdID", frame.Header.CmdID),
		)
		select {
		case s.msgCh <- frame:
		default:
			s.logger.Warn("msgCh full, dropping frame", zap.String("station", s.ID))
		}
	}
}

// sendFrame 发送帧
// 每次发送时生成新的 RandomValue（与主机卫士 CProtocal::GetRandomKey 一致）
// 注意：调用方若已持有 s.mu，需使用 sendFrameUnsafe；sendFrame 会加锁，禁止在已加锁时调用。
func (s *SimStation) sendFrame(cmdID uint32, body interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sendFrameUnsafe(cmdID, body)
}

// sendFrameUnsafe 无锁发送帧（调用方必须已持有 s.mu）
func (s *SimStation) sendFrameUnsafe(cmdID uint32, body interface{}) error {
	if s.conn == nil {
		return fmt.Errorf("not connected")
	}

	// 每次加密时生成新的 RandomValue（与 C++ srand(time(NULL)) + rand() % USHORT_MAX 一致）
	opts := s.encodeOpts
	if opts.Encrypt {
		opts.RandomValue = uint16(rand.Intn(65535))
	}

	frameBytes, err := protocol.EncodeFrame(cmdID, s.DeviceID, body, opts)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}

	if _, err := s.conn.Write(frameBytes); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	s.MsgSent++
	return nil
}

// GetState 获取当前状态（线程安全）
func (s *SimStation) GetState() StationState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.State
}

// IsOnline 是否在线
func (s *SimStation) IsOnline() bool {
	return s.GetState() == StateOnline
}

// UpdateResources 更新资源使用率
func (s *SimStation) UpdateResources(cpu, memory, disk float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Resources = Resources{CPU: cpu, Memory: memory, Disk: disk}
}

// UpdateCabinetDoorStatus 更新柜门状态
func (s *SimStation) UpdateCabinetDoorStatus(doorNo int, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Cabinet != nil && doorNo >= 0 && doorNo < len(s.Cabinet.DoorStatus) {
		s.Cabinet.DoorStatus[doorNo] = status
	}
}

// SetCabinetSlotFault 设置管控柜插槽故障（线程安全）
func (s *SimStation) SetCabinetSlotFault(doorNo int, fault bool, reason string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Cabinet == nil {
		return false
	}
	return s.Cabinet.SetSlotFault(doorNo, fault, reason)
}

// SetAllCabinetSlotsFault 设置全部插槽故障（线程安全）
func (s *SimStation) SetAllCabinetSlotsFault(fault bool, reason string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Cabinet == nil {
		return false
	}
	s.Cabinet.SetAllSlotsFault(fault, reason)
	return true
}

// GetCabinetSlotStatus 获取插槽状态（线程安全）
func (s *SimStation) GetCabinetSlotStatus(doorNo int) (CabinetSlot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Cabinet == nil {
		return CabinetSlot{}, false
	}
	return s.Cabinet.GetSlotStatus(doorNo)
}

// GetCabinetSlotFaults 获取所有故障插槽（线程安全）
func (s *SimStation) GetCabinetSlotFaults() []CabinetSlot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Cabinet == nil {
		return nil
	}
	var result []CabinetSlot
	for _, slot := range s.Cabinet.Slots {
		if slot.Fault {
			result = append(result, slot)
		}
	}
	return result
}

// GetResources 获取资源使用率（线程安全）
func (s *SimStation) GetResources() Resources {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Resources
}

// GetCabinet 获取管控柜（线程安全）
func (s *SimStation) GetCabinet() *Cabinet {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Cabinet
}

// GetUpgradeTask 获取升级任务（线程安全）
func (s *SimStation) GetUpgradeTask() *UpgradeTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.UpgradeTask
}

// SetUpgradeTask 设置升级任务
func (s *SimStation) SetUpgradeTask(task *UpgradeTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpgradeTask = task
}

// GetDeviceID 获取设备ID
func (s *SimStation) GetDeviceID() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DeviceID
}

// SetDeviceID 设置设备ID
func (s *SimStation) SetDeviceID(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DeviceID = id
}

// notifyMessageReceived 通知消息接收（内部方法）
func (s *SimStation) notifyMessageReceived(cmdID uint32, body string) {
	if s.OnMessageReceived != nil {
		s.OnMessageReceived(s.ID, cmdID, body, 0)
	}
}

// notifyStateChange 通知状态变更（内部方法）
func (s *SimStation) notifyStateChange(oldState StationState) {
	if s.OnStateChange != nil {
		s.OnStateChange(s.ID, string(oldState), string(s.State), s.DeviceID)
	}
}

// SetState 设置状态
func (s *SimStation) SetState(state StationState) {
	s.mu.Lock()
	oldState := s.State
	s.State = state
	s.mu.Unlock()
	if oldState != state {
		s.notifyStateChange(oldState)
	}
}

// GetLogger 获取日志器
func (s *SimStation) GetLogger() *zap.Logger {
	return s.logger
}

// UpdateLastHeartbeat 更新心跳时间
func (s *SimStation) UpdateLastHeartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastHeartbeat = time.Now()
}

// SendFrame 发送帧（公共接口）
func (s *SimStation) SendFrame(cmdID uint32, body interface{}) error {
	return s.sendFrame(cmdID, body)
}

// sendRegister 发送注册请求（协议 §7.1）
// 外层Head包含 ComputerID/CMDID/CMDVER/msgId/CMDContent
func (s *SimStation) sendRegister() error {
	innerBody := map[string]interface{}{
		"model":   s.Model,
		"version": s.Version,
		"ip":      s.IP,
		"mac":     s.MAC,
		"name":    s.Name,
	}
	body := map[string]interface{}{
		"ComputerID": s.ComputerID(),
		"CMDID":      protocol.CmdRegister,
		"CMDVER":     1,
		"msgId":      generateMsgID(),
		"CMDContent": innerBody,
	}
	return s.sendFrame(protocol.CmdRegister, body)
}

// StartHeartbeatLoop 启动心跳循环
func (s *SimStation) StartHeartbeatLoop() {
	ticker := time.NewTicker(time.Duration(s.HeartbeatInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if !s.IsOnline() {
				return
			}
			resources := s.GetResources()
			cabinet := s.GetCabinet()
			body := map[string]interface{}{
				"ComputerID": s.ComputerID(),
				"CMDID":      protocol.CmdHeartbeat,
				"CMDVER":     1,
				"CMDContent": map[string]interface{}{
					"cpu":        resources.CPU,
					"memory":     resources.Memory,
					"disk":       resources.Disk,
					"doorStatus": cabinet.DoorStatus,
				},
			}
			if err := s.sendFrame(protocol.CmdHeartbeat, body); err != nil {
				s.logger.Error("heartbeat send failed", zap.String("station", s.ID), zap.Error(err))
				// 心跳发送失败说明连接已不可靠，主动关闭触发统一重连
				s.mu.Lock()
				if s.conn != nil {
					s.conn.Close()
				}
				s.mu.Unlock()
				return
			}
		}
	}
}

// handleRegisterResponse 处理注册响应
// SASOC 响应格式：{CMDID:102, msgId:"...", CMDContent:{code:0, message:"success", data:{deviceId:10001}}}
func (s *SimStation) handleRegisterResponse(frame *protocol.Frame) {
	// 尝试解码包体
	var resp map[string]interface{}
	if err := frame.DecodeJSONBody(&resp); err != nil {
		// 解码失败：记录详细诊断信息
		s.logger.Error("register response decode failed",
			zap.String("station", s.ID),
			zap.Error(err),
			zap.Bool("encrypted", frame.Header.IsEncrypted()),
			zap.Bool("compressed", frame.Header.IsCompressed()),
			zap.Uint16("randomValue", frame.Header.RandomValue),
			zap.Uint32("checksum", frame.Header.CheckSum),
			zap.Uint8("fillLen", frame.Header.FillLen),
			zap.Int("bodyLen", len(frame.Body)),
		)

		// 如果包体为空，可能是心跳应答被误判为注册响应，不改变状态
		if len(frame.Body) == 0 {
			s.logger.Warn("register response has empty body, ignoring", zap.String("station", s.ID))
			return
		}

		// 解码失败时仍然标记为在线（SASOC已确认注册成功，只是响应解析失败）
		// 因为 SASOC 平台已经显示设备在线，说明注册请求被接受了
		s.logger.Warn("marking station online despite decode failure (SASOC accepted registration)",
			zap.String("station", s.ID),
		)
		s.SetState(StateOnline)
		// 从包头 devID 提取设备ID
		if s.GetDeviceID() == 0 && frame.Header.DevID > 0 {
			s.SetDeviceID(frame.Header.DevID)
			s.logger.Info("deviceId from header (decode failed path)", zap.Uint32("deviceId", s.GetDeviceID()))
		}

		// 启动心跳
		if s.HeartbeatEnabled {
			go s.StartHeartbeatLoop()
		}
		// 自动信息上报
		go func() {
			time.Sleep(1 * time.Second)
			s.sendInfoReport()
		}()
		return
	}

	// 记录解码成功的响应
	s.logger.Info("register response decoded",
		zap.String("station", s.ID),
		zap.String("body", fmt.Sprintf("%v", resp)),
		zap.Uint32("headerDevID", frame.Header.DevID),
	)

	// 从 CMDContent 嵌套结构中提取 code 和 data
	cmdContent, _ := resp["CMDContent"].(map[string]interface{})
	if cmdContent == nil {
		// 兼容：如果没有 CMDContent 包裹，尝试直接从 resp 读取（Echo Server 场景）
		cmdContent = resp
	}

	code, _ := cmdContent["code"].(float64)

	switch int(code) {
	case protocol.CodeSuccess:
		// 从 CMDContent.data.deviceId 获取设备ID
		data, _ := cmdContent["data"].(map[string]interface{})
		if data != nil {
			if devid, ok := data["deviceId"].(float64); ok {
				s.SetDeviceID(uint32(devid))
			}
		} else {
			// 兼容：deviceId 直接在 CMDContent 下
			if devid, ok := cmdContent["deviceId"].(float64); ok {
				s.SetDeviceID(uint32(devid))
			}
		}
		// 补充：如果 JSON 中未获取到 deviceId，从包头 devID 字段提取
		if s.GetDeviceID() == 0 && frame.Header.DevID > 0 {
			s.SetDeviceID(frame.Header.DevID)
			s.logger.Info("deviceId from header", zap.Uint32("deviceId", s.GetDeviceID()))
		}
		s.SetState(StateOnline)
		s.logger.Info("register success",
			zap.String("station", s.ID),
			zap.Uint32("deviceId", s.GetDeviceID()),
		)
		// 启动心跳
		if s.HeartbeatEnabled {
			go s.StartHeartbeatLoop()
		}
		// 自动信息上报
		go func() {
			time.Sleep(1 * time.Second)
			s.sendInfoReport()
		}()

	case protocol.CodeNotRegistered:
		// 未注册：等待 5 秒后重新发送注册请求
		go func() {
			time.Sleep(5 * time.Second)
			if err := s.sendRegister(); err != nil {
				s.logger.Warn("re-register failed", zap.String("station", s.ID), zap.Error(err))
			}
		}()

	case protocol.CodeOverCapacity:
		s.logger.Error("register rejected: over capacity", zap.String("station", s.ID))
		s.SetState(StateIdle)

	default:
		s.logger.Warn("register response unknown code", zap.String("station", s.ID), zap.Int("code", int(code)))
	}
}

// sendInfoReport 发送信息上报（协议 §7.3）
func (s *SimStation) sendInfoReport() error {
	virusLibs := make([]map[string]interface{}, 0)
	for _, lib := range s.VirusLibs {
		virusLibs = append(virusLibs, map[string]interface{}{
			"type":        lib.Type,
			"version":     lib.Version,
			"upgradeTime": lib.UpgradeTime,
		})
	}

	innerBody := map[string]interface{}{
		"model":     s.Model,
		"version":   s.Version,
		"virusLibs": virusLibs,
		"cabinet":   s.Cabinet.ToReportMap(),
	}
	body := map[string]interface{}{
		"ComputerID": s.ComputerID(),
		"CMDID":      protocol.CmdInfoReport,
		"CMDVER":     1,
		"msgId":      generateMsgID(),
		"CMDContent": innerBody,
	}

	return s.sendFrame(protocol.CmdInfoReport, body)
}

// handleUpgradeIssue 处理升级命令下发
// S→C 下发格式有两种：
//   - 对象格式：{CMDID:108, msgId:"...", CMDContent:{...}}
//   - 数组格式：[{CMDID:108, msgId:"...", CMDContent:{...}}]（真实 SASOC 平台格式）
func (s *SimStation) handleUpgradeIssue(frame *protocol.Frame) {
	// 先尝试对象格式，失败则尝试数组格式
	var req map[string]interface{}
	if err := frame.DecodeJSONBody(&req); err != nil {
		// 数组格式：[{...}]
		var arr []map[string]interface{}
		if e2 := frame.DecodeJSONBody(&arr); e2 != nil || len(arr) == 0 {
			s.logger.Error("CMD108 decode failed (both object and array)", zap.Error(err))
			return
		}
		req = arr[0]
	}

	// 从 CMDContent 提取业务数据
	cmdContent, _ := req["CMDContent"].(map[string]interface{})
	if cmdContent == nil {
		cmdContent = req // 兼容无包裹格式
	}

	// msgId 在 req 层面（与 CMDContent 同级），不在 cmdContent 内部
	taskId, _ := req["msgId"].(string)
	if taskId == "" {
		taskId = fmt.Sprintf("upgrade-%d", time.Now().UnixMilli())
	}

	// 打印收到的 CMD108 完整内容（用于定位问题）
	reqJSON, _ := json.Marshal(req)
	cmdJSON, _ := json.Marshal(cmdContent)
	s.logger.Info("收到 CMD108 升级下发",
		zap.String("station", s.SN),
		zap.String("msgId", taskId),
		zap.ByteString("req", reqJSON),
		zap.ByteString("cmdContent", cmdJSON),
	)

	virusType, _ := cmdContent["virusType"].(float64)
	version, _ := cmdContent["version"].(string)
	downloadUrl, _ := cmdContent["downloadUrl"].(string)
	checksum, _ := cmdContent["checksum"].(string)

	// 判断升级类型：对齐真实客户端 SasocUpgradeBridge.cpp:221-266
	// 判定为软件升级：upgradeType == "software" 或存在 PackageName 字段
	upgradeType, _ := cmdContent["upgradeType"].(string)
	if upgradeType == "" {
		upgradeType, _ = cmdContent["type"].(string) // 兼容旧字段名
	}
	if upgradeType == "" {
		// 通过是否存在 PackageName 判断是否为软件升级
		if _, hasPkg := cmdContent["PackageName"]; hasPkg {
			upgradeType = "software"
		} else {
			upgradeType = "virus" // 默认病毒库升级
		}
	}

	s.mu.Lock()

	// 检查是否已有升级任务
	if s.UpgradeTask != nil && s.UpgradeTask.IsRunning {
		// 拒绝，返回 code=3001
		innerBody := map[string]interface{}{"code": protocol.CodeTaskExclusive, "message": "已有升级任务执行中"}
		body := map[string]interface{}{
			"ComputerID": s.ComputerID(),
			"CMDID":      protocol.CmdUpgradeIssue,
			"CMDVER":     1,
			"msgId":      taskId,
			"CMDContent": innerBody,
		}
		s.sendFrameUnsafe(protocol.CmdUpgradeIssue, body)
		s.mu.Unlock()
		return
	}

	// 接受升级任务
	s.UpgradeTask = &UpgradeTask{
		TaskID:      taskId,
		UpgradeType: upgradeType,
		VirusType:   int(virusType),
		Version:     version,
		DownloadURL: downloadUrl,
		Checksum:    checksum,
		Status:      "downloading",
		Progress:    0,
	}

	// 1. 先发 CMD109 "downloading" 进度（与真实代码完全一致：先发109再发108响应）
	s.sendUpgradeResultUnsafe(taskId, "downloading", 0, "virus package downloading")

	// 2. 发 CMD108 应答（对齐真实客户端 SasocTcpBuildResponseJson：必须含 ComputerID + CMDVER）
	respContent := struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}{
		Code:    protocol.CodeSuccess,
		Message: "success",
		Data:    map[string]interface{}{},
	}
	respBody := struct {
		ComputerID string      `json:"ComputerID"`
		CMDID      uint32      `json:"CMDID"`
		CMDVER     int         `json:"CMDVER"`
		MsgID      string      `json:"msgId"`
		CMDContent interface{} `json:"CMDContent"`
	}{
		ComputerID: s.ComputerID(),
		CMDID:      protocol.CmdUpgradeIssue,
		CMDVER:     1,
		MsgID:      taskId,
		CMDContent: respContent,
	}
	s.sendFrameUnsafe(protocol.CmdUpgradeIssue, respBody)

	s.mu.Unlock()

	// 启动升级流程（必须在释放锁之后，否则 ExecuteUpgrade 中的 time.Sleep 会阻塞心跳）
	go s.ExecuteUpgrade()
}

func (s *SimStation) sendUpgradeOpLog(operation, message string) {
	body := map[string]interface{}{
		"ComputerID": s.ComputerID(),
		"CMDID":      protocol.CmdOperationLog,
		"CMDVER":     1,
		"msgId":      generateMsgID(),
		"CMDContent": map[string]interface{}{
			"timestamp": time.Now().UnixMilli(),
			"sn":        s.SN,
			"operation": operation,
			"result":    "success",
			"message":   message,
		},
	}
	if err := s.sendFrame(protocol.CmdOperationLog, body); err != nil {
		s.logger.Warn("upgrade op-log failed", zap.String("station", s.ID), zap.Error(err))
	}
}

// sendUpgradeResultUnsafe 发送 CMD109 升级结果上报（无锁版本，调用方必须已持有 s.mu）
// 严格对齐协议文档 §7.9 示例：
//
//	根层只含 CMDID + CMDVER + msgId + CMDContent
//	CMDContent 含 taskId + status + progress/virusType/version/errorCode + message
//	msgId 使用普通 UUID（非 taskId-result-status-timestamp 格式）
//	status 取值：running / completed / failed（文档未定义 downloading / downloaded）
func (s *SimStation) sendUpgradeResultUnsafe(taskId, status string, progress int, message string) {
	if taskId == "" {
		return
	}

	// CMDContent 按协议 §7.9 进度示例：taskId → status → progress → message
	// 完成示例：taskId → status → virusType → version
	// 失败示例：taskId → status → errorCode → message
	// 对齐真实客户端 SasocUpgradeBridge.cpp:70-126：必须包含 upgradeType
	content := map[string]interface{}{
		"taskId": taskId,
		"status": status,
	}
	// upgradeType 必须与真实客户端一致：SasocUpgradeBridge.cpp:89
	if s.UpgradeTask != nil {
		content["upgradeType"] = s.UpgradeTask.UpgradeType
	}
	if progress >= 0 {
		content["progress"] = progress
	}
	if status == "completed" && s.UpgradeTask != nil {
		content["virusType"] = s.UpgradeTask.VirusType
		content["version"] = s.UpgradeTask.Version
	}
	if message != "" {
		content["message"] = message
	}

	// msgId 格式对齐真实客户端：{taskId}-result-{status}-{timestamp}
	// SasocUpgradeBridge.cpp:84: snprintf(msgId, "%s-result-%s-%ld", taskId, status, (long)time(NULL))
	timestamp := time.Now().Unix()
	msgId := fmt.Sprintf("%s-result-%s-%d", taskId, status, timestamp)

	body := struct {
		ComputerID string      `json:"ComputerID"`
		CMDID      uint32      `json:"CMDID"`
		CMDVER     int         `json:"CMDVER"`
		MsgID      string      `json:"msgId"`
		CMDContent interface{} `json:"CMDContent"`
	}{
		ComputerID: s.ComputerID(),
		CMDID:      protocol.CmdUpgradeResult,
		CMDVER:     1,
		MsgID:      msgId,
		CMDContent: content,
	}

	// 打印发送的 CMD109 完整内容（用于定位问题）
	bodyJSON, _ := json.Marshal(body)
	s.logger.Info("发送 CMD109 升级结果上报",
		zap.String("station", s.SN),
		zap.String("taskId", taskId),
		zap.String("status", status),
		zap.Int("progress", progress),
		zap.ByteString("body", bodyJSON),
	)

	s.sendFrameUnsafe(protocol.CmdUpgradeResult, body)
}

// sendUpgradeResult 发送 CMD109 升级结果上报（加锁版本）
func (s *SimStation) sendUpgradeResult(taskId, status string, progress int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendUpgradeResultUnsafe(taskId, status, progress, message)
}

// ExecuteUpgrade 执行升级流程（模拟，无真实下载）
// 状态流转对齐真实客户端 SasocUpgradeBridge.cpp，同时模拟渐进式进度上报：
//
//	病毒库：downloading(0→100%) → downloaded(100%) → running(80→95%) → completed(100%)
//	软件：  downloading(0→100%) → running(50→90%) → completed(100%)
//
// 总时长约 80 秒（1~2 分钟区间），避免 0% 直接跳变到 100%
// 注意：初始 downloading(0%) 已在 handleUpgradeIssue 中发送
func (s *SimStation) ExecuteUpgrade() {
	if s.UpgradeTask == nil {
		return
	}

	taskId := s.UpgradeTask.TaskID
	s.UpgradeTask.IsRunning = true
	isSoftware := s.UpgradeTask.UpgradeType == "software"

	// 记录操作日志：收到升级指令
	s.sendUpgradeOpLog("upgrade", fmt.Sprintf("收到%s升级指令，taskId=%s，version=%s",
		s.UpgradeTask.UpgradeType, taskId, s.UpgradeTask.Version))

	if isSoftware {
		// 软件升级：downloading(0→100%) → running(50→90%) → completed(100%)
		// 阶段1：下载进度 10%→100%，每 5 秒上报一次（约 50 秒）
		for p := 10; p <= 100; p += 10 {
			s.sleepOrCancel(5 * time.Second)
			s.sendUpgradeResult(taskId, "downloading", p, fmt.Sprintf("software downloading %d%%", p))
		}
		// 阶段2：安装进度 50%→90%，每 8 秒上报一次（约 32 秒）
		for p := 50; p <= 90; p += 10 {
			s.sendUpgradeResult(taskId, "running", p, fmt.Sprintf("software upgrade running %d%%", p))
			s.sleepOrCancel(8 * time.Second)
		}
		// 阶段3：完成
		s.UpgradeTask.Status = "completed"
		s.UpgradeTask.Progress = 100
		s.sendUpgradeResult(taskId, "completed", 100, "software upgrade completed")
	} else {
		// 病毒库升级：downloading(0→100%) → downloaded(100%) → running(80→95%) → completed(100%)
		// 阶段1：下载进度 10%→100%，每 5 秒上报一次（约 50 秒）
		for p := 10; p <= 100; p += 10 {
			s.sleepOrCancel(5 * time.Second)
			s.sendUpgradeResult(taskId, "downloading", p, fmt.Sprintf("virus package downloading %d%%", p))
		}
		// 阶段2：下载完成
		s.sendUpgradeResult(taskId, "downloaded", 100, "virus package downloaded")
		// 阶段3：安装进度 80%→95%，每 8 秒上报一次（约 24 秒）
		s.sendUpgradeResult(taskId, "running", 80, "virus package installing")
		for p := 85; p <= 90; p += 5 {
			s.sleepOrCancel(8 * time.Second)
			s.sendUpgradeResult(taskId, "running", p, "virus package installing")
		}
		s.sleepOrCancel(8 * time.Second)
		s.sendUpgradeResult(taskId, "running", 95, "virus engine restarting")
		// 阶段4：完成
		s.sleepOrCancel(2 * time.Second)
		s.UpgradeTask.Status = "completed"
		s.UpgradeTask.Progress = 100
		s.sendUpgradeResult(taskId, "completed", 100, "virus upgrade completed")
	}

	// 记录操作日志：升级完成
	s.sendUpgradeOpLog("upgrade", fmt.Sprintf("%s升级完成，taskId=%s，version=%s",
		s.UpgradeTask.UpgradeType, taskId, s.UpgradeTask.Version))

	// 更新版本
	s.mu.Lock()
	if s.UpgradeTask.UpgradeType == "software" {
		// 软件升级：更新站点自身版本号
		s.Version = s.UpgradeTask.Version
	} else {
		// 病毒库升级：更新病毒库版本
		for i := range s.VirusLibs {
			if s.VirusLibs[i].Type == s.UpgradeTask.VirusType {
				s.VirusLibs[i].Version = s.UpgradeTask.Version
				s.VirusLibs[i].UpgradeTime = time.Now().UnixMilli()
			}
		}
	}
	s.mu.Unlock()

	// 自动信息上报刷新版本
	time.Sleep(1 * time.Second)
	s.sendInfoReport()

	// 记录操作日志：信息上报已刷新
	s.sendUpgradeOpLog("upgrade", fmt.Sprintf("信息上报已刷新，展示新版本 %s", s.UpgradeTask.Version))

	// 清除升级任务
	s.mu.Lock()
	s.UpgradeTask.IsRunning = false
	s.UpgradeTask = nil
	s.mu.Unlock()
}
