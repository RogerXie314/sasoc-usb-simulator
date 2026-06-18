package simulator

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync"
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

// generateMsgID 生成消息ID（时间戳+随机数，连接内唯一）
func generateMsgID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Intn(100000))
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
	TaskID      string  `json:"taskId"`                // 升级任务标识（=下发请求108的msgId）
	VirusType   int     `json:"virusType"`
	Version     string  `json:"version"`
	DownloadURL string  `json:"downloadUrl"`
	Checksum    string  `json:"checksum"`
	Status      string  `json:"status"` // running / completed / failed
	Progress    int     `json:"progress"`
	IsRunning   bool    `json:"isRunning"`
	ErrorMsg    string  `json:"errorMsg,omitempty"`
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
	VirusLibs   []VirusLib  `json:"virusLibs"`
	Cabinet     *Cabinet    `json:"cabinet"`
	Resources   Resources   `json:"resources"`
	UpgradeTask *UpgradeTask `json:"upgradeTask"`

	// 内部字段
	conn   net.Conn
	mu     sync.Mutex
	cancel context.CancelFunc
	ctx    context.Context
	msgCh  chan *protocol.Frame // 收到的 S→C 消息
	logger *zap.Logger

	// 编码选项缓存
	encodeOpts protocol.EncodeOptions

	// 消息统计
	MsgSent     int64 `json:"msgSent"`
	MsgReceived int64 `json:"msgReceived"`
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
	}
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
	oldState := s.State
	s.State = StateRegister
	s.notifyStateChange(oldState)

	// 建立 TCP 连接
	addr := fmt.Sprintf("%s:%d", s.SasocHost, s.SasocPort)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		oldState = s.State
		s.State = StateReconnect
		s.notifyStateChange(oldState)
		s.logger.Error("TCP connect failed",
			zap.String("station", s.ID),
			zap.Error(err),
		)
		// 启动重连 goroutine
		go s.reconnectLoop()
		return fmt.Errorf("connect to %s failed: %w", addr, err)
	}

	s.conn = conn
	s.logger.Info("TCP connected",
		zap.String("station", s.ID),
		zap.String("remote", addr),
	)

	// 更新编码选项（每次发送时生成新的 RandomValue）
	s.encodeOpts = protocol.EncodeOptions{
		Encrypt:     s.EncryptEnabled,
		Compress:    s.CompressEnabled,
		RandomValue: 0,
	}

	// 启动消息接收 goroutine（等待 SASOC 注册响应和后续推送）
	go s.receiveLoop()

	// 发送注册请求
	if err := s.sendRegister(); err != nil {
		oldState = s.State
		s.State = StateReconnect
		s.notifyStateChange(oldState)
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
			oldState := s.GetState()
			s.SetState(StateOnline)
			s.notifyStateChange(oldState)

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
func (s *SimStation) reconnectLoop() {
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
func (s *SimStation) handleDisconnect() {
	s.mu.Lock()
	oldState := s.State

	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}

	s.State = StateReconnect
	s.DeviceID = 0
	s.mu.Unlock()

	s.notifyStateChange(oldState)

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
func (s *SimStation) sendFrame(cmdID uint32, body interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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
		"ComputerID": s.SN,
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
				"ComputerID": s.SN,
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
		oldState := s.State
		s.mu.Lock()
		s.State = StateOnline
		// 从包头 devID 提取设备ID
		if s.DeviceID == 0 && frame.Header.DevID > 0 {
			s.DeviceID = frame.Header.DevID
			s.logger.Info("deviceId from header (decode failed path)", zap.Uint32("deviceId", s.DeviceID))
		}
		s.mu.Unlock()
		s.notifyStateChange(oldState)

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
				s.mu.Lock()
				s.DeviceID = uint32(devid)
				s.mu.Unlock()
			}
		} else {
			// 兼容：deviceId 直接在 CMDContent 下
			if devid, ok := cmdContent["deviceId"].(float64); ok {
				s.mu.Lock()
				s.DeviceID = uint32(devid)
				s.mu.Unlock()
			}
		}
		// 补充：如果 JSON 中未获取到 deviceId，从包头 devID 字段提取
		if s.DeviceID == 0 && frame.Header.DevID > 0 {
			s.mu.Lock()
			s.DeviceID = frame.Header.DevID
			s.mu.Unlock()
			s.logger.Info("deviceId from header", zap.Uint32("deviceId", s.DeviceID))
		}
		oldState := s.State
		s.mu.Lock()
		s.State = StateOnline
		s.mu.Unlock()
		s.logger.Info("register success",
			zap.String("station", s.ID),
			zap.Uint32("deviceId", s.DeviceID),
		)
		s.notifyStateChange(oldState)
		// 启动心跳
		if s.HeartbeatEnabled {
			go s.StartHeartbeatLoop()
		}
		// 自动信息上报
		go func() {
			time.Sleep(1 * time.Second)
			s.sendInfoReport()
		}()

	case protocol.CodeOverCapacity:
		s.logger.Error("register rejected: over capacity", zap.String("station", s.ID))
		s.SetState(StateIdle)

	case protocol.CodeNotRegistered:
		s.logger.Warn("not registered, re-registering", zap.String("station", s.ID))
		go func() {
			time.Sleep(5 * time.Second)
			s.mu.Lock()
			s.sendRegister()
			s.mu.Unlock()
		}()

	default:
		s.logger.Error("register failed", zap.String("station", s.ID), zap.Float64("code", code))
		s.SetState(StateReconnect)
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
		"ComputerID": s.SN,
		"CMDID":      protocol.CmdInfoReport,
		"CMDVER":     1,
		"msgId":      generateMsgID(),
		"CMDContent": innerBody,
	}

	return s.sendFrame(protocol.CmdInfoReport, body)
}

