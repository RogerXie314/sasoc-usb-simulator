package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/usb-simulator/internal/hub"
	"go.uber.org/zap"
)

// WebSocket 升级器
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源（开发环境）
	},
}

// 订阅的事件类型列表
var wsEventTypes = []string{
	hub.EventStationStateChanged,
	hub.EventMessageSent,
	hub.EventMessageReceived,
	hub.EventUsbInserted,
	hub.EventUsbRemoved,
}

// wsClient WebSocket 客户端连接
type wsClient struct {
	hub  *hub.Hub
	conn *websocket.Conn
}

// handleWebSocket WebSocket 处理器工厂
func handleWebSocket(h *hub.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			zap.L().Warn("websocket upgrade failed", zap.Error(err))
			return
		}

		client := &wsClient{
			hub:  h,
			conn: conn,
		}

		zap.L().Info("websocket client connected",
			zap.String("remote", conn.RemoteAddr().String()),
		)

		// 订阅所有事件类型
		subs := make([]*hub.EventSubscriber, 0, len(wsEventTypes))
		for _, eventType := range wsEventTypes {
			sub := h.EventBus().Subscribe(eventType)
			subs = append(subs, sub)
		}

		// 合并所有订阅通道为单一事件通道
		eventCh := make(chan hub.Event, 256)
		for _, sub := range subs {
			go func(s *hub.EventSubscriber) {
				for {
					select {
					case <-s.Quit:
						return
					case e, ok := <-s.Ch:
						if !ok {
							return
						}
						select {
						case eventCh <- e:
						default:
							// 通道满则丢弃
						}
					}
				}
			}(sub)
		}

		// 发送欢迎消息
		welcome, _ := json.Marshal(map[string]interface{}{
			"type":    "connected",
			"message": "websocket connection established",
			"ts":      time.Now().UnixMilli(),
		})

		// 启动读写 goroutine
		done := make(chan struct{})
		go client.readPump(done)
		go client.writePump(subs, eventCh, welcome, done)
	}
}

// readPump 读取客户端消息（保活 + 优雅关闭）
func (c *wsClient) readPump(done chan struct{}) {
	defer close(done)

	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				zap.L().Debug("websocket read error", zap.Error(err))
			}
			break
		}
	}
}

// writePump 向客户端发送消息（事件转发 + 心跳 ping）
func (c *wsClient) writePump(subs []*hub.EventSubscriber, eventCh chan hub.Event, welcome []byte, done chan struct{}) {
	defer func() {
		// 清理所有订阅
		for i, sub := range subs {
			if i < len(wsEventTypes) {
				c.hub.EventBus().Unsubscribe(wsEventTypes[i], sub.ID)
			}
		}
		c.conn.Close()

		zap.L().Info("websocket client disconnected",
			zap.String("remote", c.conn.RemoteAddr().String()),
		)
	}()

	// 发送欢迎消息
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteMessage(websocket.TextMessage, welcome); err != nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			// 客户端断开
			_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return

		case event, ok := <-eventCh:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				zap.L().Warn("websocket marshal event failed", zap.Error(err))
				continue
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				zap.L().Debug("websocket write failed", zap.Error(err))
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
