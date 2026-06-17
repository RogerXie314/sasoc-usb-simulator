package protocol

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"testing"
)

// ========== Header 测试 ==========

func TestHeaderEncodeDecode(t *testing.T) {
	original := NewHeader(CmdRegister, 10001, 256)
	original.DecFlag = 1
	original.ZipFlag = 1
	original.FillLen = 8
	original.RandomValue = 12345
	original.SerialNo = 99999
	original.CheckSum = 0xDEADBEEF
	original.Sid = 0x11111111

	encoded, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	if len(encoded) != HeaderSize {
		t.Fatalf("encoded length %d != %d", len(encoded), HeaderSize)
	}

	decoded, err := DecodeHeader(encoded)
	if err != nil {
		t.Fatalf("DecodeHeader failed: %v", err)
	}

	// 逐字段比较
	if decoded.HeadFlag[0] != original.HeadFlag[0] || decoded.HeadFlag[1] != original.HeadFlag[1] {
		t.Errorf("HeadFlag mismatch: got %v, want %v", decoded.HeadFlag, original.HeadFlag)
	}
	if decoded.Version != original.Version {
		t.Errorf("Version mismatch: got %d, want %d", decoded.Version, original.Version)
	}
	if decoded.SrcType != original.SrcType {
		t.Errorf("SrcType mismatch: got %d, want %d", decoded.SrcType, original.SrcType)
	}
	if decoded.BodyLen != original.BodyLen {
		t.Errorf("BodyLen mismatch: got %d, want %d", decoded.BodyLen, original.BodyLen)
	}
	if decoded.DecFlag != original.DecFlag {
		t.Errorf("DecFlag mismatch: got %d, want %d", decoded.DecFlag, original.DecFlag)
	}
	if decoded.ZipFlag != original.ZipFlag {
		t.Errorf("ZipFlag mismatch: got %d, want %d", decoded.ZipFlag, original.ZipFlag)
	}
	if decoded.FillLen != original.FillLen {
		t.Errorf("FillLen mismatch: got %d, want %d", decoded.FillLen, original.FillLen)
	}
	if decoded.RandomValue != original.RandomValue {
		t.Errorf("RandomValue mismatch: got %d, want %d", decoded.RandomValue, original.RandomValue)
	}
	if decoded.SerialNo != original.SerialNo {
		t.Errorf("SerialNo mismatch: got %d, want %d", decoded.SerialNo, original.SerialNo)
	}
	if decoded.CheckSum != original.CheckSum {
		t.Errorf("CheckSum mismatch: got 0x%X, want 0x%X", decoded.CheckSum, original.CheckSum)
	}
	if decoded.CmdID != original.CmdID {
		t.Errorf("CmdID mismatch: got %d, want %d", decoded.CmdID, original.CmdID)
	}
	if decoded.DevID != original.DevID {
		t.Errorf("DevID mismatch: got %d, want %d", decoded.DevID, original.DevID)
	}
	if decoded.TimeFlag != original.TimeFlag {
		t.Errorf("TimeFlag mismatch: got %d, want %d", decoded.TimeFlag, original.TimeFlag)
	}
}

