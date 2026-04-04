package handlers

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
)

type UniversityApplicationHandler struct {
	db  *database.DB
	log *logger.Logger
}

func NewUniversityApplicationHandler(db *database.DB, log *logger.Logger) *UniversityApplicationHandler {
	return &UniversityApplicationHandler{db: db, log: log}
}

type docEntry struct {
	Original string `json:"original"`
	Stored   string `json:"stored"`
}

var allowedUniDocExt = map[string]bool{
	".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".doc": true, ".docx": true,
}

const maxUniAppFileBytes = 32 << 20
const maxUniAppFiles = 8

// POST /api/v1/auth/university/apply — публично: заявка на аккаунт ВУЗа с вложениями
func (h *UniversityApplicationHandler) Apply(c *gin.Context) {
	ctx := c.Request.Context()

	email := strings.TrimSpace(strings.ToLower(c.PostForm("email")))
	password := c.PostForm("password")
	orgName := strings.TrimSpace(c.PostForm("organization_name"))
	notes := strings.TrimSpace(c.PostForm("notes"))

	if email == "" || password == "" || len(password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email, password (min 6) обязательны"})
		return
	}
	if orgName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "organization_name обязателен"})
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "multipart form required"})
		return
	}
	files := form.File["documents"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Прикрепите хотя бы один документ (скан лицензии, выписка и т.д.)"})
		return
	}
	if len(files) > maxUniAppFiles {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Не более %d файлов", maxUniAppFiles)})
		return
	}

	var userExists int64
	_ = h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE LOWER(email) = $1`, email).Scan(&userExists)
	if userExists > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Пользователь с таким email уже зарегистрирован"})
		return
	}

	var pend int64
	_ = h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM university_applications WHERE LOWER(email) = $1 AND status = 'pending'`,
		email,
	).Scan(&pend)
	if pend > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Заявка с этим email уже на рассмотрении"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}

	var appID int64
	err = h.db.QueryRow(ctx,
		`INSERT INTO university_applications (email, password_hash, organization_name, notes, documents, status)
		 VALUES ($1, $2, $3, $4, '[]', 'pending') RETURNING id`,
		email, string(hash), orgName, notes,
	).Scan(&appID)
	if err != nil {
		h.log.Error("uni apply insert", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось сохранить заявку"})
		return
	}

	dir := filepath.Join("data", "uni-apps", fmt.Sprintf("%d", appID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.db.Exec(ctx, `DELETE FROM university_applications WHERE id = $1`, appID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать каталог для файлов"})
		return
	}

	var docs []docEntry
	for _, fh := range files {
		if fh.Size > maxUniAppFileBytes {
			h.cleanupAppDir(appID)
			h.db.Exec(ctx, `DELETE FROM university_applications WHERE id = $1`, appID)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Файл «%s» больше 32 МБ", fh.Filename)})
			return
		}
		ext := strings.ToLower(filepath.Ext(fh.Filename))
		if !allowedUniDocExt[ext] {
			h.cleanupAppDir(appID)
			h.db.Exec(ctx, `DELETE FROM university_applications WHERE id = $1`, appID)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Недопустимый тип файла: %s", fh.Filename)})
			return
		}
		stored := fmt.Sprintf("%s_%s", uuid.NewString()[:8], sanitizeStoredName(fh.Filename))
		dst := filepath.Join(dir, stored)
		if err := c.SaveUploadedFile(fh, dst); err != nil {
			h.cleanupAppDir(appID)
			h.db.Exec(ctx, `DELETE FROM university_applications WHERE id = $1`, appID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка сохранения файла"})
			return
		}
		docs = append(docs, docEntry{Original: filepath.Base(fh.Filename), Stored: stored})
	}

	docsJSON, _ := json.Marshal(docs)
	_, err = h.db.Exec(ctx, `UPDATE university_applications SET documents = $2, updated_at = NOW() WHERE id = $1`, appID, docsJSON)
	if err != nil {
		h.cleanupAppDir(appID)
		h.db.Exec(ctx, `DELETE FROM university_applications WHERE id = $1`, appID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заявку"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":     appID,
		"status": "pending",
		"message": "Заявка принята. После проверки администратором вы сможете войти с указанным email и паролем.",
	})
}

func sanitizeStoredName(name string) string {
	base := filepath.Base(name)
	base = strings.ReplaceAll(base, "..", "_")
	if len(base) > 180 {
		base = base[:180]
	}
	return base
}

func (h *UniversityApplicationHandler) cleanupAppDir(appID int64) {
	_ = os.RemoveAll(filepath.Join("data", "uni-apps", fmt.Sprintf("%d", appID)))
}

// GET /api/v1/admin/university-applications
func (h *UniversityApplicationHandler) List(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Только администратор"})
		return
	}
	ctx := c.Request.Context()
	status := strings.TrimSpace(c.DefaultQuery("status", "pending"))
	if status != "pending" && status != "all" {
		status = "pending"
	}

	q := `SELECT id, email, organization_name, COALESCE(notes, ''), documents, status, COALESCE(review_note, ''), created_at, updated_at
		FROM university_applications`
	var args []interface{}
	if status != "all" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC LIMIT 200`

	rows, err := h.db.Query(ctx, q, args...)
	if err != nil {
		h.log.Error("uni app list", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка выборки"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id int64
		var email, org, notes, st, reviewNote string
		var docs []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &email, &org, &notes, &docs, &st, &reviewNote, &createdAt, &updatedAt); err != nil {
			continue
		}
		var docList []docEntry
		_ = json.Unmarshal(docs, &docList)
		out = append(out, gin.H{
			"id":                 id,
			"email":              email,
			"organization_name": org,
			"notes":              notes,
			"documents":          docList,
			"status":             st,
			"review_note":        reviewNote,
			"created_at":         createdAt.Format(time.RFC3339),
			"updated_at":         updatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, out)
}

// POST /api/v1/admin/university-applications/:id/approve
func (h *UniversityApplicationHandler) Approve(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Только администратор"})
		return
	}
	ctx := c.Request.Context()
	adminID := c.GetInt64("user_id")
	id := c.Param("id")

	tx, err := h.db.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tx"})
		return
	}
	defer tx.Rollback(ctx)

	var email, pwdHash, org, status string
	err = tx.QueryRow(ctx,
		`SELECT email, password_hash, organization_name, status FROM university_applications WHERE id = $1`,
		id,
	).Scan(&email, &pwdHash, &org, &status)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Заявка не найдена"})
		return
	}
	if status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "Заявка уже обработана"})
		return
	}

	var exists int64
	_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE LOWER(email) = LOWER($1)`, email).Scan(&exists)
	if exists > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Пользователь с этим email уже есть"})
		return
	}

	var newUserID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ($1, $2, 'university') RETURNING id`,
		email, pwdHash,
	).Scan(&newUserID)
	if err != nil {
		h.log.Error("uni approve insert user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать пользователя"})
		return
	}

	_, err = tx.Exec(ctx,
		`UPDATE university_applications SET status = 'approved', reviewer_id = $2, updated_at = NOW() WHERE id = $1`,
		id, adminID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить заявку"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ВУЗ зарегистрирован", "user_id": newUserID, "email": email})
}

// POST /api/v1/admin/university-applications/:id/reject
func (h *UniversityApplicationHandler) Reject(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Только администратор"})
		return
	}
	ctx := c.Request.Context()
	adminID := c.GetInt64("user_id")
	id := c.Param("id")

	var body struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&body)

	var status string
	err := h.db.QueryRow(ctx, `SELECT status FROM university_applications WHERE id = $1`, id).Scan(&status)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Заявка не найдена"})
		return
	}
	if status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "Заявка уже обработана"})
		return
	}

	_, err = h.db.Exec(ctx,
		`UPDATE university_applications SET status = 'rejected', reviewer_id = $2, review_note = $3, updated_at = NOW() WHERE id = $1`,
		id, adminID, strings.TrimSpace(body.Note),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отклонить"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Заявка отклонена"})
}

// GET /api/v1/admin/university-applications/:id/file — скачать вложение (f=stored имя из documents[].stored)
func (h *UniversityApplicationHandler) DownloadFile(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Только администратор"})
		return
	}
	ctx := c.Request.Context()
	id := c.Param("id")
	stored := filepath.Base(c.Query("f"))
	if stored == "" || stored == "." {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Укажите параметр f"})
		return
	}

	var docs []byte
	err := h.db.QueryRow(ctx, `SELECT documents FROM university_applications WHERE id = $1`, id).Scan(&docs)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Не найдено"})
		return
	}
	var docList []docEntry
	if json.Unmarshal(docs, &docList) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bad documents"})
		return
	}

	ok := false
	var orig string
	for _, d := range docList {
		if d.Stored == stored {
			ok = true
			orig = d.Original
			break
		}
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Файл не из этой заявки"})
		return
	}

	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if strings.ContainsAny(stored, "/\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file"})
		return
	}
	full := filepath.Join("data", "uni-apps", id, stored)

	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(stored)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Header("Content-Type", ct)

	inline := c.Query("inline") == "1" || c.Query("inline") == "true"
	fn := strings.ReplaceAll(orig, `"`, ``)
	if inline {
		c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fn))
	} else {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	}
	c.File(full)
}
