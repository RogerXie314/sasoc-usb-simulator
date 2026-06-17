package simulator

// Cabinet 管控柜
type Cabinet struct {
	TotalPorts int           `json:"totalPorts"`
	UsedPorts  int           `json:"usedPorts"`
	DoorStatus []int         `json:"doorStatus"` // 0-4
	Slots      []CabinetSlot `json:"slots"`
}

// CabinetSlot 柜内插槽
type CabinetSlot struct {
	DoorNo   int    `json:"doorNo"`
	SN       string `json:"sn,omitempty"` // 对齐协议 §7.3 slots[].sn
	Occupied bool   `json:"occupied"`
}

// NewCabinet 创建管控柜
func NewCabinet(totalPorts int) *Cabinet {
	c := &Cabinet{
		TotalPorts: totalPorts,
		UsedPorts:  0,
		DoorStatus: make([]int, totalPorts),
		Slots:      make([]CabinetSlot, totalPorts),
	}
	// 默认所有柜门关闭
	for i := 0; i < totalPorts; i++ {
		c.DoorStatus[i] = 1 // 关闭
		c.Slots[i] = CabinetSlot{DoorNo: i + 1, Occupied: false}
	}
	return c
}

// PutUsb 向柜内放入 U 盘
func (c *Cabinet) PutUsb(doorNo int, usbSN string) bool {
	idx := doorNo - 1
	if idx < 0 || idx >= c.TotalPorts {
		return false
	}
	if c.Slots[idx].Occupied {
		return false
	}
	c.Slots[idx].SN = usbSN
	c.Slots[idx].Occupied = true
	c.UsedPorts++
	return true
}

// RemoveUsb 从柜内取出 U 盘
func (c *Cabinet) RemoveUsb(doorNo int) (string, bool) {
	idx := doorNo - 1
	if idx < 0 || idx >= c.TotalPorts {
		return "", false
	}
	if !c.Slots[idx].Occupied {
		return "", false
	}
	sn := c.Slots[idx].SN
	c.Slots[idx].SN = ""
	c.Slots[idx].Occupied = false
	c.UsedPorts--
	return sn, true
}

// GetUsbList 获取柜内所有 U 盘列表
func (c *Cabinet) GetUsbList() []CabinetSlot {
	result := make([]CabinetSlot, 0)
	for _, slot := range c.Slots {
		if slot.Occupied {
			result = append(result, slot)
		}
	}
	return result
}

// ToReportMap 转换为信息上报格式（对齐协议 §7.3 cabinet）
func (c *Cabinet) ToReportMap() map[string]interface{} {
	slots := make([]map[string]interface{}, 0)
	for _, slot := range c.Slots {
		if slot.Occupied {
			slots = append(slots, map[string]interface{}{
				"doorNo": slot.DoorNo,
				"sn":     slot.SN,
			})
		}
	}

	return map[string]interface{}{
		"totalPorts": c.TotalPorts,
		"usedPorts":  c.UsedPorts,
		"slots":      slots,
	}
}
