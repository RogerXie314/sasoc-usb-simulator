package hub

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/model"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
)

// lastIP 最后分配的 IP 地址（用于递增分配）
var lastIP = [4]int{192, 168, 1, 100}
var lastIPMu sync.Mutex

// GenerateRandomIP 生成随机 IP 地址
func GenerateRandomIP() string {
	return fmt.Sprintf("192.168.%d.%d", rand.Intn(256), rand.Intn(256))
}

// GenerateSequentialIP 生成递增的 IP 地址
func GenerateSequentialIP() string {
	lastIPMu.Lock()
	defer lastIPMu.Unlock()
	lastIP[3]++
	if lastIP[3] > 254 {
		lastIP[3] = 1
		lastIP[2]++
		if lastIP[2] > 254 {
			lastIP[2] = 1
		}
	}
	return fmt.Sprintf("%d.%d.%d.%d", lastIP[0], lastIP[1], lastIP[2], lastIP[3])
}

// lastMAC 最后分配的 MAC 地址
var lastMAC = [6]int{0x00, 0x50, 0x56, 0xC0, 0x00, 0x00}
var lastMACMu sync.Mutex

// GenerateRandomMAC 生成随机 MAC 地址
func GenerateRandomMAC() string {
	return fmt.Sprintf("%02X-%02X-%02X-%02X-%02X-%02X",
		rand.Intn(256), rand.Intn(256), rand.Intn(256),
		rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

// GenerateSequentialMAC 生成递增的 MAC 地址
func GenerateSequentialMAC() string {
	lastMACMu.Lock()
	defer lastMACMu.Unlock()
	// 从第4字节开始递增
	for i := 5; i >= 3; i-- {
		lastMAC[i]++
		if lastMAC[i] > 255 {
			lastMAC[i] = 0
		} else {
			break
		}
	}
	return fmt.Sprintf("%02X-%02X-%02X-%02X-%02X-%02X",
		lastMAC[0], lastMAC[1], lastMAC[2], lastMAC[3], lastMAC[4], lastMAC[5])
}

type Event struct {
	Type      string      `json:"type"`      // 事件类型
	Timestamp time.Time   `json:"timestamp"` // 事件时间
	Payload   interface{} `json:"payload"`   // 事件数据
}

// EventSubscriber 事件订阅者（通常是 WebSocket 连接）
type EventSubscriber struct {
	ID   string
	Ch   chan Event
	Quit chan struct{}
}

// EventBus 事件总线：发布-订阅模式
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]*EventSubscriber // key = event type
	bufferSize  int
	logger      *zap.Logger
}

// NewEventBus 创建事件总线
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &EventBus{
		subscribers: make(map[string][]*EventSubscriber),
		bufferSize:  bufferSize,
		logger:      zap.L(),
	}
}

// Subscribe 订阅事件类型，返回订阅者对象
func (eb *EventBus) Subscribe(eventType string) *EventSubscriber {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	sub := &EventSubscriber{
		ID:   fmt.Sprintf("sub-%d", time.Now().UnixNano()),
		Ch:   make(chan Event, eb.bufferSize),
		Quit: make(chan struct{}),
	}

	eb.subscribers[eventType] = append(eb.subscribers[eventType], sub)
	eb.logger.Debug("event subscriber added",
		zap.String("eventType", eventType),
		zap.String("subscriberID", sub.ID),
	)
	return sub
}

// Unsubscribe 取消订阅
func (eb *EventBus) Unsubscribe(eventType, subscriberID string) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subs, ok := eb.subscribers[eventType]
	if !ok {
		return
	}
	for i, sub := range subs {
		if sub.ID == subscriberID {
			close(sub.Quit)
			eb.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			eb.logger.Debug("event subscriber removed",
				zap.String("eventType", eventType),
				zap.String("subscriberID", subscriberID),
			)
			break
		}
	}
}

// Publish 发布事件到所有订阅者（非阻塞）
func (eb *EventBus) Publish(eventType string, payload interface{}) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	event := Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   payload,
	}

	subs, ok := eb.subscribers[eventType]
	if !ok {
		return
	}

	for _, sub := range subs {
		select {
		case sub.Ch <- event:
		default:
			// 订阅者通道满，丢弃事件并记录
			eb.logger.Warn("event subscriber channel full, dropping event",
				zap.String("eventType", eventType),
				zap.String("subscriberID", sub.ID),
			)
		}
	}
}

