package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
)

type ShareHandler struct {
	db    *database.DB
	redis *redis.Client
	log   *logger.Logger
}

func NewShareHandler(db *database.DB, rdb *redis.Client, log *logger.Logger) *ShareHandler {
	return &ShareHandler{db: db, redis: rdb, log: log}
}

type CreateShareRequest struct {
	// TTL в часах, от 1 до 720 (30 дней). По умолчанию 24.
	TTLHours int `json:"ttl_hours"`
	// Если true — после первого успешного открытия ссылки (страница или JSON) токен удаляется.
	ViewOnce bool `json:"view_once"`
	// Если true вместе с view_once — не переиспользовать текущую ссылку, выдать новый токен (кнопка «Новый QR»).
	ForceNew bool `json:"force_new"`
}

type ShareTokenData struct {
	DiplomaID int64     `json:"diploma_id"`
	StudentID int64     `json:"student_id"`
	ExpiresAt time.Time `json:"expires_at"`
	ViewOnce  bool      `json:"view_once"`
}

func publicBaseURL() string {
	base := os.Getenv("PUBLIC_BASE_URL")
	if base == "" {
		return "http://localhost:8080"
	}
	return strings.TrimRight(base, "/")
}

func shareMetaStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func shareActiveSlotKey(studentID, diplomaID int64) string {
	return fmt.Sprintf("share:active:%d:%d", studentID, diplomaID)
}

func statusLabelRU(status string) string {
	switch status {
	case "verified":
		return "Подтверждён"
	case "pending":
		return "Ожидает подтверждения"
	case "revoked":
		return "Аннулирован"
	default:
		return status
	}
}

// CreateShareLink — студент создаёт временную ссылку на свой диплом
// POST /api/v1/diplomas/:id/share
//
// Поведение для UI «QR и ссылка» (студент):
//   - view_once + без force_new: пока токен не открывали и TTL не вышел, повторный POST возвращает тот же token/url (стабильный QR).
//   - После открытия GET /d/:token или GET /api/v1/shared/:token вызывается finishShareAccess — токен удаляется.
//   - force_new: новый токен даже при активном старом (кнопка «Новый QR»).
func (h *ShareHandler) CreateShareLink(c *gin.Context) {
	ctx := c.Request.Context()
	studentID := c.GetInt64("user_id")
	diplomaIDStr := c.Param("id")

	var req CreateShareRequest
	c.ShouldBindJSON(&req)

	if req.TTLHours <= 0 {
		req.TTLHours = 24
	}
	if req.TTLHours > 720 {
		req.TTLHours = 720
	}

	var diplomaID int64
	err := h.db.QueryRow(ctx,
		`SELECT id FROM diplomas WHERE id = $1 AND student_id = $2 AND status = 'verified'`,
		diplomaIDStr, studentID,
	).Scan(&diplomaID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Diploma not found or not verified"})
		return
	}

	ttl := time.Duration(req.TTLHours) * time.Hour
	base := publicBaseURL()

	// Одноразовая ссылка: пока никто не открыл URL и TTL не вышел — тот же QR (обновление страницы / повторный вход в раздел).
	if req.ViewOnce && !req.ForceNew {
		slotKey := shareActiveSlotKey(studentID, diplomaID)
		if existingToken, err := h.redis.Get(ctx, slotKey).Result(); err == nil && existingToken != "" {
			raw, err := h.redis.Get(ctx, "share:"+existingToken).Result()
			if err == nil {
				var prev ShareTokenData
				if json.Unmarshal([]byte(raw), &prev) == nil &&
					prev.ViewOnce && prev.StudentID == studentID && prev.DiplomaID == diplomaID &&
					time.Now().Before(prev.ExpiresAt) {
					ttlH := int(math.Ceil(time.Until(prev.ExpiresAt).Hours()))
					if ttlH < 1 {
						ttlH = 1
					}
					c.JSON(http.StatusOK, gin.H{
						"token":       existingToken,
						"url":         fmt.Sprintf("%s/api/v1/shared/%s", base, existingToken),
						"view_url":    fmt.Sprintf("%s/d/%s", base, existingToken),
						"expires_at":  prev.ExpiresAt.Format(time.RFC3339),
						"ttl_hours":   ttlH,
						"view_once":   true,
						"reused":      true,
					})
					return
				}
			}
		}
	}

	token := uuid.NewString()
	expiresAt := time.Now().Add(ttl)

	data := ShareTokenData{
		DiplomaID: diplomaID,
		StudentID: studentID,
		ExpiresAt: expiresAt,
		ViewOnce:  req.ViewOnce,
	}
	dataJSON, _ := json.Marshal(data)

	if err := h.redis.Set(ctx, "share:"+token, dataJSON, ttl).Err(); err != nil {
		h.log.Error("Failed to store share token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create share link"})
		return
	}

	if req.ViewOnce {
		slotKey := shareActiveSlotKey(studentID, diplomaID)
		if err := h.redis.Set(ctx, slotKey, token, ttl).Err(); err != nil {
			h.log.Error("Failed to store share slot", "error", err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"token":       token,
		"url":         fmt.Sprintf("%s/api/v1/shared/%s", base, token),
		"view_url":    fmt.Sprintf("%s/d/%s", base, token),
		"expires_at":  expiresAt.Format(time.RFC3339),
		"ttl_hours":   req.TTLHours,
		"view_once":   req.ViewOnce,
	})
}

