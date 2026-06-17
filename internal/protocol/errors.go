package protocol

import (
	"errors"
	"time"
)

var (
	ErrHeaderTooShort  = errors.New("header data too short, expected 48 bytes")
	ErrInvalidHeadFlag = errors.New("invalid head flag, expected 0x5054")
	ErrBodyLenTooLarge = errors.New("body length exceeds maximum (65535)")
	ErrChecksumFailed  = errors.New("CRC32 checksum verification failed")
	ErrDecryptFailed   = errors.New("AES256 decryption failed")
	ErrEncryptFailed   = errors.New("AES256 encryption failed")
	ErrDecompressFailed = errors.New("ZLib decompression failed")
	ErrCompressFailed   = errors.New("ZLib compression failed")
	ErrInvalidFillLen   = errors.New("invalid fill length for decryption")
	ErrInvalidJSON      = errors.New("invalid JSON body")
	ErrFrameTooShort    = errors.New("frame too short, less than header size")
	ErrBodyTruncated    = errors.New("body data truncated, not enough bytes")
)

// currentMillis 获取当前时间戳（毫秒）
func currentMillis() int64 {
	return time.Now().UnixMilli()
}