// SubscriberCount 返回某事件类型的订阅者数量
func (eb *EventBus) SubscriberCount(eventType string) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers[eventType])
}

// 事件类型常量
const (
	EventStationStateChanged = "station_state_changed"
	EventMessageSent         = "message_sent"
	EventMessageReceived     = "message_received"
	EventPressureMetric      = "pressure_metric"
	EventUsbInserted         = "usb_inserted"
	EventUsbRemoved          = "usb_removed"
)

// StationStatePayload station_state_changed 事件数据
type StationStatePayload struct {
	StationID string `json:"stationId"`
	OldState  string `json:"oldState"`
	NewState  string `json:"newState"`
	DeviceID  uint32 `json:"deviceId,omitempty"`
}

// MessagePayload message_sent / message_received 事件数据
type MessagePayload struct {
	StationID string `json:"stationId"`
	CmdID     uint32 `json:"cmdid"`
	Direction string `json:"direction"`
	Body      string `json:"body,omitempty"`
	LatencyMs int    `json:"latencyMs,omitempty"`
}

// PressureMetricPayload pressure_metric 事件数据
type PressureMetricPayload struct {
	TestID               int64   `json:"testId"`
	OnlineCount          int     `json:"onlineCount"`
	HeartbeatSuccessRate float64 `json:"heartbeatSuccessRate"`
	AvgLatencyMs         float64 `json:"avgLatencyMs"`
	Throughput           float64 `json:"throughput"`
}

// UsbEventPayload usb_inserted / usb_removed 事件数据
type UsbEventPayload struct {
	UsbID     string `json:"usbId"`
	StationID string `json:"stationId,omitempty"`
	DoorNo    int    `json:"doorNo,omitempty"`
}

// Hub 管理所有模拟器实例（安检站 + U盘设备）
type Hub struct {
	mu       sync.RWMutex
	stations map[string]*simulator.SimStation // key = station ID
	usbDevs  map[string]*simulator.UsbDevice  // key = usb ID
	db       *sql.DB
	bus      *EventBus
	logger   *zap.Logger
	cfg      *config.Config // 运行时配置（可被 API 修改）
}

// NewHub 创建 Hub
func NewHub(db *sql.DB, bus *EventBus, cfg *config.Config) *Hub {
	return &Hub{
		stations: make(map[string]*simulator.SimStation),
		usbDevs:  make(map[string]*simulator.UsbDevice),
		db:       db,
		bus:      bus,
		logger:   zap.L(),
		cfg:      cfg,
	}
}

// Config 返回运行时配置
func (h *Hub) Config() *config.Config {
	return h.cfg
}

// DB 返回数据库引用
func (h *Hub) DB() *sql.DB {
	return h.db
}

// EventBus 返回事件总线引用
func (h *Hub) EventBus() *EventBus {
	return h.bus
}

// --- Station 管理 ---

