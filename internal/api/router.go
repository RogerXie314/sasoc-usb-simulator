package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/hub"
	"go.uber.org/zap"
)

// SetupRouter 创建并配置 Gin 路由
func SetupRouter(h *hub.Hub, cfg *config.Config) *gin.Engine {
	// Gin 模式由 main.go 统一控制，此处不再切换
	r := gin.New()

	// 中间件
	r.Use(recoveryMiddleware())
	r.Use(requestLoggerMiddleware())
	r.Use(corsMiddleware())

	// WebSocket
	r.GET("/ws", handleWebSocket(h))

	// API v1 分组 — 路径对齐前端调用
	v1 := r.Group("/api/v1")
	{
		// ---- 版本信息 ----
		v1.GET("/version", func(c *gin.Context) {
			responseSuccess(c, gin.H{"version": AppVersion})
		})
		stationHandler := newStationHandler(h)
		usbHandler := newUsbPluginHandler(h)
		debugHandler := newProtocolDebugHandler(h)
		cfgHandler := newConfigHandler(h)

		// ---- 运行时配置 ----
		v1.GET("/config", cfgHandler.getConfig)
		v1.PUT("/config", cfgHandler.updateConfig)
		v1.GET("/config/sasoc-status", cfgHandler.getSasocStatus)

		// ---- 安检站管理 ----
		// 前端: GET /stations  →  列表
		v1.GET("/stations", stationHandler.listStations)
		// 前端: POST /stations  →  创建
		v1.POST("/stations", stationHandler.createStation)
		// 前端: POST /stations/batch  →  批量创建
		v1.POST("/stations/batch", stationHandler.batchCreateStations)
		// 前端: POST /stations/start-all  →  全部上线
		v1.POST("/stations/start-all", stationHandler.startAllStations)
		// 前端: POST /stations/stop-all  →  全部下线
		v1.POST("/stations/stop-all", stationHandler.stopAllStations)
		// 前端: GET /stations/stats  →  聚合统计
		v1.GET("/stations/stats", stationHandler.getStationStats)
		// 前端: POST /stations/:sn/:action(start|stop|restart)  →  启停
		v1.POST("/stations/:sn/:action", stationHandler.stationAction)
		// 前端: POST /stations/:sn/command  →  发送协议命令
		v1.POST("/stations/:sn/command", stationHandler.sendCommand)
		// 前端: POST /stations/:sn/simulate-upgrade → 模拟平台下发升级（被动接收场景）
		v1.POST("/stations/:sn/simulate-upgrade", stationHandler.simulateUpgrade)
		// 前端: DELETE /stations/:sn  →  删除
		v1.DELETE("/stations/:sn", stationHandler.deleteStation)

		// 旧路径兼容（保留）
		station := v1.Group("/station")
		{
			station.POST("/create", stationHandler.createStation)
			station.GET("/list", stationHandler.listStations)
			station.POST("/:id/start", stationHandler.startStation)
			station.POST("/:id/stop", stationHandler.stopStation)
			station.POST("/:id/restart", stationHandler.restartStation)
			station.POST("/:id/heartbeat", stationHandler.heartbeat)
			station.POST("/:id/report", stationHandler.infoReport)
			station.POST("/:id/verify-claim", stationHandler.verifyClaim)
			station.POST("/:id/claim-usb", stationHandler.claimUsb)
			station.POST("/:id/return-usb", stationHandler.returnUsb)
			station.POST("/:id/alarm", stationHandler.alarm)
			station.POST("/:id/operation-log", stationHandler.operationLog)
			station.POST("/:id/trigger-upgrade", stationHandler.triggerUpgrade)
			station.PUT("/:id/config", stationHandler.updateConfig)
			station.GET("/:id/messages", stationHandler.getMessages)
		}

		// ---- U盘插件 ----
		// 前端: GET /usbs  →  列表
		v1.GET("/usbs", usbHandler.listUsb)
		// 前端: POST /usbs  →  创建/插入
		v1.POST("/usbs", usbHandler.insertUsbSimple)
		// 前端: POST /usbs/batch  →  批量创建
		v1.POST("/usbs/batch", usbHandler.batchCreateUsbs)
		// 前端: POST /usbs/:sn/remove  →  拔出
		v1.POST("/usbs/:sn/remove", usbHandler.removeUsbBySN)
		// 前端: POST /usbs/:sn/insert  →  插入到指定站
		v1.POST("/usbs/:sn/insert", usbHandler.insertUsbBySN)
		// 前端: GET /usbs/:sn/data  →  读取
		v1.GET("/usbs/:sn/data", usbHandler.readUsbBySN)
		// 前端: POST /usbs/:sn/write  →  写入
		v1.POST("/usbs/:sn/write", usbHandler.writeUsbBySN)
		// 前端: POST /usbs/:sn/status  →  生命周期状态转换
		v1.POST("/usbs/:sn/status", usbHandler.setUsbStatus)
		// 前端: DELETE /usbs/:sn  →  删除
		v1.DELETE("/usbs/:sn", usbHandler.deleteUsbBySN)
		// 前端: POST /usbs/fault-injection  →  故障注入
		v1.POST("/usbs/fault-injection", usbHandler.updateUsbFault)

		// ---- 管控柜故障注入 ----
		cabinetHandler := newCabinetHandler(h)
		// 单槽故障注入
		v1.POST("/cabinet/:stationId/fault", cabinetHandler.injectSlotFault)
		// 批量故障注入（整柜或指定门号列表）
		v1.POST("/cabinet/:stationId/fault-batch", cabinetHandler.injectBatchFault)
		// 恢复（单槽或整柜）
		v1.POST("/cabinet/:stationId/restore", cabinetHandler.restoreSlot)
		// 查询全部插槽状态
		v1.GET("/cabinet/:stationId/slots", cabinetHandler.listSlots)

		// 旧路径兼容（保留）
		usbPlug := v1.Group("/usb-plug")
		{
			usbPlug.GET("/list", usbHandler.listUsb)
			usbPlug.POST("/insert", usbHandler.insertUsb)
			usbPlug.POST("/remove/:usbId", usbHandler.removeUsb)
			usbPlug.GET("/read/:usbId", usbHandler.readUsb)
			usbPlug.POST("/write/:usbId", usbHandler.writeUsb)
			usbPlug.POST("/batch-write", usbHandler.batchWrite)
		}

		// ---- 协议调试 ----
		debug := v1.Group("/debug")
		{
			debug.POST("/send", debugHandler.sendRaw)
			debug.POST("/encode", debugHandler.encodeFrame)
			debug.POST("/decode", debugHandler.decodeFrame)
		}

		// ---- 消息日志 ----
		// 前端: GET /logs  ->  查询日志
		v1.GET("/logs", stationHandler.listLogs)
		// 前端: DELETE /logs  ->  清空日志
		v1.DELETE("/logs", stationHandler.clearLogs)

		// ---- 压力测试 ----
		pressureHandler := newPressureHandler(h)
		v1.POST("/pressure/start", pressureHandler.startPressure)
		v1.POST("/pressure/stop", pressureHandler.stopPressure)
		v1.GET("/pressure/stats", pressureHandler.getPressureStats)

		// ---- 批量申领码 ----
		claimHandler := newClaimHandler(h)
		v1.POST("/claim/start", claimHandler.startClaim)
		v1.GET("/claim/status", claimHandler.statusClaim)
		v1.POST("/claim/cancel", claimHandler.cancelClaim)
		v1.GET("/claim/export", claimHandler.exportClaim)
		v1.GET("/claim/counts", claimHandler.countsClaim)
		v1.POST("/claim/login", claimHandler.loginClaim)
		v1.POST("/claim/login-and-start", claimHandler.loginAndStartClaim)
		v1.GET("/claim/login-status", claimHandler.loginStatusClaim)
	}

	return r
}

// corsMiddleware CORS 跨域中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With")
		c.Header("Access-Control-Max-Age", "86400")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Disposition")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// requestLoggerMiddleware 请求日志中间件
func requestLoggerMiddleware() gin.HandlerFunc {
	logger := zap.L()
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.Duration("latency", latency),
		}

		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()))
		}

		switch {
		case status >= 500:
			logger.Error("request", fields...)
		case status >= 400:
			logger.Warn("request", fields...)
		default:
			logger.Info("request", fields...)
		}
	}
}

// recoveryMiddleware panic 恢复中间件
func recoveryMiddleware() gin.HandlerFunc {
	logger := zap.L()
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error("panic recovered",
					zap.Any("error", err),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
				)
				c.AbortWithStatusJSON(500, gin.H{
					"code":    500,
					"message": "internal server error",
				})
			}
		}()
		c.Next()
	}
}