func TestHeaderValidation(t *testing.T) {
	tests := []struct {
		name    string
		header  *Header
		wantErr error
	}{
		{
			name:   "valid header",
			header: NewHeader(CmdHeartbeat, 0, 100),
			wantErr: nil,
		},
		{
			name: "invalid head flag",
			header: &Header{
				HeadFlag: [2]byte{0xFF, 0xFF},
				Version:  1,
				SrcType:  7,
				BodyLen:  100,
			},
			wantErr: ErrInvalidHeadFlag,
		},
		{
			name: "body len at max",
			header: &Header{
				HeadFlag: [2]byte{HeadFlagHi, HeadFlagLo},
				BodyLen:  MaxBodyLen,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.header.Validate()
			if err != tt.wantErr {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeHeaderTooShort(t *testing.T) {
	data := make([]byte, 20) // 不足 48 字节
	_, err := DecodeHeader(data)
	if err != ErrHeaderTooShort {
		t.Errorf("expected ErrHeaderTooShort, got %v", err)
	}
}

// ========== 密钥派生测试 ==========

func TestDeriveKey(t *testing.T) {
	// 验证密钥派生的基本属性
	key := DeriveKey(12345)

	if len(key) != 32 {
		t.Fatalf("key length %d != 32", len(key))
	}

	// 验证前 20 字节是 SHA1 摘要，后 12 字节为零
	allZero := true
	for i := 20; i < 32; i++ {
		if key[i] != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Error("key bytes 20-31 should be zero")
	}

	// 验证不同 randomValue 产生不同密钥
	key2 := DeriveKey(54321)
	keysEqual := true
	for i := 0; i < 32; i++ {
		if key[i] != key2[i] {
			keysEqual = false
			break
		}
	}
	if keysEqual {
		t.Error("different randomValue should produce different keys")
	}

	// 验证相同 randomValue 产生相同密钥（确定性）
	key3 := DeriveKey(12345)
	for i := 0; i < 32; i++ {
		if key[i] != key3[i] {
			t.Errorf("same randomValue should produce same key, diff at byte %d", i)
			break
		}
	}
}

func TestDeriveKey_MatchesCpp(t *testing.T) {
	// 验证与 C++ GetEncryptKey 的计算结果一致
	// C++ 逻辑：SHA1("4&k7w4&P588%e5r684k$bj@JB$Jf8bik" + nRandomKey)
	// nRandomKey 通过 ostringstream << unsigned short 输出为十进制字符串

	randomValue := uint16(12345)
	input := fmt.Sprintf("%s%d", "4&k7w4&P588%e5r684k$bj@JB$Jf8bik", randomValue)
	expectedHash := sha1.Sum([]byte(input))

	key := DeriveKey(randomValue)

	// 前 20 字节应与 SHA1 结果完全一致
	for i := 0; i < 20; i++ {
		if key[i] != expectedHash[i] {
			t.Errorf("key[%d] = 0x%02X, expected SHA1[%d] = 0x%02X", i, key[i], i, expectedHash[i])
		}
	}

	// 后 12 字节应为零
	for i := 20; i < 32; i++ {
		if key[i] != 0 {
			t.Errorf("key[%d] = 0x%02X, expected 0", i, key[i])
		}
	}
}

// ========== AES-256-ECB 加解密测试 ==========

func TestAes256EncryptDecrypt(t *testing.T) {
	key, err := GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	plaintext := []byte(`{"sn":"MS20260001","model":"WNT-MS-2000"}`)

	ciphertext, fillLen, err := Aes256Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if len(ciphertext)%16 != 0 {
		t.Errorf("ciphertext not block-aligned, len=%d", len(ciphertext))
	}

	if fillLen == 0 || fillLen > 16 {
		t.Errorf("invalid fillLen: %d", fillLen)
	}

	decrypted, err := Aes256Decrypt(ciphertext, key, fillLen)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted mismatch:\ngot:  %s\nwant: %s", decrypted, plaintext)
	}
}

func TestAes256EncryptDecryptWithDeriveKey(t *testing.T) {
	randomValue := uint16(54321)
	key := DeriveKey(randomValue)

	plaintext := []byte(`{"cmd":"test","data":"hello world"}`)

	ciphertext, fillLen, err := Aes256Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Aes256Decrypt(ciphertext, key, fillLen)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted mismatch:\ngot:  %s\nwant: %s", decrypted, plaintext)
	}
}

func TestAes256EmptyPlaintext(t *testing.T) {
	key, _ := GenerateRandomKey()
	plaintext := []byte{}

	ciphertext, fillLen, err := Aes256Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt empty failed: %v", err)
	}

	decrypted, err := Aes256Decrypt(ciphertext, key, fillLen)
	if err != nil {
		t.Fatalf("Decrypt empty failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("decrypted empty should be empty, got %d bytes", len(decrypted))
	}
}

func TestAes256ZeroPadding(t *testing.T) {
	// 测试零填充：加密后解密，尾部不应有多余的非零字节
	key := DeriveKey(9999)

	// 测试各种长度
	testLens := []int{1, 15, 16, 17, 31, 32, 33, 100, 255, 256}
	for _, n := range testLens {
		t.Run(fmt.Sprintf("len_%d", n), func(t *testing.T) {
			plaintext := make([]byte, n)
			for i := range plaintext {
				plaintext[i] = byte(i % 256)
			}

			ciphertext, fillLen, err := Aes256Encrypt(plaintext, key)
			if err != nil {
				t.Fatalf("Encrypt len=%d failed: %v", n, err)
			}

			if len(ciphertext)%16 != 0 {
				t.Errorf("ciphertext not aligned, len=%d", len(ciphertext))
			}

			expectedFillLen := 16 - (n % 16)
			if int(fillLen) != expectedFillLen {
				t.Errorf("fillLen=%d, expected=%d", fillLen, expectedFillLen)
			}

			decrypted, err := Aes256Decrypt(ciphertext, key, fillLen)
			if err != nil {
				t.Fatalf("Decrypt len=%d failed: %v", n, err)
			}

			if len(decrypted) != n {
				t.Errorf("decrypted len=%d, expected=%d", len(decrypted), n)
			}

			for i := range plaintext {
				if decrypted[i] != plaintext[i] {
					t.Errorf("byte %d mismatch: got 0x%02X, want 0x%02X", i, decrypted[i], plaintext[i])
					break
				}
			}
		})
	}
}

// ========== ZLib 压缩解压测试 ==========

func TestZLibCompressDecompress(t *testing.T) {
	original := []byte(`{"sn":"USB20250001","model":"SK-8GB-A1","firmwareVersion":"FW3.1.0"}`)

	compressed, err := ZLibCompress(original)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// 压缩后应该比原始数据小（对于重复性高的文本）
	if len(compressed) >= len(original) {
		t.Logf("warning: compressed (%d) not smaller than original (%d)", len(compressed), len(original))
	}

	decompressed, err := ZLibDecompress(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("decompressed mismatch:\ngot:  %s\nwant: %s", decompressed, original)
	}
}

// ========== CRC32 校验测试 ==========

func TestChecksumConsistency(t *testing.T) {
	data := []byte(`{"test":true}`)

	sum1 := ComputeChecksum(data)
	sum2 := ComputeChecksum(data)

	if sum1 != sum2 {
		t.Errorf("checksum not deterministic: %X != %X", sum1, sum2)
	}

	// 修改数据后校验值应该不同
	data2 := []byte(`{"test":false}`)
	sum3 := ComputeChecksum(data2)

	if sum1 == sum3 {
		t.Errorf("checksum should differ for different data")
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte(`{"test":true}`)
	sum := ComputeChecksum(data)

	if !VerifyChecksum(data, sum) {
		t.Error("VerifyChecksum should return true for correct checksum")
	}

	if VerifyChecksum(data, sum+1) {
		t.Error("VerifyChecksum should return false for wrong checksum")
	}
}

// ========== 完整帧编解码测试 ==========

func TestEncodeDecodeFrame_PlainText(t *testing.T) {
	body := map[string]interface{}{
		"sn":    "MS20260001",
		"model": "WNT-MS-2000",
		"ip":    "192.168.1.100",
		"mac":   "00:11:22:33:44:55",
	}

	opts := EncodeOptions{
		Encrypt:  false,
		Compress: false,
	}

	frameBytes, err := EncodeFrame(CmdRegister, 0, body, opts)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	// 帧长度 = 48 + bodyLen
	if len(frameBytes) < HeaderSize {
		t.Fatalf("frame too short: %d", len(frameBytes))
	}

	frame, err := DecodeFrame(frameBytes)
	if err != nil {
		t.Fatalf("DecodeFrame failed: %v", err)
	}

	if frame.Header.CmdID != CmdRegister {
		t.Errorf("CmdID mismatch: got %d, want %d", frame.Header.CmdID, CmdRegister)
	}

	if frame.Header.DevID != 0 {
		t.Errorf("DevID mismatch: got %d, want 0", frame.Header.DevID)
	}

	if frame.Header.IsEncrypted() {
		t.Error("should not be encrypted")
	}

	if frame.Header.IsCompressed() {
		t.Error("should not be compressed")
	}

	// 解码 JSON
	var result map[string]interface{}
	if err := frame.DecodeJSONBody(&result); err != nil {
		t.Fatalf("DecodeJSONBody failed: %v", err)
	}

	if result["sn"] != "MS20260001" {
		t.Errorf("sn mismatch: got %v, want MS20260001", result["sn"])
	}
}

func TestEncodeDecodeFrame_CompressedOnly(t *testing.T) {
	body := map[string]interface{}{
		"sn":    "MS20260002",
		"model": "WNT-MS-2000",
	}

	opts := EncodeOptions{
		Encrypt:  false,
		Compress: true,
	}

	frameBytes, err := EncodeFrame(CmdInfoReport, 10001, body, opts)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	frame, err := DecodeFrame(frameBytes)
	if err != nil {
		t.Fatalf("DecodeFrame failed: %v", err)
	}

	if frame.Header.IsEncrypted() {
		t.Error("should not be encrypted")
	}
	if !frame.Header.IsCompressed() {
		t.Error("should be compressed")
	}

	var result map[string]interface{}
	if err := frame.DecodeJSONBody(&result); err != nil {
		t.Fatalf("DecodeJSONBody failed: %v", err)
	}

	if result["sn"] != "MS20260002" {
		t.Errorf("sn mismatch: got %v", result["sn"])
	}
}

func TestEncodeDecodeFrame_EncryptedAndCompressed(t *testing.T) {
	body := map[string]interface{}{
		"sn":             "MS20260003",
		"model":          "WNT-MS-2000",
		"version":        "V2.0.1",
		"virusLibs":      []map[string]interface{}{{"type": 1, "version": "v3.2.1"}},
	}

	opts := EncodeOptions{
		Encrypt:     true,
		Compress:    true,
		RandomValue: 12345,
		SerialNo:    77777,
	}

	frameBytes, err := EncodeFrame(CmdHeartbeat, 10002, body, opts)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	frame, err := DecodeFrame(frameBytes)
	if err != nil {
		t.Fatalf("DecodeFrame failed: %v", err)
	}

	if !frame.Header.IsEncrypted() {
		t.Error("should be encrypted")
	}
	if !frame.Header.IsCompressed() {
		t.Error("should be compressed")
	}
	if frame.Header.RandomValue != 12345 {
		t.Errorf("RandomValue mismatch: got %d, want 12345", frame.Header.RandomValue)
	}
	if frame.Header.SerialNo != 77777 {
		t.Errorf("SerialNo mismatch: got %d, want 77777", frame.Header.SerialNo)
	}

	var result map[string]interface{}
	if err := frame.DecodeJSONBody(&result); err != nil {
		t.Fatalf("DecodeJSONBody failed: %v", err)
	}

	if result["sn"] != "MS20260003" {
		t.Errorf("sn mismatch: got %v", result["sn"])
	}
}

func TestEncodeDecodeFrame_AllCmdIDs(t *testing.T) {
	cmdIDs := []uint32{
		CmdHeartbeat, CmdInfoReport, CmdRegister, CmdClaimVerify,
		CmdUsbClaim, CmdUsbReturn, CmdAlarm, CmdOperationLog,
		CmdUpgradeIssue, CmdUpgradeResult,
	}

	for _, cmdID := range cmdIDs {
		t.Run(CmdIDToString(cmdID), func(t *testing.T) {
			body := map[string]interface{}{"cmd": cmdID, "test": true}

			opts := EncodeOptions{Encrypt: true, Compress: true, RandomValue: 999}

			frameBytes, err := EncodeFrame(cmdID, 10000, body, opts)
			if err != nil {
				t.Fatalf("EncodeFrame cmdID=%d failed: %v", cmdID, err)
			}

			frame, err := DecodeFrame(frameBytes)
			if err != nil {
				t.Fatalf("DecodeFrame cmdID=%d failed: %v", cmdID, err)
			}

			if frame.Header.CmdID != cmdID {
				t.Errorf("CmdID mismatch: got %d, want %d", frame.Header.CmdID, cmdID)
			}

			var result map[string]interface{}
			if err := frame.DecodeJSONBody(&result); err != nil {
				t.Fatalf("DecodeJSONBody cmdID=%d failed: %v", cmdID, err)
			}
		})
	}
}

func TestReadFrame(t *testing.T) {
	body := map[string]interface{}{"test": "readframe"}
	opts := EncodeOptions{Encrypt: false, Compress: false}

	frameBytes, err := EncodeFrame(CmdHeartbeat, 0, body, opts)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	reader := bytes.NewReader(frameBytes)
	frame, err := ReadFrame(reader)
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}

	if frame.Header.CmdID != CmdHeartbeat {
		t.Errorf("CmdID mismatch: got %d, want %d", frame.Header.CmdID, CmdHeartbeat)
	}

	// 解码 JSON 验证内容
	var result map[string]interface{}
	if err := frame.DecodeJSONBody(&result); err != nil {
		t.Fatalf("DecodeJSONBody failed: %v", err)
	}
	if result["test"] != "readframe" {
		t.Errorf("test field mismatch: got %v", result["test"])
	}
}

func TestCRC32VerificationInDecode(t *testing.T) {
	// 加密+压缩模式下，CRC32 校验在解密解压后验证原始数据
	body := map[string]interface{}{"test": "crc_verify"}
	opts := EncodeOptions{Encrypt: true, Compress: true, RandomValue: 42}

	frameBytes, err := EncodeFrame(CmdHeartbeat, 0, body, opts)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	// 正常解码应成功
	frame, err := DecodeFrame(frameBytes)
	if err != nil {
		t.Fatalf("DecodeFrame failed: %v", err)
	}

	var result map[string]interface{}
	if err := frame.DecodeJSONBody(&result); err != nil {
		t.Fatalf("DecodeJSONBody should succeed for valid frame: %v", err)
	}

	// 篡改包体内容，CRC32 校验应失败
	if len(frameBytes) > HeaderSize+10 {
		frameBytes[HeaderSize+5] ^= 0xFF
	}

	frame2, err := DecodeFrame(frameBytes)
	if err != nil {
		t.Fatalf("DecodeFrame should still succeed (CRC done in DecodeJSONBody): %v", err)
	}

	var result2 map[string]interface{}
	err = frame2.DecodeJSONBody(&result2)
	if err == nil {
		t.Error("DecodeJSONBody should fail for tampered encrypted data")
	}
}

func TestSrcLenPreserved(t *testing.T) {
	// 验证 SrcLen 字段正确记录原始数据长度
	body := map[string]interface{}{"test": "srclen"}
	jsonBytes, _ := json.Marshal(body)

	opts := EncodeOptions{Encrypt: true, Compress: true, RandomValue: 555}
	frameBytes, err := EncodeFrame(CmdInfoReport, 0, body, opts)
	if err != nil {
		t.Fatalf("EncodeFrame failed: %v", err)
	}

	frame, err := DecodeFrame(frameBytes)
	if err != nil {
		t.Fatalf("DecodeFrame failed: %v", err)
	}

	if int(frame.Header.SrcLen) != len(jsonBytes) {
		t.Errorf("SrcLen mismatch: got %d, want %d", frame.Header.SrcLen, len(jsonBytes))
	}
}

// ========== 辅助函数 ==========

func CmdIDToString(cmdID uint32) string {
	names := map[uint32]string{
		CmdHeartbeat:     "Heartbeat",
		CmdInfoReport:    "InfoReport",
		CmdRegister:      "Register",
		CmdClaimVerify:   "ClaimVerify",
		CmdUsbClaim:      "UsbClaim",
		CmdUsbReturn:     "UsbReturn",
		CmdAlarm:         "Alarm",
		CmdOperationLog:  "OperationLog",
		CmdUpgradeIssue:  "UpgradeIssue",
		CmdUpgradeResult: "UpgradeResult",
	}
	if name, ok := names[cmdID]; ok {
		return name
	}
	return "Unknown"
}

// 确保 json 包被使用
var _ = json.Marshal