// RestoreStations 从数据库恢复已有站点到内存
// 启动时调用，恢复上次运行时创建的站点（状态重置为 idle，需要手动启动连接）
func (h *Hub) RestoreStations() error {
	if h.db == nil {
		return nil
	}

	rows, err := model.ListStations(h.db, "")
	if err != nil {
		return fmt.Errorf("list stations from DB: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, row := range rows {
		// 解析 config JSON
		var cfgMap map[string]interface{}
		if err := json.Unmarshal([]byte(row.Config), &cfgMap); err != nil {
			h.logger.Warn("failed to parse station config, using defaults",
				zap.String("stationID", row.ID),
				zap.Error(err),
			)
			cfgMap = make(map[string]interface{})
		}

		// 从配置中提取SASOC地址
		sasocHost := h.cfg.Sasoc.Host
		sasocPort := h.cfg.Sasoc.Port
		if v, ok := cfgMap["sasocHost"].(string); ok && v != "" {
			sasocHost = v
		}
		if v, ok := cfgMap["sasocPort"].(float64); ok && v > 0 {
			sasocPort = int(v)
		}

		// 从配置中提取心跳和加密设置
		heartbeatEnabled := true
		heartbeatInterval := 30
		encryptEnabled := h.cfg.Simulator.Encrypt
		compressEnabled := h.cfg.Simulator.Compress
		if v, ok := cfgMap["heartbeatEnabled"].(bool); ok {
			heartbeatEnabled = v
		}
		if v, ok := cfgMap["heartbeatInterval"].(float64); ok && v > 0 {
			heartbeatInterval = int(v)
		}
		if v, ok := cfgMap["encryptEnabled"].(bool); ok {
			encryptEnabled = v
		}
		if v, ok := cfgMap["compressEnabled"].(bool); ok {
			compressEnabled = v
		}

		// 使用工厂函数恢复 SimStation 对象
		s := simulator.RestoreSimStation(
			row.ID, row.SN, row.Model, row.Version,
			row.IP, row.MAC, row.Name,
			uint32(row.DeviceID),
			sasocHost, sasocPort,
			heartbeatEnabled, heartbeatInterval,
			encryptEnabled, compressEnabled,
		)

		// 注入状态变更回调
		s.OnStateChange = func(stationID string, oldState, newState string, deviceID uint32) {
			h.NotifyStationStateChange(stationID, oldState, newState, deviceID)
		}
		// 注入消息接收回调
		s.OnMessageReceived = func(stationID string, cmdID uint32, body string, latencyMs int) {
			h.NotifyMessageReceived(stationID, cmdID, body, latencyMs)
		}

		h.stations[row.ID] = s
		h.logger.Info("station restored from DB",
			zap.String("stationID", row.ID),
			zap.String("sn", row.SN),
		)
	}

	h.logger.Info("stations restored", zap.Int("count", len(rows)))
	return nil
}

// AddStation 添加安检站（内存 + 数据库）
// 如果站点已存在，更新内存和数据库记录
func (h *Hub) AddStation(s *simulator.SimStation) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, exists := h.stations[s.ID]

	// 注入状态变更回调，使 station 内部状态变更能通知前端
	s.OnStateChange = func(stationID string, oldState, newState string, deviceID uint32) {
		h.NotifyStationStateChange(stationID, oldState, newState, deviceID)
	}
	// 注入消息接收回调，使 station 收到应答时记录消息日志
	s.OnMessageReceived = func(stationID string, cmdID uint32, body string, latencyMs int) {
		h.NotifyMessageReceived(stationID, cmdID, body, latencyMs)
	}

	h.stations[s.ID] = s

	// 持久化到数据库（UPSERT）
	if h.db != nil {
		configJSON := "{}"
		if cfg, err := json.Marshal(map[string]interface{}{
			"sasocHost":         s.SasocHost,
			"sasocPort":         s.SasocPort,
			"heartbeatEnabled":  s.HeartbeatEnabled,
			"heartbeatInterval": s.HeartbeatInterval,
			"encryptEnabled":    s.EncryptEnabled,
			"compressEnabled":   s.CompressEnabled,
		}); err == nil {
			configJSON = string(cfg)
		}

		row := &model.SimStationRow{
			ID:       s.ID,
			SN:       s.SN,
			Model:    s.Model,
			Version:  s.Version,
			IP:       s.IP,
			MAC:      s.MAC,
			Name:     s.Name,
			DeviceID: int(s.DeviceID),
			Status:   string(s.State),
			Config:   configJSON,
		}
		if err := model.InsertStation(h.db, row); err != nil {
			h.logger.Warn("failed to persist station to DB",
				zap.String("stationID", s.ID),
				zap.Error(err),
			)
		}
	}

	if exists {
		h.logger.Info("station updated", zap.String("stationID", s.ID))
	} else {
		h.logger.Info("station added", zap.String("stationID", s.ID))
	}
	return nil
}

