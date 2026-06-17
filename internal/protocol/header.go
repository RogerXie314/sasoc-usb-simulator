package protocol

import "encoding/binary"

// 协议常量
const (
	HeaderSize    = 48              // 包头固定 48 字节
	HeadFlagHi    = 0x50            // 头标记高字节 'P'
	HeadFlagLo    = 0x54            // 头标记低字节 'T'
	SrcTypeStation = 7              // 数据源类型：移动介质安检站
	Version1      = 1               // 协议版本号
	MaxBodyLen    = 65535           // bodyLen 为 uint16，最大值
)

// CMDID 命令ID常量
const (
	CmdHeartbeat      uint32 = 100  // 心跳
	CmdInfoReport     uint32 = 101  // 信息上报
	CmdRegister       uint32 = 102  // 设备注册
	CmdClaimVerify    uint32 = 103  // 申领码验证
	CmdUsbClaim       uint32 = 104  // U盘领取上报
	CmdUsbReturn      uint32 = 105  // U盘归还上报
	CmdAlarm          uint32 = 106  // 告警上报
	CmdOperationLog   uint32 = 107  // 操作日志上报
	CmdUpgradeIssue   uint32 = 108  // 病毒库升级下发
	CmdUpgradeResult  uint32 = 109  // 升级结果上报
)

// 错误码常量
const (
	CodeSuccess         = 0     // 成功
	CodeFail            = 1     // 通用失败
	CodeNotRegistered   = 1000  // 设备未注册
	CodeOverCapacity    = 1001  // 超过容量上限
	CodeClaimNotExist   = 2001  // 申领码不存在或已失效
	CodeUsbNotCollected = 2002  // U盘未收录
	CodeUsbScrapped     = 2003  // U盘已报废
	CodeClaimNotAvailable = 2005 // 申领码状态不可领取
	CodeOutOfTimeRange  = 2006  // 不在使用时间范围
	CodeTaskExclusive   = 3001  // 任务互斥
)

// Header 48 字节包头（大端序）
// 严格按照协议文档偏移定义，使用固定大小类型确保跨平台一致性
type Header struct {
	// 偏移 0-1: 头标记，固定 0x5054 ('P','T')
	HeadFlag [2]byte
	// 偏移 2: 协议版本号
	Version uint8
	// 偏移 3: 数据源类型（安检站=7）
	SrcType uint8
	// 偏移 4-5: 包体长度（无符号 short）
	BodyLen uint16
	// 偏移 6: 加密标志（0=明文，>0=AES256加密）
	DecFlag uint8
	// 偏移 7: 压缩标志（0=未压缩，>0=ZLib压缩）
	ZipFlag uint8
	// 偏移 8: 填充长度（AES 加密后补齐的字节数）
	FillLen uint8
	// 偏移 9: 预留标志
	PreFlag uint8
	// 偏移 10-11: 随机值（用于密钥派生）
	RandomValue uint16
	// 偏移 12-15: 序列号
	SerialNo uint32
	// 偏移 16-19: CRC32 校验（校验范围：包体 + 偏移16之前的包头）
	CheckSum uint32
	// 偏移 20-23: 会话ID
	Sid uint32
	// 偏移 24-31: 时间戳（毫秒）
	TimeFlag uint64
	// 偏移 32-35: 命令ID
	CmdID uint32
	// 偏移 36-39: 设备ID（注册成功后由服务端分配）
	DevID uint32
	// 偏移 40-41: 源长度
	SrcLen uint16
	// 偏移 42-47: 预留字段
	LastValue [6]byte
}