func (h *ShareHandler) readShareToken(ctx context.Context, token string) (ShareTokenData, string, string, map[string]interface{}, error) {
	var zero ShareTokenData
	raw, err := h.redis.Get(ctx, "share:"+token).Result()
	if err != nil {
		return zero, "", "", nil, err
	}

	var data ShareTokenData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return zero, "", "", nil, err
	}

	if time.Now().After(data.ExpiresAt) {
		h.redis.Del(ctx, "share:"+token)
		return zero, "", "", nil, redis.Nil
	}

	var diplomaNumber, status string
	var metadata []byte
	err = h.db.QueryRow(ctx,
		`SELECT diploma_number, status, metadata FROM diplomas WHERE id = $1`,
		data.DiplomaID,
	).Scan(&diplomaNumber, &status, &metadata)
	if err != nil {
		return zero, "", "", nil, err
	}

	var metadataMap map[string]interface{}
	_ = json.Unmarshal(metadata, &metadataMap)

	return data, diplomaNumber, status, metadataMap, nil
}

func (h *ShareHandler) finishShareAccess(ctx context.Context, token string, data ShareTokenData) {
	if !data.ViewOnce {
		return
	}
	h.redis.Del(ctx, "share:"+token)
	if data.StudentID != 0 && data.DiplomaID != 0 {
		slotKey := shareActiveSlotKey(data.StudentID, data.DiplomaID)
		if cur, _ := h.redis.Get(ctx, slotKey).Result(); cur == token {
			h.redis.Del(ctx, slotKey)
		}
	}
}

// AccessSharedDiploma — публичный доступ по временному токену (JSON)
// GET /api/v1/shared/:token
func (h *ShareHandler) AccessSharedDiploma(c *gin.Context) {
	ctx := c.Request.Context()
	token := c.Param("token")

	data, diplomaNumber, status, metadataMap, err := h.readShareToken(ctx, token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Link expired or not found"})
		return
	}

	ttlRemaining := time.Until(data.ExpiresAt)

	c.JSON(http.StatusOK, gin.H{
		"diploma_number": diplomaNumber,
		"status":         status,
		"metadata":       metadataMap,
		"expires_at":     data.ExpiresAt.Format(time.RFC3339),
		"ttl_remaining":  fmt.Sprintf("%.0fh", ttlRemaining.Hours()),
		"view_once":      data.ViewOnce,
	})

	h.finishShareAccess(ctx, token, data)
}