// RemoveStation 移除安检站（内存 + 数据库）
func (h *Hub) RemoveStation(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, exists := h.stations[id]
	if !exists {
		return fmt.Errorf("station %s not found", id)
	}

	// 停止运行中的站点
	if s.GetState() != simulator.StateIdle {
		if err := s.Stop(); err != nil {
			h.logger.Warn("failed to stop station on remove",
				zap.String("stationID", id),
				zap.Error(err),
			)
		}
	}

	delete(h.stations, id)

	// 从数据库删除
	if h.db != nil {
		if err := model.DeleteStation(h.db, id); err != nil {
			h.logger.Warn("failed to delete station from DB",
				zap.String("stationID", id),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("station removed", zap.String("stationID", id))
	return nil
}

// GetStation 获取安检站
func (h *Hub) GetStation(id string) (*simulator.SimStation, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.stations[id]
	return s, ok
}

// GetStationBySN 通过 SN 获取安检站
// 当 ID != SN 时，需要遍历匹配 SN 字段
func (h *Hub) GetStationBySN(sn string) (*simulator.SimStation, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, s := range h.stations {
		if s.SN == sn {
			return s, true
		}
	}
	return nil, false
}

// ListStations 列出所有安检站
func (h *Hub) ListStations() []*simulator.SimStation {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*simulator.SimStation, 0, len(h.stations))
	for _, s := range h.stations {
		result = append(result, s)
	}
	return result
}

// ListStationsByStatus 按状态列出安检站
func (h *Hub) ListStationsByStatus(status simulator.StationState) []*simulator.SimStation {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*simulator.SimStation, 0)
	for _, s := range h.stations {
		if s.GetState() == status {
			result = append(result, s)
		}
	}
	return result
}

// StationCount 返回安检站数量
func (h *Hub) StationCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.stations)
}

// btoi bool转int
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- 管控柜故障注入 ---

