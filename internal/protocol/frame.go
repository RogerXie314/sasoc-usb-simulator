package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// EncodeOptions 帧编码选项
type EncodeOptions struct {
	Encrypt     bool   // 是否加密
	Compress    bool   // 是否压缩
	RandomValue uint16 // 随机值（加密时用于密钥派生）
	SerialNo    uint32 // 序列号
}

// DefaultEncodeOptions 默认编码选项（明文 + 不压缩）
func DefaultEncodeOptions() EncodeOptions {
	return EncodeOptions{
		Encrypt:     false,
		Compress:    false,
		RandomValue: 0,
		SerialNo:    0,
	}
}

// Frame 完整帧（包头 + 包体）
type Frame struct {
	Header *Header
	Body   []byte // 原始包体（可能加密/压缩）
}

// DecodeJSONBody 解码包体为 JSON
// 处理流程（与主机卫士 CProtocal::ParsePortocal 一致）：
//   - 解密（AES-256-ECB）→ 去填充 → 解压（ZLib）→ CRC32校验 → JSON解析
func (f *Frame) DecodeJSONBody(v interface{}) error {
	payload := f.Body

	// 1. AES-256-ECB 解密
	if f.Header.IsEncrypted() {
		key := DeriveKey(f.Header.RandomValue)
		decrypted, err := Aes256Decrypt(payload, key, f.Header.FillLen)
		if err != nil {
			return err
		}
		payload = decrypted
	}

	// 2. ZLib 解压
	if f.Header.IsCompressed() {
		decompressed, err := ZLibDecompress(payload)
		if err != nil {
			return err
		}
		payload = decompressed
	}

	// 3. CRC32 校验（校验解密解压后的原始数据）
	if f.Header.CheckSum != 0 {
		if !VerifyChecksum(payload, f.Header.CheckSum) {
			return ErrChecksumFailed
		}
	}

	// 4. JSON 解析
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	return nil
}

// BodyJSON 返回包体的 JSON 字符串（自动解密解压+CRC32校验）
func (f *Frame) BodyJSON() (string, error) {
	payload := f.Body

	if f.Header.IsEncrypted() {
		key := DeriveKey(f.Header.RandomValue)
		decrypted, err := Aes256Decrypt(payload, key, f.Header.FillLen)
		if err != nil {
			return "", err
		}
		payload = decrypted
	}

	if f.Header.IsCompressed() {
		decompressed, err := ZLibDecompress(payload)
		if err != nil {
			return "", err
		}
		payload = decompressed
	}

	// CRC32 校验
	if f.Header.CheckSum != 0 {
		if !VerifyChecksum(payload, f.Header.CheckSum) {
			return "", ErrChecksumFailed
		}
	}

	return string(payload), nil
}

// RawPayload 返回解密解压后的原始数据（用于非JSON场景）
func (f *Frame) RawPayload() ([]byte, error) {
	payload := f.Body

	if f.Header.IsEncrypted() {
		key := DeriveKey(f.Header.RandomValue)
		decrypted, err := Aes256Decrypt(payload, key, f.Header.FillLen)
		if err != nil {
			return nil, err
		}
		payload = decrypted
	}

	if f.Header.IsCompressed() {
		decompressed, err := ZLibDecompress(payload)
		if err != nil {
			return nil, err
		}
		payload = decompressed
	}

	return payload, nil
}

// EncodeFrame 完整编码流程（与主机卫士 CProtocal::GetPortocal 一致）：
//
//	JSON → CRC32(原始) → 压缩 → 加密 → 填充包头 → 拼帧
//
// C++ 源码（WLProtocal/Protocal.cpp 第767-912行）：
//  1. GetCheckSum(pSrcData, nSrcLen, crc)  — CRC32 校验原始数据
//  2. Compress()                            — ZLib 压缩
//  3. Encrypt()                             — AES-256-ECB 加密
//  4. FillPortocalHead()                    — 填充包头
//  5. 拼接包头 + 包体
func EncodeFrame(cmdID uint32, devID uint32, body interface{}, opts EncodeOptions) ([]byte, error) {
	// 1. JSON 序列化
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}

	payload := jsonBytes

	// 2. CRC32 校验原始数据（与 C++ GetCheckSum(pSrcData, nSrcLen) 一致）
	checksum := ComputeChecksum(payload)

	// 3. ZLib 压缩
	if opts.Compress {
		compressed, err := ZLibCompress(payload)
		if err != nil {
			return nil, err
		}
		payload = compressed
	}

	// 4. AES-256-ECB 加密（含零填充）
	var fillLen uint8
	if opts.Encrypt {
		key := DeriveKey(opts.RandomValue)
		encrypted, fl, err := Aes256Encrypt(payload, key)
		if err != nil {
			return nil, err
		}
		payload = encrypted
		fillLen = fl
	}

	// 5. 构造包头
	header := NewHeader(cmdID, devID, uint16(len(payload)))
	header.DecFlag = boolToFlag(opts.Encrypt)
	header.ZipFlag = boolToFlag(opts.Compress)
	header.FillLen = fillLen
	header.RandomValue = opts.RandomValue
	header.SerialNo = opts.SerialNo
	header.CheckSum = checksum     // CRC32 原始数据校验值
	header.SrcLen = uint16(len(jsonBytes)) // 原始数据长度

	// 6. 编码包头并拼接帧
	headerBytes, _ := header.Encode()

	frame := make([]byte, 0, HeaderSize+len(payload))
	frame = append(frame, headerBytes...)
	frame = append(frame, payload...)

	return frame, nil
}

