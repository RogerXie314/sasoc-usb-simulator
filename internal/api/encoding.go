package api

import (
	"encoding/json"
	"io"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// autoDecode 对可能为 GBK 编码的字符串做自适应转换
func autoDecode(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	dec := simplifiedchinese.GBK.NewDecoder()
	out, _, err := transform.String(dec, s)
	if err != nil {
		return s
	}
	return out
}

// autoDecodeBytes 对可能为 GBK 编码的字节做自适应转换
func autoDecodeBytes(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	out, err := simplifiedchinese.GBK.NewDecoder().Bytes(b)
	if err != nil {
		return b
	}
	return out
}

// bindJSONWithAutoDecode 读取请求体，对可能为 GBK 的 JSON 做自适应 UTF-8 转换后再反序列化
func bindJSONWithAutoDecode(c *gin.Context, obj interface{}) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	body = autoDecodeBytes(body)
	return json.Unmarshal(body, obj)
}
