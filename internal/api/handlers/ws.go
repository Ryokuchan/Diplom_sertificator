package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// NotifyHub — глобальный хаб для push-уведомлений пользователям
type NotifyHub struct {
	mu      sync.RWMutex
	clients map[int64]map[*websocket.Conn]struct{}
}

var GlobalHub = &NotifyHub{
	clients: make(map[int64]map[*websocket.Conn]struct{}),
}

func (h *NotifyHub) register(userID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*websocket.Conn]struct{})
	}
	h.clients[userID][conn] = struct{}{}
}

func (h *NotifyHub) unregister(userID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conns, ok := h.clients[userID]; ok {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.clients, userID)
		}
	}
}

// SendToUser отправляет уведомление конкретному пользователю
func (h *NotifyHub) SendToUser(userID int64, msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	conns := h.clients[userID]
	h.mu.RUnlock()
	for conn := range conns {
		conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck
	}
}

// Broadcast отправляет уведомление всем подключённым пользователям
func (h *NotifyHub) Broadcast(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, conns := range h.clients {
		for conn := range conns {
			conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck
		}
	}
}

// NotifyMessage — структура push-уведомления
type NotifyMessage struct {
	Type    string `json:"type"`              // "reload", "job_done", "app_approved", "app_rejected"
	Payload any    `json:"payload,omitempty"` // доп. данные
}

type WSHandler struct {
	db    *database.DB
	redis *redis.Client
	log   *logger.Logger
}

func NewWSHandler(db *database.DB, rdb *redis.Client, log *logger.Logger) *WSHandler {
	return &WSHandler{db: db, redis: rdb, log: log}
}

type JobStatusMessage struct {
	JobID    string `json:"job_id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Summary  string `json:"summary,omitempty"`
}

// JobStatus — WebSocket стрим статуса job
// GET /ws/jobs/:id
func (h *WSHandler) JobStatus(c *gin.Context) {
	jobID := c.Param("id")
	userID := c.GetInt64("user_id")

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Error("WebSocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	ctx := c.Request.Context()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-ticker.C:
			var status, summary string
			var progress int
			err := h.db.QueryRow(ctx,
				`SELECT status, progress, COALESCE(error, '')
				 FROM upload_jobs WHERE id = $1 AND user_id = $2`,
				jobID, userID,
			).Scan(&status, &progress, &summary)

			if err != nil {
				conn.WriteJSON(gin.H{"error": "job not found"}) //nolint:errcheck
				return
			}

			msg := JobStatusMessage{
				JobID:    jobID,
				Status:   status,
				Progress: progress,
				Summary:  summary,
			}

			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

			if status == "done" || status == "failed" {
				// Уведомляем все вкладки этого пользователя о завершении
				GlobalHub.SendToUser(userID, NotifyMessage{
					Type:    "job_done",
					Payload: map[string]string{"job_id": jobID, "status": status},
				})
				conn.WriteMessage(websocket.CloseMessage, //nolint:errcheck
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "job finished"))
				return
			}
		}
	}
}

// Notify — постоянный WebSocket канал уведомлений для пользователя
// GET /ws/notify
func (h *WSHandler) Notify(c *gin.Context) {
	userID := c.GetInt64("user_id")

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Error("WebSocket notify upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	GlobalHub.register(userID, conn)
	defer GlobalHub.unregister(userID, conn)

	// Читаем входящие сообщения (ping/pong), чтобы не блокировать
	conn.SetReadDeadline(time.Time{}) //nolint:errcheck
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(70 * time.Second)) //nolint:errcheck
		return nil
	})

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-c.Request.Context().Done():
			return
		case <-pingTicker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendReload отправляет команду reload конкретному пользователю (хелпер для хендлеров)
func SendReload(userID int64, reason string) {
	GlobalHub.SendToUser(userID, NotifyMessage{
		Type:    "reload",
		Payload: map[string]string{"reason": reason},
	})
}

// BroadcastReload отправляет reload всем (например, после одобрения ВУЗа)
func BroadcastReload(reason string) {
	GlobalHub.Broadcast(NotifyMessage{
		Type:    "reload",
		Payload: map[string]string{"reason": reason},
	})
}
