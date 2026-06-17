package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// Aes256Encrypt AES-256-ECB 加密 + 零填充
// 与主机卫士 CProtocal::Encrypt 完全一致（WLProtocal/Protocal.cpp 第233-467行）
//
// C++ 源码关键逻辑：
//
//	nFillLen = 16 - (nSrcLen % 16);           // 填充到16的倍数
//	nTotalLen = nFillLen + nSrcLen;
//	pPaddingBuf = new char[nTotalLen];
//	memset(pPaddingBuf, 0, nTotalLen);         // 零填充
//	memcpy(pPaddingBuf, pSrcData, nSrcLen);     // 复制原始数据
//	wlEcbEncryptData(&ctx, pPaddingBuf, pPaddingBuf, nTotalLen);  // ECB加密
//
// 返回：密文 + 填充字节数 + error
func Aes256Encrypt(plaintext []byte, key []byte) ([]byte, uint8, error) {
	if len(key) != 32 {
		return nil, 0, fmt.Errorf("invalid key size %d, expected 32", len(key))
	}

	// 零填充：将数据填充到 16 字节的倍数
	// 与 C++ 一致：nFillLen = 16 - (nSrcLen % 16)
	fillLen := aes.BlockSize - (len(plaintext) % aes.BlockSize)
	// 注意：当 len(plaintext) 是 16 的倍数时，C++ 中 nFillLen = 16 - 0 = 16
	// 即总是至少填充一些字节（与 PKCS7 中 n%16==0 时填充16字节的行为相同）
	padded := make([]byte, len(plaintext)+fillLen)
	copy(padded, plaintext)
	// padded 剩余部分已为零值（Go 的 make 初始化）

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrEncryptFailed, err)
	}

	// ECB 模式加密
	ciphertext := make([]byte, len(padded))
	encryptECB(block, ciphertext, padded)

	return ciphertext, uint8(fillLen), nil
}

// Aes256Decrypt AES-256-ECB 解密 + 去除零填充
// 与主机卫士 CProtocal::Decrypt 完全一致（WLProtocal/Protocal.cpp 第155-231行）
//
// C++ 源码关键逻辑：
//
//	wlEcbDecryptData(&ctx, pPaddingBuf, pEncryptData, nEncryptLen);
//	nDecryptLen = nEncryptLen - nFillLen;
//	memcpy(pDecryptData, pPaddingBuf, nDecryptLen);
func Aes256Decrypt(ciphertext []byte, key []byte, fillLen uint8) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key size %d, expected 32", len(key))
	}
	if len(ciphertext) == 0 {
		return nil, ErrDecryptFailed
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("%w: ciphertext not block-aligned", ErrDecryptFailed)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}

	// ECB 模式解密
	padded := make([]byte, len(ciphertext))
	decryptECB(block, padded, ciphertext)

	// 去除零填充：与 C++ 一致，nDecryptLen = nEncryptLen - nFillLen
	if fillLen > 0 && int(fillLen) <= len(padded) {
		return padded[:len(padded)-int(fillLen)], nil
	}

	// 如果 fillLen 为 0，尝试自动检测填充长度
	// 这种情况不应发生在正常协议通信中，但作为容错处理
	if len(padded) > 0 {
		// 查找最后一个非零字节的位置
		lastNonZero := len(padded) - 1
		for lastNonZero >= 0 && padded[lastNonZero] == 0 {
			lastNonZero--
		}
		if lastNonZero < len(padded)-1 {
			return padded[:lastNonZero+1], nil
		}
	}

	return padded, nil
}

// encryptECB ECB 模式加密（Go 标准库不直接提供 ECB，手动逐块加密）
func encryptECB(block cipher.Block, dst, src []byte) {
	blockSize := block.BlockSize()
	for i := 0; i < len(src); i += blockSize {
		block.Encrypt(dst[i:i+blockSize], src[i:i+blockSize])
	}
}

// decryptECB ECB 模式解密
func decryptECB(block cipher.Block, dst, src []byte) {
	blockSize := block.BlockSize()
	for i := 0; i < len(src); i += blockSize {
		block.Decrypt(dst[i:i+blockSize], src[i:i+blockSize])
	}
}

// GenerateRandomKey 生成 32 字节随机 AES 密钥（用于测试）
func GenerateRandomKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate random key: %w", err)
	}
	return key, nil
}