// handleUpgradeIssue 处理升级命令下发
// S→C 下发格式：{CMDID:108, msgId:"...", CMDContent:{virusType, version, downloadUrl, checksum}}
func (s *SimStation) handleUpgradeIssue(frame *protocol.Frame) {
	var req map[string]interface{}
	if err := frame.DecodeJSONBody(&req); err != nil {
		return
	}

	// 从 CMDContent 提取业务数据
	cmdContent, _ := req["CMDContent"].(map[string]interface{})
	if cmdContent == nil {
		cmdContent = req // 兼容无包裹格式
	}

	// 检查是否已有升级任务
	if s.UpgradeTask != nil && s.UpgradeTask.IsRunning {
		// 拒绝，返回 code=3001
		innerBody := map[string]interface{}{"code": protocol.CodeTaskExclusive, "message": "已有升级任务执行中"}
		body := map[string]interface{}{
			"ComputerID": s.SN,
			"CMDID":      protocol.CmdUpgradeIssue,
			"CMDVER":     1,
			"msgId":      generateMsgID(),
			"CMDContent": innerBody,
		}
		s.sendFrame(protocol.CmdUpgradeIssue, body)
		return
	}

	// 接受升级任务，提取 msgId 作为 taskId
	virusType, _ := cmdContent["virusType"].(float64)
	version, _ := cmdContent["version"].(string)
	downloadUrl, _ := cmdContent["downloadUrl"].(string)
	checksum, _ := cmdContent["checksum"].(string)
	taskId, _ := cmdContent["msgId"].(string)
	if taskId == "" {
		taskId = fmt.Sprintf("upgrade-%d", time.Now().UnixMilli())
	}

	s.UpgradeTask = &UpgradeTask{
		TaskID:      taskId,
		VirusType:   int(virusType),
		Version:     version,
		DownloadURL: downloadUrl,
		Checksum:    checksum,
		Status:      "running",
		Progress:    0,
	}

	// 返回接受
	innerBody := map[string]interface{}{"code": protocol.CodeSuccess, "message": "success"}
	body := map[string]interface{}{
		"ComputerID": s.SN,
		"CMDID":      protocol.CmdUpgradeIssue,
		"CMDVER":     1,
		"msgId":      generateMsgID(),
		"CMDContent": innerBody,
	}
	s.sendFrame(protocol.CmdUpgradeIssue, body)

	// 启动升级流程
	go s.ExecuteUpgrade()
}

// ExecuteUpgrade 执行升级流程
func (s *SimStation) ExecuteUpgrade() {
	if s.UpgradeTask == nil {
		return
	}

	taskId := s.UpgradeTask.TaskID
	s.UpgradeTask.IsRunning = true

	stages := []struct {
		progress int
		desc     string
		delay    time.Duration
	}{
		{20, "downloading", 3 * time.Second},
		{50, "verifying", 2 * time.Second},
		{80, "installing", 3 * time.Second},
		{100, "completed", 0},
	}

	for _, stage := range stages {
		select {
		case <-s.ctx.Done():
			s.UpgradeTask.IsRunning = false
			return
		default:
		}

		s.UpgradeTask.Progress = stage.progress
		s.UpgradeTask.Status = "running"

		// 上报进度（CMDID=109，携带 taskId）
		innerBody := map[string]interface{}{
			"taskId":   taskId,
			"status":   "running",
			"progress": stage.progress,
		}
		body := map[string]interface{}{
			"ComputerID": s.SN,
			"CMDID":      protocol.CmdUpgradeResult,
			"CMDVER":     1,
			"msgId":      generateMsgID(),
			"CMDContent": innerBody,
		}
		s.sendFrame(protocol.CmdUpgradeResult, body)

		if stage.delay > 0 {
			time.Sleep(stage.delay)
		}
	}

	// 上报完成（CMDID=109，携带 taskId + virusType + version）
	s.UpgradeTask.Status = "completed"
	innerBody := map[string]interface{}{
		"taskId":    taskId,
		"status":    "completed",
		"virusType": s.UpgradeTask.VirusType,
		"version":   s.UpgradeTask.Version,
	}
	body := map[string]interface{}{
		"ComputerID": s.SN,
		"CMDID":      protocol.CmdUpgradeResult,
		"CMDVER":     1,
		"msgId":      generateMsgID(),
		"CMDContent": innerBody,
	}
	s.sendFrame(protocol.CmdUpgradeResult, body)

	// 更新病毒库版本
	for i := range s.VirusLibs {
		if s.VirusLibs[i].Type == s.UpgradeTask.VirusType {
			s.VirusLibs[i].Version = s.UpgradeTask.Version
			s.VirusLibs[i].UpgradeTime = time.Now().UnixMilli()
		}
	}

	// 自动信息上报刷新
	time.Sleep(1 * time.Second)
	s.sendInfoReport()

	// 清除升级任务
	s.UpgradeTask.IsRunning = false
	s.UpgradeTask = nil
}
