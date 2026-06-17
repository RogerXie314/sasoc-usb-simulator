package protocol

import (
	"crypto/sha1"
	"fmt"
)

// 密钥派生常量（与主机卫士 CProtocal::GetEncryptKey 完全一致）
const (
	// sha1KeyPrefix 是主机卫士协议中硬编码的密钥前缀字符串
	// 来源：WLProtocal/Protocal.cpp 第21行
	//   const string g_strEncryptKey = "4&k7w4&P588%e5r684k$bj@JB$Jf8bik";
	sha1KeyPrefix = "4&k7w4&P588%e5r684k$bj@JB$Jf8bik"

	// sha1DigestLen SHA1 输出 20 字节
	sha1DigestLen = 20

	// aesKeyLen AES-256 密钥长度 32 字节（SHA1 的 20 字节 + 12 字节零填充）
	aesKeyLen = 32
)

// DeriveKey 根据 randomValue 派生 32 字节 AES 密钥
//
// 算法（与主机卫士 CProtocal::GetEncryptKey 完全一致）：
//  1. 拼接：sha1KeyPrefix + strconv.Itoa(int(randomValue))
//  2. SHA1 哈希：得到 20 字节摘要
//  3. 输出：20 字节 SHA1 摘要 + 12 字节零 = 32 字节 AES-256 密钥
//
// 对应 C++ 源码（WLProtocal/Protocal.cpp 第126-145行）：
//
//	void CProtocal::GetEncryptKey(unsigned short nRandomKey, char *&pEncryptKey)
//	{
//	    sha1_context sha1Ctx;
//	    ostringstream strTemp;
//	    unsigned char hashCode[SHA1_LEN] = {0};
//	    strTemp << g_strEncryptKey.c_str() << nRandomKey;
//	    sha1_starts(&sha1Ctx);
//	    sha1_update(&sha1Ctx, (unsigned char *)strTemp.str().c_str(), strTemp.str().length());
//	    sha1_finish(&sha1Ctx, hashCode);
//	    memcpy(pEncryptKey, hashCode, sizeof(hashCode));
//	}
//
// 注意：C++ 中 pEncryptKey 缓冲区为 KEY_LEN=32 字节，先 ZeroMemory 再 GetEncryptKey，
// 因此只有前 20 字节是 SHA1 值，后 12 字节保持为 0。
func DeriveKey(randomValue uint16) []byte {
	// Step 1: 拼接 sha1KeyPrefix + randomValue 的十进制字符串
	// C++ ostringstream 的 << unsigned short 会输出十进制数字字符串
	input := fmt.Sprintf("%s%d", sha1KeyPrefix, randomValue)

	// Step 2: SHA1 哈希
	hash := sha1.Sum([]byte(input))

	// Step 3: 20 字节 SHA1 摘要 + 12 字节零填充 = 32 字节 AES-256 密钥
	key := make([]byte, aesKeyLen)
	copy(key, hash[:sha1DigestLen])
	// key[20:32] 已为零值

	return key
}