// DecodeFrame 从字节数组解码完整帧
// 仅解析包头+提取包体，不做CRC32校验（CRC32校验在DecodeJSONBody中完成）
// data 至少包含 48 字节包头
func DecodeFrame(data []byte) (*Frame, error) {
	if len(data) < HeaderSize {
		return nil, ErrFrameTooShort
	}

	// 1. 解析包头
	header, err := DecodeHeader(data[:HeaderSize])
	if err != nil {
		return nil, err
	}

	// 2. 校验包头
	if err := header.Validate(); err != nil {
		return nil, err
	}

	// 3. 检查包体长度
	expectedLen := HeaderSize + int(header.BodyLen)
	if len(data) < expectedLen {
		return nil, ErrBodyTruncated
	}

	// 4. 提取包体
	body := data[HeaderSize:expectedLen]

	// 注意：CRC32 校验移至 DecodeJSONBody 中执行
	// 因为 CRC32 校验的是解密解压后的原始数据，而非包头+加密包体

	return &Frame{
		Header: header,
		Body:   body,
	}, nil
}

// ReadFrame 从 bufio.Reader 读取一帧
// 先读 48 字节包头，再按 bodyLen 读取包体
func ReadFrame(r *bytes.Reader) (*Frame, error) {
	// 1. 读取包头
	headerBuf := make([]byte, HeaderSize)
	if _, err := r.Read(headerBuf); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	header, err := DecodeHeader(headerBuf)
	if err != nil {
		return nil, err
	}

	if err := header.Validate(); err != nil {
		return nil, err
	}

	// 2. 读取包体
	bodyBuf := make([]byte, header.BodyLen)
	if _, err := r.Read(bodyBuf); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return &Frame{
		Header: header,
		Body:   bodyBuf,
	}, nil
}

// boolToFlag 布尔转标志位
func boolToFlag(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// TotalFrameLen 计算帧总长度
func TotalFrameLen(bodyLen uint16) int {
	return HeaderSize + int(bodyLen)
}

// ReadFrameFromTCP 从 TCP 连接读取一帧（使用给定的读取函数）
// readFn 应当保证读取恰好 n 字节
func ReadFrameFromTCP(readFn func([]byte) (int, error)) (*Frame, error) {
	// 1. 读取包头
	headerBuf := make([]byte, HeaderSize)
	if _, err := readFn(headerBuf); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	header, err := DecodeHeader(headerBuf)
	if err != nil {
		return nil, err
	}

	if err := header.Validate(); err != nil {
		return nil, err
	}

	// 2. 读取包体
	if header.BodyLen == 0 {
		// 无包体（如心跳应答）
		return &Frame{
			Header: header,
			Body:   []byte{},
		}, nil
	}

	bodyBuf := make([]byte, header.BodyLen)
	if _, err := readFn(bodyBuf); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return &Frame{
		Header: header,
		Body:   bodyBuf,
	}, nil
}

// EncodeFrameRaw 从原始包体字节数据编码帧（跳过 JSON 序列化）
func EncodeFrameRaw(cmdID uint32, devID uint32, rawPayload []byte, opts EncodeOptions) ([]byte, error) {
	// 1. CRC32 校验原始数据
	checksum := ComputeChecksum(rawPayload)
	srcLen := uint16(len(rawPayload))

	payload := rawPayload

	// 2. ZLib 压缩
	if opts.Compress {
		compressed, err := ZLibCompress(payload)
		if err != nil {
			return nil, err
		}
		payload = compressed
	}

	// 3. AES-256-ECB 加密
	var fillLen uint8
	if opts.Encrypt {
		key := DeriveKey(opts.RandomValue)
		encrypted, fl, err := Aes256Encrypt(payload, key)
		if err != nil {
			return nil, err
		}
		payload = encrypted
		fillLen = fl
	}

	// 4. 构造包头
	header := NewHeader(cmdID, devID, uint16(len(payload)))
	header.DecFlag = boolToFlag(opts.Encrypt)
	header.ZipFlag = boolToFlag(opts.Compress)
	header.FillLen = fillLen
	header.RandomValue = opts.RandomValue
	header.SerialNo = opts.SerialNo
	header.CheckSum = checksum
	header.SrcLen = srcLen

	// 5. 编码包头并拼接帧
	headerBytes, _ := header.Encode()

	frame := make([]byte, 0, HeaderSize+len(payload))
	frame = append(frame, headerBytes...)
	frame = append(frame, payload...)

	return frame, nil
}

// ensure binary package is used (for future TCP reader implementations)
var _ = binary.BigEndian
