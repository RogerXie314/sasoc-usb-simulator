package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AppVersion 应用版本号（与 main.go 保持同步）
const AppVersion = "V2.5"

// responseSuccess 成功响应
func responseSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": data,
	})
}

// responseError 错误响应
func responseError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{
		"code":    status,
		"message": msg,
	})
}

// responseErrorWithCode 带业务错误码的响应
func responseErrorWithCode(c *gin.Context, httpStatus int, code int, msg string) {
	c.JSON(httpStatus, gin.H{
		"code":    code,
		"message": msg,
	})
}
