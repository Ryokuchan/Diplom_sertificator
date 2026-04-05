package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
)

type StatsHandler struct {
	db  *database.DB
	log *logger.Logger
}

func NewStatsHandler(db *database.DB, log *logger.Logger) *StatsHandler {
	return &StatsHandler{db: db, log: log}
}

// GET /api/v1/university/stats
func (h *StatsHandler) UniversityStats(c *gin.Context) {
	if c.GetString("role") != "university" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Доступно только ВУЗу"})
		return
	}
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	var total, verified, revoked, pending int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM diplomas WHERE university_id = $1`, userID).Scan(&total)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM diplomas WHERE university_id = $1 AND status='verified'`, userID).Scan(&verified)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM diplomas WHERE university_id = $1 AND status='revoked'`, userID).Scan(&revoked)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM diplomas WHERE university_id = $1 AND status='pending'`, userID).Scan(&pending)

	c.JSON(http.StatusOK, gin.H{
		"total":    total,
		"verified": verified,
		"revoked":  revoked,
		"pending":  pending,
	})
}

// GET /api/v1/university/records/export
func (h *StatsHandler) ExportUniversityRecords(c *gin.Context) {
	if c.GetString("role") != "university" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Доступно только ВУЗу"})
		return
	}
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	rows, err := h.db.Query(ctx,
		`SELECT d.id, d.diploma_number, d.status, d.metadata, d.created_at
		 FROM diplomas d WHERE d.university_id = $1 ORDER BY d.created_at DESC`,
		userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка выборки"})
		return
	}
	defer rows.Close()

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="records_%s.csv"`, time.Now().Format("20060102_150405")))

	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"ID", "Номер диплома", "ФИО", "Специальность", "Год", "Статус", "Дата создания"})

	for rows.Next() {
		var id int64
		var diplomaNumber, status string
		var createdAt time.Time
		var metadata []byte
		if err := rows.Scan(&id, &diplomaNumber, &status, &metadata, &createdAt); err != nil {
			continue
		}
		var m map[string]interface{}
		_ = json.Unmarshal(metadata, &m)
		_ = w.Write([]string{
			fmt.Sprintf("%d", id),
			diplomaNumber,
			statsMapStr(m, "name"),
			statsMapStr(m, "specialty"),
			statsMapStr(m, "year"),
			status,
			createdAt.Format("02.01.2006 15:04"),
		})
	}
	w.Flush()
}

// GET /api/v1/employer/history/export
func (h *StatsHandler) ExportEmployerHistory(c *gin.Context) {
	if c.GetString("role") != "hr" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Доступно только работодателю"})
		return
	}
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	rows, err := h.db.Query(ctx,
		`SELECT vl.created_at, d.diploma_number, d.metadata, d.status
		 FROM verification_logs vl
		 JOIN diplomas d ON d.id = vl.diploma_id
		 WHERE vl.verifier_id = $1
		 ORDER BY vl.created_at DESC LIMIT 5000`,
		userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка выборки"})
		return
	}
	defer rows.Close()

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="history_%s.csv"`, time.Now().Format("20060102_150405")))

	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"Дата", "Номер диплома", "ФИО", "Результат"})

	for rows.Next() {
		var createdAt time.Time
		var diplomaNumber, status string
		var metadata []byte
		if err := rows.Scan(&createdAt, &diplomaNumber, &metadata, &status); err != nil {
			continue
		}
		var m map[string]interface{}
		_ = json.Unmarshal(metadata, &m)
		result := "Недействителен"
		if status == "verified" {
			result = "Действителен"
		}
		_ = w.Write([]string{
			createdAt.Format("02.01.2006 15:04"),
			diplomaNumber,
			statsMapStr(m, "name"),
			result,
		})
	}
	w.Flush()
}

// GET /api/v1/admin/stats
func (h *StatsHandler) AdminStats(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Только администратор"})
		return
	}
	ctx := c.Request.Context()

	var totalUsers, students, universities, employers int
	var totalDiplomas, verifiedDiplomas, revokedDiplomas, totalChecks int

	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&totalUsers)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role='student'`).Scan(&students)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role='university'`).Scan(&universities)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role='hr'`).Scan(&employers)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM diplomas`).Scan(&totalDiplomas)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM diplomas WHERE status='verified'`).Scan(&verifiedDiplomas)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM diplomas WHERE status='revoked'`).Scan(&revokedDiplomas)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM verification_logs`).Scan(&totalChecks)

	c.JSON(http.StatusOK, gin.H{
		"users": gin.H{
			"total":        totalUsers,
			"students":     students,
			"universities": universities,
			"employers":    employers,
		},
		"diplomas": gin.H{
			"total":    totalDiplomas,
			"verified": verifiedDiplomas,
			"revoked":  revokedDiplomas,
			"pending":  totalDiplomas - verifiedDiplomas - revokedDiplomas,
		},
		"checks": totalChecks,
	})
}

func statsMapStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
