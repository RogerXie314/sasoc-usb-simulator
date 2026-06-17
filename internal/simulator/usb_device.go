package simulator

import (
	"fmt"
	"sync"
	"time"
)

// UsbStatus U盘生命周期状态（对齐场景文档 §3.1 U盘生命周期状态机）
type UsbStatus string

const (
	UsbStatusRegistered UsbStatus = "registered"  // 已录入：集团录入
	UsbStatusSent       UsbStatus = "sent"        // 已寄出：写入电站标识+合格标记
	UsbStatusReceived   UsbStatus = "received"    // 已收录：电站匹配收录
	UsbStatusIssued     UsbStatus = "issued"      // 已发放：申领+标记写入
	UsbStatusScrapped   UsbStatus = "scrapped"    // 已报废：损坏、遗失
)

// UsbStatusCN 中文状态映射
var UsbStatusCN = map[UsbStatus]string{
	UsbStatusRegistered: "已录入",
	UsbStatusSent:       "已寄出",
	UsbStatusReceived:   "已收录",
	UsbStatusIssued:     "已发放",
	UsbStatusScrapped:   "已报废",
}

// CN 中文显示
func (s UsbStatus) CN() string {
	if cn, ok := UsbStatusCN[s]; ok {
		return cn
	}
	return string(s)
}

// UsbDevice 模拟 U 盘设备
type UsbDevice struct {
	ID              string     `json:"usbId"`
	SN              string     `json:"sn"`
	Model           string     `json:"model"`
	FirmwareVersion string     `json:"firmwareVersion"`
	Qualified       bool       `json:"qualified"`
	AreaName        string     `json:"areaName"`
	Status          UsbStatus  `json:"status"`                     // U盘生命周期状态
	ClaimInfo       *ClaimInfo `json:"claimInfo"`
	VirusFreeMark   bool       `json:"virusFreeMark"`
	Inserted        bool       `json:"inserted"`
	WriteDelay      int        `json:"writeDelay"` // ms，故障注入用
	ReadDelay       int        `json:"readDelay"`  // ms，故障注入用
	WriteFail       bool       `json:"writeFail"`  // 模拟写入失败
	ReadFail        bool       `json:"readFail"`   // 模拟读取失败
	StationID       string     `json:"stationId,omitempty"` // 所属安检站
	DoorNo          int        `json:"doorNo,omitempty"`    // 柜门号
}

// ClaimInfo 申领信息
type ClaimInfo struct {
	ApplicantName string   `json:"applicantName"`
	ApplicantNo   string   `json:"applicantNo"`
	StartTime     *int64   `json:"startTime"`
	EndTime       *int64   `json:"endTime"`
	AreaCodes     []string `json:"areaCodes"`
}

// NewUsbDevice 创建模拟 U 盘（默认状态：已收录，厂级场景下U盘已被电站收录）
func NewUsbDevice(id, sn, model, firmwareVersion, areaName string) *UsbDevice {
	return &UsbDevice{
		ID:              id,
		SN:              sn,
		Model:           model,
		FirmwareVersion: firmwareVersion,
		Qualified:       true,
		AreaName:        areaName,
		Status:          UsbStatusReceived,
		VirusFreeMark:   true,
		Inserted:        false,
	}
}

// Insert 模拟插入
func (u *UsbDevice) Insert() {
	u.Inserted = true
}

// Remove 模拟拔出
func (u *UsbDevice) Remove() {
	u.Inserted = false
}

// Write 写入信息（合格标记 + 区域名称）
func (u *UsbDevice) Write(qualified bool, areaName string) error {
	if u.WriteFail {
		return fmt.Errorf("simulated write failure")
	}
	if u.WriteDelay > 0 {
		time.Sleep(time.Duration(u.WriteDelay) * time.Millisecond)
	}
	u.Qualified = qualified
	u.AreaName = areaName
	return nil
}

// Read 读取信息
func (u *UsbDevice) Read() (map[string]interface{}, error) {
	if u.ReadFail {
		return nil, fmt.Errorf("simulated read failure")
	}
	if u.ReadDelay > 0 {
		time.Sleep(time.Duration(u.ReadDelay) * time.Millisecond)
	}
	return map[string]interface{}{
		"sn":              u.SN,
		"model":           u.Model,
		"firmwareVersion": u.FirmwareVersion,
		"qualified":       u.Qualified,
		"areaName":        u.AreaName,
		"virusFreeMark":   u.VirusFreeMark,
		"status":          string(u.Status),
	}, nil
}

// --- U盘生命周期状态流转方法 ---

// SetRegistered 置为已录入（集团录入）
func (u *UsbDevice) SetRegistered() {
	u.Status = UsbStatusRegistered
}

// SetSent 置为已寄出
func (u *UsbDevice) SetSent() {
	u.Status = UsbStatusSent
}

// SetReceived 置为已收录（电站收录）
func (u *UsbDevice) SetReceived() {
	u.Status = UsbStatusReceived
}

// SetIssued 置为已发放（申领成功）
func (u *UsbDevice) SetIssued() {
	u.Status = UsbStatusIssued
	// 发放时清空管控柜关联
	u.StationID = ""
	u.DoorNo = 0
}

// SetScrapped 置为已报废
func (u *UsbDevice) SetScrapped() {
	u.Status = UsbStatusScrapped
}

// CanBeIssued 是否可被发放（只有已收录状态可发放）
func (u *UsbDevice) CanBeIssued() bool {
	return u.Status == UsbStatusReceived
}

// CanBeReturned 是否可被归还（只有已发放状态可归还）
func (u *UsbDevice) CanBeReturned() bool {
	return u.Status == UsbStatusIssued
}

// CanBeScrapped 是否可被报废（已收录/已发放可报废）
func (u *UsbDevice) CanBeScrapped() bool {
	return u.Status == UsbStatusReceived || u.Status == UsbStatusIssued
}

// UsbPluginManager U 盘插件管理器
type UsbPluginManager struct {
	devices map[string]*UsbDevice
	mu      sync.RWMutex
}

// NewUsbPluginManager 创建 U 盘插件管理器
func NewUsbPluginManager() *UsbPluginManager {
	return &UsbPluginManager{
		devices: make(map[string]*UsbDevice),
	}
}

// AddDevice 添加 U 盘
func (m *UsbPluginManager) AddDevice(device *UsbDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[device.ID] = device
}

// RemoveDevice 移除 U 盘
func (m *UsbPluginManager) RemoveDevice(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.devices, id)
}

// GetDevice 获取 U 盘
func (m *UsbPluginManager) GetDevice(id string) (*UsbDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[id]
	return d, ok
}

// ListDevices 列出所有 U 盘
func (m *UsbPluginManager) ListDevices() []*UsbDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*UsbDevice, 0, len(m.devices))
	for _, d := range m.devices {
		result = append(result, d)
	}
	return result
}

// ListInserted 列出所有已插入的 U 盘
func (m *UsbPluginManager) ListInserted() []*UsbDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*UsbDevice, 0)
	for _, d := range m.devices {
		if d.Inserted {
			result = append(result, d)
		}
	}
	return result
}
