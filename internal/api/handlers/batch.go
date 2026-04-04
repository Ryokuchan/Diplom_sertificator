package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
)

type BatchHandler struct {
	db    *database.DB
	redis *redis.Client
	log   *logger.Logger
}

func NewBatchHandler(db *database.DB, rdb *redis.Client, log *logger.Logger) *BatchHandler {
	return &BatchHandler{db: db, redis: rdb, log: log}
}

type BatchVerifyRequest struct {
	// Каждый элемент: diploma_number, qr hash или id
	Identifiers []string `json:"identifiers" binding:"required,min=1,max=500"`
}

type BatchVerifyItem struct {
	Identifier string `json:"identifier"`
	Valid      bool   `json:"valid"`
	Name       string `json:"name,omitempty"`
	University string `json:"university,omitempty"`
	Specialty  string `json:"specialty,omitempty"`
	Year       string `json:"year,omitempty"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
}

// VerifyBatch — массовая верификация дипломов
// POST /api/v1/verify/batch
func (h *BatchHandler) VerifyBatch(c *gin.Context) {
	ctx := c.Request.Context()

	var req BatchVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make([]BatchVerifyItem, 0, len(req.Identifiers))

	for _, id := range req.Identifiers {
		id = strings.TrimSpace(id)
		if id == "" {
			results = append(results, BatchVerifyItem{Identifier: id, Error: "empty identifier"})
			continue
		}

		var diplomaNumber, status string
		var metadata []byte
		err := h.db.QueryRow(ctx,
			`SELECT diploma_number, status, metadata
			 FROM diplomas
			 WHERE diploma_number = $1 OR qr_code LIKE '%' || $1 OR id::text = $1
			 LIMIT 1`,
			id,
		).Scan(&diplomaNumber, &status, &metadata)

		if err != nil {
			results = append(results, BatchVerifyItem{
				Identifier: id,
				Valid:       false,
				Error:       "not found",
			})
			continue
		}

		item := BatchVerifyItem{
			Identifier: id,
			Valid:       status == "verified",
			Status:      status,
		}

		if status == "verified" {
			var m map[string]interface{}
			if json.Unmarshal(metadata, &m) == nil {
				item.Name = fmt.Sprintf("%v", m["name"])
				item.University = fmt.Sprintf("%v", m["university"])
				item.Specialty = fmt.Sprintf("%v", m["specialty"])
				item.Year = fmt.Sprintf("%v", m["year"])
			}
		}

		results = append(results, item)
	}

	valid := 0
	for _, r := range results {
		if r.Valid {
			valid++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total":   len(results),
		"valid":   valid,
		"invalid": len(results) - valid,
		"results": results,
	})
}

// GetJobReport — детальный отчёт по job
// GET /api/v1/university/jobs/:id/report
func (h *BatchHandler) GetJobReport(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")
	jobID := c.Param("id")

	var filename, status, summary string
	var progress int
	var createdAt, updatedAt time.Time

	err := h.db.QueryRow(ctx,
		`SELECT filename, status, COALESCE(error, ''), progress, created_at, updated_at
		 FROM upload_jobs WHERE id = $1 AND user_id = $2`,
		jobID, userID,
	).Scan(&filename, &status, &summary, &progress, &createdAt, &updatedAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	// Считаем реально вставленные этим job
	var inserted int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM diplomas WHERE university_id = $1 AND created_at >= $2`,
		userID, createdAt,
	).Scan(&inserted)

	c.JSON(http.StatusOK, gin.H{
		"job_id":     jobID,
		"filename":   filename,
		"status":     status,
		"progress":   progress,
		"inserted":   inserted,
		"summary":    summary,
		"created_at": createdAt.Format(time.RFC3339),
		"updated_at": updatedAt.Format(time.RFC3339),
	})
}
