package handlers

import (
	"encoding/json"
	"net/http"
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
		return true // для беты разрешаем все origins
	},
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

	// Пинг каждые 30 секунд чтобы держать соединение
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
				conn.WriteJSON(gin.H{"error": "job not found"})
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

			// Если job завершён — закрываем соединение
			if status == "done" || status == "failed" {
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "job finished"))
				return
			}
		}
	}
}
