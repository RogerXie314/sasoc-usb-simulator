package protocol

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// ZLibCompress ZLib 压缩
func ZLibCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCompressFailed, err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCompressFailed, err)
	}
	return buf.Bytes(), nil
}

// ZLibDecompress ZLib 解压
func ZLibDecompress(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecompressFailed, err)
	}
	defer r.Close()

	result, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecompressFailed, err)
	}
	return result, nil
}