// InjectCabinetSlotFault 对指定站点的指定插槽注入/恢复故障
func (h *Hub) InjectCabinetSlotFault(stationID string, doorNo int, fault bool, reason string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, ok := h.stations[stationID]
	if !ok {
		return fmt.Errorf("station %s not found", stationID)
	}

	if !s.SetCabinetSlotFault(doorNo, fault, reason) {
		return fmt.Errorf("invalid doorNo %d for station %s", doorNo, stationID)
	}

	// 持久化到数据库
	if h.db != nil {
		status := 4 // 故障
		if !fault {
			status = 1 // 关闭
		}
		if err := model.UpsertCabinetSlotStatus(h.db, &model.CabinetSlotStatus{
			StationID: stationID,
			DoorNo:    doorNo,
			Status:    status,
			Reason:    reason,
		}); err != nil {
			h.logger.Warn("failed to persist cabinet slot status",
				zap.String("stationID", stationID),
				zap.Int("doorNo", doorNo),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("cabinet slot fault injected",
		zap.String("stationID", stationID),
		zap.Int("doorNo", doorNo),
		zap.Bool("fault", fault),
		zap.String("reason", reason),
	)
	return nil
}

// InjectCabinetAllSlotsFault 对指定站点的全部插槽注入/恢复故障
func (h *Hub) InjectCabinetAllSlotsFault(stationID string, fault bool, reason string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, ok := h.stations[stationID]
	if !ok {
		return fmt.Errorf("station %s not found", stationID)
	}

	if !s.SetAllCabinetSlotsFault(fault, reason) {
		return fmt.Errorf("station %s has no cabinet", stationID)
	}

	// 持久化到数据库
	if h.db != nil {
		status := 4
		if !fault {
			status = 1
		}
		for i := 1; i <= s.GetCabinet().TotalPorts; i++ {
			if err := model.UpsertCabinetSlotStatus(h.db, &model.CabinetSlotStatus{
				StationID: stationID,
				DoorNo:    i,
				Status:    status,
				Reason:    reason,
			}); err != nil {
				h.logger.Warn("failed to persist cabinet slot status",
					zap.String("stationID", stationID),
					zap.Int("doorNo", i),
					zap.Error(err),
				)
			}
		}
	}

	h.logger.Info("cabinet all slots fault injected",
		zap.String("stationID", stationID),
		zap.Bool("fault", fault),
		zap.String("reason", reason),
	)
	return nil
}

// RestoreCabinetSlotStatuses 从数据库恢复管控柜插槽故障状态（启动时调用）
func (h *Hub) RestoreCabinetSlotStatuses() error {
	if h.db == nil {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, s := range h.stations {
		statuses, err := model.ListCabinetSlotStatusesByStation(h.db, s.ID)
		if err != nil {
			h.logger.Warn("failed to list cabinet slot statuses",
				zap.String("stationID", s.ID),
				zap.Error(err),
			)
			continue
		}
		for _, st := range statuses {
			if st.Status == 4 {
				s.SetCabinetSlotFault(st.DoorNo, true, st.Reason)
			}
		}
	}

	h.logger.Info("cabinet slot statuses restored")
	return nil
}

// AddUsbDevice 添加 U 盘设备（内存 + 数据库）
func (h *Hub) AddUsbDevice(u *simulator.UsbDevice) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.usbDevs[u.ID]; exists {
		return fmt.Errorf("usb device %s already exists", u.ID)
	}

	h.usbDevs[u.ID] = u

	// 持久化到数据库
	if h.db != nil {
		row := &model.SimUsbRow{
			ID:              u.ID,
			SN:              u.SN,
			Model:           u.Model,
			FirmwareVersion: u.FirmwareVersion,
			Qualified:       btoi(u.Qualified),
			AreaName:        u.AreaName,
			ClaimInfo:       "{}",
			Inserted:        btoi(u.Inserted),
			StationID:       u.StationID,
			DoorNo:          u.DoorNo,
			WriteDelay:      u.WriteDelay,
			ReadDelay:       u.ReadDelay,
			WriteFail:       btoi(u.WriteFail),
			ReadFail:        btoi(u.ReadFail),
		}
		if err := model.InsertUsb(h.db, row); err != nil {
			h.logger.Warn("failed to persist usb to DB",
				zap.String("usbID", u.ID),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("usb device added", zap.String("usbID", u.ID))
	return nil
}

// RemoveUsbDevice 移除 U 盘设备（内存 + 数据库）
func (h *Hub) RemoveUsbDevice(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.usbDevs[id]; !exists {
		return fmt.Errorf("usb device %s not found", id)
	}

	delete(h.usbDevs, id)

	// 从数据库删除
	if h.db != nil {
		if err := model.DeleteUsb(h.db, id); err != nil {
			h.logger.Warn("failed to delete usb from DB",
				zap.String("usbID", id),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("usb device removed", zap.String("usbID", id))
	return nil
}

// RestoreUsbs 从数据库恢复 U 盘设备到内存
func (h *Hub) RestoreUsbs() error {
	if h.db == nil {
		return nil
	}
	rows, err := model.ListUsbs(h.db)
	if err != nil {
		return fmt.Errorf("list usbs from DB: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, row := range rows {
		usb := simulator.NewUsbDevice(row.ID, row.SN, row.Model, row.FirmwareVersion, row.AreaName)
		usb.Qualified = row.Qualified != 0
		usb.Inserted = row.Inserted != 0
		usb.StationID = row.StationID
		usb.DoorNo = row.DoorNo
		usb.WriteDelay = row.WriteDelay
		usb.ReadDelay = row.ReadDelay
		usb.WriteFail = row.WriteFail != 0
		usb.ReadFail = row.ReadFail != 0
		h.usbDevs[row.ID] = usb
	}

	h.logger.Info("usbs restored", zap.Int("count", len(rows)))
	return nil
}

// GetUsbDevice 获取 U 盘设备
func (h *Hub) GetUsbDevice(id string) (*simulator.UsbDevice, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	u, ok := h.usbDevs[id]
	return u, ok
}

// ListUsbDevices 列出所有 U 盘设备
func (h *Hub) ListUsbDevices() []*simulator.UsbDevice {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*simulator.UsbDevice, 0, len(h.usbDevs))
	for _, u := range h.usbDevs {
		result = append(result, u)
	}
	return result
}

// ListUsbDevicesByStation 列出属于指定安检站的 U 盘设备
func (h *Hub) ListUsbDevicesByStation(stationID string) []*simulator.UsbDevice {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*simulator.UsbDevice, 0)
	for _, u := range h.usbDevs {
		if u.StationID == stationID {
			result = append(result, u)
		}
	}
	return result
}

// UsbDeviceCount 返回 U 盘设备数量
func (h *Hub) UsbDeviceCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.usbDevs)
}

// --- 生命周期 ---

// ShutdownAll 优雅停止所有站点
func (h *Hub) ShutdownAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Info("shutting down all stations", zap.Int("count", len(h.stations)))

	for id, s := range h.stations {
		if s.GetState() != simulator.StateIdle {
			if err := s.Stop(); err != nil {
				h.logger.Warn("failed to stop station during shutdown",
					zap.String("stationID", id),
					zap.Error(err),
				)
			}
		}
	}

	h.logger.Info("all stations stopped")
}

// ReconnectAllStations 通知所有安检站使用新的SASOC地址重连
func (h *Hub) ReconnectAllStations(newHost string, newPort int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Info("reconnecting all stations to new SASOC address",
		zap.String("host", newHost),
		zap.Int("port", newPort),
	)

	for _, s := range h.stations {
		if s.GetState() != simulator.StateIdle {
			_ = s.Stop()
			s.SasocHost = newHost
			s.SasocPort = newPort
			s.ReinitWithNewAddr(newHost, newPort)
			go s.Start()
		} else {
			s.SasocHost = newHost
			s.SasocPort = newPort
		}
	}
}

// --- 状态变更通知 ---

// NotifyStationStateChange 发布安检站状态变更事件
func (h *Hub) NotifyStationStateChange(stationID, oldState, newState string, deviceID uint32) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(EventStationStateChanged, StationStatePayload{
		StationID: stationID,
		OldState:  oldState,
		NewState:  newState,
		DeviceID:  deviceID,
	})

	// 同步更新数据库状态
	if h.db != nil {
		if err := model.UpdateStationStatus(h.db, stationID, newState); err != nil {
			h.logger.Warn("failed to update station status in DB",
				zap.String("stationID", stationID),
				zap.Error(err),
			)
		}
	}
}

// NotifyMessageSent 发布消息发送事件
func (h *Hub) NotifyMessageSent(stationID string, cmdID uint32, body string, latencyMs int) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(EventMessageSent, MessagePayload{
		StationID: stationID,
		CmdID:     cmdID,
		Direction: "send",
		Body:      body,
		LatencyMs: latencyMs,
	})

	// 写入消息日志
	if h.db != nil {
		_, _ = model.InsertMessageLog(h.db, &model.MessageLogRow{
			StationID:   stationID,
			Direction:   "send",
			CmdID:       int(cmdID),
			RequestBody: body,
			LatencyMs:   latencyMs,
		})
	}
}

// NotifyMessageReceived 发布消息接收事件
func (h *Hub) NotifyMessageReceived(stationID string, cmdID uint32, body string, latencyMs int) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(EventMessageReceived, MessagePayload{
		StationID: stationID,
		CmdID:     cmdID,
		Direction: "recv",
		Body:      body,
		LatencyMs: latencyMs,
	})

	// 写入消息日志
	if h.db != nil {
		_, _ = model.InsertMessageLog(h.db, &model.MessageLogRow{
			StationID:    stationID,
			Direction:    "recv",
			CmdID:        int(cmdID),
			ResponseBody: body,
			LatencyMs:    latencyMs,
		})
	}
}

// NotifyPressureMetric 发布压测指标事件
func (h *Hub) NotifyPressureMetric(payload PressureMetricPayload) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(EventPressureMetric, payload)
}

// NotifyUsbInserted 发布U盘插入事件
func (h *Hub) NotifyUsbInserted(usbID, stationID string, doorNo int) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(EventUsbInserted, UsbEventPayload{
		UsbID:     usbID,
		StationID: stationID,
		DoorNo:    doorNo,
	})
}

// NotifyUsbRemoved 发布U盘拔出事件
func (h *Hub) NotifyUsbRemoved(usbID, stationID string, doorNo int) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(EventUsbRemoved, UsbEventPayload{
		UsbID:     usbID,
		StationID: stationID,
		DoorNo:    doorNo,
	})
}
