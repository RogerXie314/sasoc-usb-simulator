package protocol

import "hash/crc32"

// crc32Table CRC32 使用的多项式表（IEEE 多项式，与 zlib 的 crc32() 一致）
// 对应 C++ 源码中的 crc32() 函数调用（使用 zlib 库）
var crc32Table = crc32.MakeTable(crc32.IEEE)

// ComputeChecksum 计算 CRC32 校验值
// 与主机卫士 CProtocal::GetCheckSum 完全一致（WLProtocal/Protocal.cpp 第596-651行）
//
// C++ 源码：
//
//	crc = crc32(0L, Z_NULL, 0);             // 初始化
//	while(nRead < nSrcLen) {
//	    crc = crc32(crc, (Bytef *)pSrcData + nRead, nCurrSize);  // 分块更新
//	}
//
// 校验范围：仅原始源数据（JSON 字节流，压缩和加密之前）
// Go 的 crc32.Checksum 等价于上述分块逻辑（内部实现一致）
func ComputeChecksum(data []byte) uint32 {
	return crc32.Checksum(data, crc32Table)
}

// VerifyChecksum 验证 CRC32 校验值
func VerifyChecksum(data []byte, expected uint32) bool {
	actual := ComputeChecksum(data)
	return actual == expected
}
