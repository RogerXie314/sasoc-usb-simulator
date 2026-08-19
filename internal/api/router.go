package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/hub"
	"go.uber.org/zap"
)

func SetupRouter(h *hub.Hub, cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.Use(recoveryMiddleware())
	r.Use(requestLoggerMiddleware())
	r.Use(corsMiddleware())

	r.GET("/ws", handleWebSocket(h))

	v1 := r.Group("/api/v1")
	{
		v1.GET("/version", func(c *gin.Context) {
			responseSuccess(c, gin.H{"version": AppVersion})
		})
		stationHandler := newStationHandler(h)
		usbHandler := newUsbPluginHandler(h)
		debugHandler := newProtocolDebugHandler(h)
		cfgHandler := newConfigHandler(h)

		v1.GET("/config", cfgHandler.getConfig)
		v1.PUT("/config", cfgHandler.updateConfig)
		v1.GET("/config/sasoc-status", cfgHandler.getSasocStatus)

		v1.GET("/stations", stationHandler.listStations)
		v1.POST("/stations", stationHandler.createStation)
		v1.POST("/stations/batch", stationHandler.batchCreateStations)
		v1.POST("/stations/start-all", stationHandler.startAllStations)
		v1.POST("/stations/stop-all", stationHandler.stopAllStations)
		v1.GET("/stations/stats", stationHandler.getStationStats)
		v1.POST("/stations/:sn/:action", stationHandler.stationAction)
		v1.POST("/stations/:sn/command", stationHandler.sendCommand)
		v1.DELETE("/stations/:sn", stationHandler.deleteStation)

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

		v1.GET("/usbs", usbHandler.listUsb)
		v1.POST("/usbs", usbHandler.insertUsbSimple)
		v1.POST("/usbs/batch", usbHandler.batchCreateUsbs)
		v1.POST("/usbs/:sn/remove", usbHandler.removeUsbBySN)
		v1.POST("/usbs/:sn/insert", usbHandler.insertUsbBySN)
		v1.GET("/usbs/:sn/data", usbHandler.readUsbBySN)
		v1.POST("/usbs/:sn/write", usbHandler.writeUsbBySN)
		v1.POST("/usbs/:sn/status", usbHandler.setUsbStatus)
		v1.DELETE("/usbs/:sn", usbHandler.deleteUsbBySN)
		v1.POST("/usbs/fault-injection", usbHandler.updateUsbFault)

		cabinetHandler := newCabinetHandler(h)
		v1.POST("/cabinet/:stationId/fault", cabinetHandler.injectSlotFault)
		v1.POST("/cabinet/:stationId/fault-batch", cabinetHandler.injectBatchFault)
		v1.POST("/cabinet/:stationId/restore", cabinetHandler.restoreSlot)
		v1.GET("/cabinet/:stationId/slots", cabinetHandler.listSlots)

		usbPlug := v1.Group("/usb-plug")
		{
			usbPlug.GET("/list", usbHandler.listUsb)
			usbPlug.POST("/insert", usbHandler.insertUsb)
			usbPlug.POST("/remove/:usbId", usbHandler.removeUsb)
			usbPlug.GET("/read/:usbId", usbHandler.readUsb)
			usbPlug.POST("/write/:usbId", usbHandler.writeUsb)
			usbPlug.POST("/batch-write", usbHandler.batchWrite)
		}

		debug := v1.Group("/debug")
		{
			debug.POST("/send", debugHandler.sendRaw)
			debug.POST("/encode", debugHandler.encodeFrame)
			debug.POST("/decode", debugHandler.decodeFrame)
		}

		v1.GET("/logs", stationHandler.listLogs)
		v1.DELETE("/logs", stationHandler.clearLogs)

		pressureHandler := newPressureHandler(h)
		v1.POST("/pressure/start", pressureHandler.startPressure)
		v1.POST("/pressure/stop", pressureHandler.stopPressure)
		v1.GET("/pressure/stats", pressureHandler.getPressureStats)

		claimHandler := newClaimHandler(h)
		v1.POST("/claim/start", claimHandler.startClaim)
		v1.GET("/claim/status", claimHandler.statusClaim)
		v1.POST("/claim/cancel", claimHandler.cancelClaim)
		v1.GET("/claim/export", claimHandler.exportClaim)
		v1.GET("/claim/counts", claimHandler.countsClaim)
		v1.POST("/claim/login", claimHandler.loginClaim)
		v1.POST("/claim/lifecycle/start", claimHandler.startLifecycle)
		v1.GET("/claim/lifecycle/status", claimHandler.statusLifecycle)
		v1.POST("/claim/lifecycle/cancel", claimHandler.cancelLifecycle)
	}

	return r
}

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