// Encode 将 Header 序列化为 48 字节大端二进制
func (h *Header) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize)

	// 偏移 0-1: HeadFlag
	buf[0] = h.HeadFlag[0]
	buf[1] = h.HeadFlag[1]
	// 偏移 2: Version
	buf[2] = h.Version
	// 偏移 3: SrcType
	buf[3] = h.SrcType
	// 偏移 4-5: BodyLen
	binary.BigEndian.PutUint16(buf[4:6], h.BodyLen)
	// 偏移 6: DecFlag
	buf[6] = h.DecFlag
	// 偏移 7: ZipFlag
	buf[7] = h.ZipFlag
	// 偏移 8: FillLen
	buf[8] = h.FillLen
	// 偏移 9: PreFlag
	buf[9] = h.PreFlag
	// 偏移 10-11: RandomValue
	binary.BigEndian.PutUint16(buf[10:12], h.RandomValue)
	// 偏移 12-15: SerialNo
	binary.BigEndian.PutUint32(buf[12:16], h.SerialNo)
	// 偏移 16-19: CheckSum
	binary.BigEndian.PutUint32(buf[16:20], h.CheckSum)
	// 偏移 20-23: Sid
	binary.BigEndian.PutUint32(buf[20:24], h.Sid)
	// 偏移 24-31: TimeFlag
	binary.BigEndian.PutUint64(buf[24:32], h.TimeFlag)
	// 偏移 32-35: CmdID
	binary.BigEndian.PutUint32(buf[32:36], h.CmdID)
	// 偏移 36-39: DevID
	binary.BigEndian.PutUint32(buf[36:40], h.DevID)
	// 偏移 40-41: SrcLen
	binary.BigEndian.PutUint16(buf[40:42], h.SrcLen)
	// 偏移 42-47: LastValue
	copy(buf[42:48], h.LastValue[:])

	return buf, nil
}

// DecodeHeader 从 48 字节大端二进制解析 Header
func DecodeHeader(data []byte) (*Header, error) {
	if len(data) < HeaderSize {
		return nil, ErrHeaderTooShort
	}

	h := &Header{}
	h.HeadFlag[0] = data[0]
	h.HeadFlag[1] = data[1]
	h.Version = data[2]
	h.SrcType = data[3]
	h.BodyLen = binary.BigEndian.Uint16(data[4:6])
	h.DecFlag = data[6]
	h.ZipFlag = data[7]
	h.FillLen = data[8]
	h.PreFlag = data[9]
	h.RandomValue = binary.BigEndian.Uint16(data[10:12])
	h.SerialNo = binary.BigEndian.Uint32(data[12:16])
	h.CheckSum = binary.BigEndian.Uint32(data[16:20])
	h.Sid = binary.BigEndian.Uint32(data[20:24])
	h.TimeFlag = binary.BigEndian.Uint64(data[24:32])
	h.CmdID = binary.BigEndian.Uint32(data[32:36])
	h.DevID = binary.BigEndian.Uint32(data[36:40])
	h.SrcLen = binary.BigEndian.Uint16(data[40:42])
	copy(h.LastValue[:], data[42:48])

	return h, nil
}

// Validate 校验包头基本合法性
func (h *Header) Validate() error {
	if h.HeadFlag[0] != HeadFlagHi || h.HeadFlag[1] != HeadFlagLo {
		return ErrInvalidHeadFlag
	}
	if h.BodyLen > MaxBodyLen {
		return ErrBodyLenTooLarge
	}
	return nil
}

// IsEncrypted 是否加密
func (h *Header) IsEncrypted() bool {
	return h.DecFlag > 0
}

// IsCompressed 是否压缩
func (h *Header) IsCompressed() bool {
	return h.ZipFlag > 0
}

// NewHeader 创建默认包头
func NewHeader(cmdID uint32, devID uint32, bodyLen uint16) *Header {
	now := currentMillis()
	return &Header{
		HeadFlag:    [2]byte{HeadFlagHi, HeadFlagLo},
		Version:     Version1,
		SrcType:     SrcTypeStation,
		BodyLen:     bodyLen,
		DecFlag:     0,
		ZipFlag:     0,
		FillLen:     0,
		PreFlag:     0,
		RandomValue: 0,
		SerialNo:    uint32(now & 0xFFFFFFFF), // 使用时间戳低32位作为序列号
		CheckSum:    0,
		Sid:         0,
		TimeFlag:    uint64(now),
		CmdID:       cmdID,
		DevID:       devID,
		SrcLen:      0,
		LastValue:   [6]byte{},
	}
}