var diplomaPublicViewTpl = template.Must(template.New("diplomaPublic").Parse(`<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>Сведения о дипломе — DiplomaVerify</title>
  <style>
    :root { --bg:#0f172a; --card:#1e293b; --text:#f1f5f9; --muted:#94a3b8; --ok:#22c55e; --warn:#eab308; }
    * { box-sizing: border-box; }
    body { margin:0; min-height:100vh; font-family: system-ui,Segoe UI,sans-serif; background:linear-gradient(160deg,#0f172a,#1e3a5f); color:var(--text); display:flex; align-items:center; justify-content:center; padding:24px; }
    .card { background:var(--card); border-radius:16px; padding:28px 32px; max-width:420px; width:100%; box-shadow:0 20px 50px rgba(0,0,0,.35); border:1px solid rgba(59,130,246,.25); }
    h1 { font-size:1.15rem; margin:0 0 8px; font-weight:600; }
    .sub { color:var(--muted); font-size:.88rem; margin-bottom:20px; }
    .row { display:flex; justify-content:space-between; gap:12px; padding:10px 0; border-bottom:1px solid rgba(148,163,184,.15); font-size:.92rem; }
    .row:last-child { border-bottom:none; }
    .k { color:var(--muted); flex-shrink:0; }
    .v { text-align:right; font-weight:500; word-break:break-word; }
    .badge { display:inline-block; padding:4px 10px; border-radius:999px; font-size:.78rem; font-weight:600; }
    .badge-ok { background:rgba(34,197,94,.2); color:var(--ok); }
    .badge-warn { background:rgba(234,179,8,.2); color:var(--warn); }
    .badge-bad { background:rgba(239,68,68,.2); color:#f87171; }
    .note { margin-top:18px; font-size:.8rem; color:var(--muted); line-height:1.45; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Сведения о дипломе</h1>
    <p class="sub">DiplomaVerify — одноразовый просмотр{{if .ViewOnceNote}} (ссылка больше не действует){{end}}</p>
    <div class="row"><span class="k">Статус</span><span class="v">{{if eq .Status "verified"}}<span class="badge badge-ok">{{.StatusRU}}</span>{{else if eq .Status "pending"}}<span class="badge badge-warn">{{.StatusRU}}</span>{{else}}<span class="badge badge-bad">{{.StatusRU}}</span>{{end}}</span></div>
    <div class="row"><span class="k">Номер диплома</span><span class="v">{{.DiplomaNumber}}</span></div>
    <div class="row"><span class="k">ФИО</span><span class="v">{{.Name}}</span></div>
    <div class="row"><span class="k">ВУЗ</span><span class="v">{{.University}}</span></div>
    <div class="row"><span class="k">Специальность</span><span class="v">{{.Specialty}}</span></div>
    <div class="row"><span class="k">Год</span><span class="v">{{.Year}}</span></div>
    <div class="row"><span class="k">Ссылка действовала до</span><span class="v">{{.ExpiresAt}}</span></div>
    {{if .ViewOnceNote}}<p class="note">Эта ссылка была одноразовой. Для нового просмотра выпускник может снова открыть раздел «QR диплома» в личном кабинете.</p>{{end}}
  </div>
</body>
</html>`))

type diplomaPublicView struct {
	DiplomaNumber string
	Status        string
	StatusRU      string
	Name          string
	University    string
	Specialty     string
	Year          string
	ExpiresAt     string
	ViewOnceNote  bool
}

// ViewSharedDiplomaHTML — человекочитаемая страница по токену (для QR)
// GET /d/:token
func (h *ShareHandler) ViewSharedDiplomaHTML(c *gin.Context) {
	ctx := c.Request.Context()
	token := c.Param("token")

	data, diplomaNumber, status, meta, err := h.readShareToken(ctx, token)
	if err != nil {
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte(`<!DOCTYPE html><html lang="ru"><head><meta charset="utf-8"/><title>Недоступно</title></head><body style="font-family:sans-serif;padding:2rem;text-align:center"><p>Ссылка недействительна или уже была использована.</p></body></html>`))
		return
	}

	viewOnceNote := data.ViewOnce

	page := diplomaPublicView{
		DiplomaNumber: diplomaNumber,
		Status:        status,
		StatusRU:      statusLabelRU(status),
		Name:          shareMetaStr(meta, "name"),
		University:    shareMetaStr(meta, "university"),
		Specialty:     shareMetaStr(meta, "specialty"),
		Year:          shareMetaStr(meta, "year"),
		ExpiresAt:     data.ExpiresAt.Format("02.01.2006 15:04"),
		ViewOnceNote:  viewOnceNote,
	}

	var buf bytes.Buffer
	if err := diplomaPublicViewTpl.Execute(&buf, page); err != nil {
		h.log.Error("diploma view template", "error", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	h.finishShareAccess(ctx, token, data)

	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
}

// RevokeShareLink — студент отзывает ссылку досрочно
// DELETE /api/v1/diplomas/:id/share/:token
func (h *ShareHandler) RevokeShareLink(c *gin.Context) {
	ctx := c.Request.Context()
	studentID := c.GetInt64("user_id")
	token := c.Param("token")

	raw, err := h.redis.Get(ctx, "share:"+token).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Token not found"})
		return
	}

	var data ShareTokenData
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data.StudentID != studentID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	h.redis.Del(ctx, "share:"+token)
	if data.ViewOnce && data.DiplomaID != 0 {
		slotKey := shareActiveSlotKey(studentID, data.DiplomaID)
		if cur, _ := h.redis.Get(ctx, slotKey).Result(); cur == token {
			h.redis.Del(ctx, slotKey)
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "Share link revoked"})
}
